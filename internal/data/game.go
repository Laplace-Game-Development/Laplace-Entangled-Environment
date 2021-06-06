package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/mediocregopher/radix/v3"
	"github.com/pebbe/zmq4"
	"laplace-entangled-env.com/internal/policy"
	"laplace-entangled-env.com/internal/redis"
	"laplace-entangled-env.com/internal/zeromq"
)

// Configurables
const WaitDurationForGameStart time.Duration = 10 * time.Second
const WaitDurationForGameAction time.Duration = 3 * time.Second
const CommandToExec string = "node"
const GamePortNum string = "5011"
const GamePort string = ":" + GamePortNum

var CommandArgs []string = []string{"./node-layer/index.js", "--binding=5011"}

// Global Variables | Singletons
var commandContext context.Context = nil
var cancelFunc func() = nil

func StartGameLogic() (func(), error) {
	executeCommand()
	return cleanUpGameLogic, nil
}

func cleanUpGameLogic() {
	log.Println("Cleaning Up Game Logic")
	cancelFunc()

	deadlineForStop := time.Now().Add(WaitDurationForGameStart)
	done := false
	for deadlineForStop.Before(time.Now()) {
		select {
		case <-commandContext.Done():
			done = true
			break
		default:
		}
	}

	if !done {
		log.Println("Game Did Not Finish Executing in Wait Time! Closing Program.")
	}
}

func executeCommand() {
	commandContext, cancelFunc = context.WithCancel(context.Background())

	pwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Error getting PWD: %v\n", err)
	}

	log.Printf("Present Working Directory\n%s\n", pwd)
	log.Printf("Executing Command!\n %s %v\n", CommandToExec, CommandArgs)

	cmd := exec.CommandContext(commandContext, CommandToExec, CommandArgs...)

	// Making sure to nil these for security reasons
	cmd.Stdout = nil
	cmd.Stderr = nil

	// TODO Add Errorhandling to function via Channel.
	go func() {
		err := cmd.Run()
		if err != nil {
			log.Printf("Error Recieved From Game %v\n", err)
		}
	}()
}

func BytesToGame(dataIn string) (string, error) {
	// Create a Zeromq Request Port
	req, err := zeromq.MasterZeroMQ.NewSocket(zmq4.Type(zmq4.REQ))
	if err != nil {
		return "", err
	}

	// Not necessary, but good practice
	defer req.Close()

	err = req.Connect(zeromq.ZeromqHost + GamePort)
	if err != nil {
		return "", err
	}

	num, err := req.Send(dataIn, zmq4.Flag(0))
	if err != nil {
		return "", err
	} else if len(dataIn) != num {
		return "", errors.New("ZeroMQ did not Accept Full Job! Characters Accepted:" + fmt.Sprintf("%d", num))
	}

	return BytesFromGame(req)
}

func BytesFromGame(req *zmq4.Socket) (string, error) {
	poller := zmq4.NewPoller()

	poller.Add(req, zmq4.POLLIN)
	sockets, err := poller.Poll(WaitDurationForGameAction)
	if err != nil {
		log.Println("It seems Response Wait Was Interrupted")
		return "", err
	} else if len(sockets) <= 0 {
		return "", errors.New("Game Seems To Be Offline!")
	}

	reply, err := req.Recv(zmq4.Flag(zmq4.DONTWAIT))
	if err != nil {
		return "", err
	}

	return reply, nil
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Game Actions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

type applyActionRequest struct {
	GameID string
	Relay  map[string]interface{}
}

type actionServerPayload struct {
	State map[string]interface{}
	Relay map[string]interface{}
}

func ApplyAction(header policy.RequestHeader, bodyFactories policy.RequestBodyFactories, isSecureConnection bool) policy.CommandResponse {
	// 1. Verify Request
	err := bodyFactories.SigVerify(header.UserID, header.Sig)
	if err != nil {
		log.Printf("Unauthorized Attempt! Error: %v\n", err)
		return policy.UnSuccessfulResponse("Unauthorized!")
	}

	// 2. Get Request Data
	args := applyActionRequest{}
	err = bodyFactories.ParseFactory(&args)
	if err != nil {
		log.Printf("Bad Argument! Error: %v\n", err)
		return policy.UnSuccessfulResponse("Bad Arguments!")
	}

	// 3. Verify User is In Game
	isInGame, err := IsUserInGame(header.UserID, args.GameID)
	if err != nil {
		log.Printf("Error Verifying User is in game: %v\n", err)
		return policy.UnSuccessfulResponse("User Not In Game")
	} else if !isInGame {
		return policy.UnSuccessfulResponse("User Not In Game")
	}

	// 4. Load Game State Data
	var state string
	err = redis.MasterRedis.Do(radix.Cmd(&state, "HGET", GameHashSetName, args.GameID))
	if err != nil || len(state) <= 0 {
		return policy.RespWithError(err)
	}

	// 5. Send to Server Application
	payload := actionServerPayload{
		Relay: args.Relay,
	}

	// This needs to be done for typesafety... Might be better to do custom marshalling for this
	err = json.Unmarshal([]byte(state), &payload.State)
	if err != nil {
		return policy.RespWithError(err)
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return policy.RespWithError(err)
	}

	response, err := BytesToGame(string(payloadBytes))
	if err != nil {
		log.Printf("A Server Error Occurred: %v\n", err)
		return policy.RawUnsuccessfulResponse("Could Not Upload State to Server!")
	}

	// 6. On Success update metadata
	milli := fmt.Sprintf("%d", time.Now().UTC().Unix())
	err = redis.MasterRedis.Do(radix.Cmd(nil, "HSET", MetadataSetPrefix+args.GameID, MetadataSetLastUsed, milli))
	if err != nil {
		log.Printf("A Server Error Occurred: %v\n", err)
	}

	// Response should already be in JSON format... Let's not marshall again pls.
	return policy.RawSuccessfulResponse(response)
}

type getGameDataRequest struct {
	GameID string
}

func GetGameData(header policy.RequestHeader, bodyFactories policy.RequestBodyFactories, isSecureConnection bool) policy.CommandResponse {
	// 1. Get Game Info From Request
	err := bodyFactories.SigVerify(header.UserID, header.Sig)
	if err != nil {
		log.Printf("Unauthorized Attempt! Error: %v\n", err)
		return policy.UnSuccessfulResponse("Unauthorized!")
	}

	// 2. Get Request Data
	args := getGameDataRequest{}
	err = bodyFactories.ParseFactory(&args)
	if err != nil {
		log.Printf("Bad Argument! Error: %v\n", err)
		return policy.UnSuccessfulResponse("Bad Arguments!")
	}

	// 3. Load Game State Data
	var state string
	err = redis.MasterRedis.Do(radix.Cmd(&state, "HGET", GameHashSetName, args.GameID))
	if err != nil {
		return policy.RespWithError(err)
	} else if len(state) <= 0 {
		return policy.UnSuccessfulResponse("Game Does Not Exist")
	}

	// 4. Send to Server Application
	payload := actionServerPayload{
		Relay: map[string]interface{}{}, // Empty JSON Object
	}

	// This needs to be done for typesafety... Might be better to do custom marshalling for this
	err = json.Unmarshal([]byte(state), &payload.State)
	if err != nil {
		return policy.RespWithError(err)
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return policy.RespWithError(err)
	}

	response, err := BytesToGame(string(payloadBytes))

	// Response should already be in JSON format... Let's not marshall again pls.
	return policy.RawSuccessfulResponse(response)
}
