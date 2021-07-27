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

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/policy"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/zeromq"
	"github.com/mediocregopher/radix/v3"
	"github.com/pebbe/zmq4"
)

//// Configurables

// Time to wait for Game to Finish cleaning up
const WaitDurationForGameStop time.Duration = 10 * time.Second

// Time to Wait For Game to Respond to an Action
const WaitDurationForGameAction time.Duration = 3 * time.Second

// Commands For Initialization

// Shell Command to execute
const CommandToExec string = "node"

// Game Port Number
const GamePortNum string = "5011"

// Game Port Number (prefixed with colon)
const GamePort string = ":" + GamePortNum

// Shell Command Args
// This value should not change at runtime
var CommandArgs []string = []string{"./node-layer/index.js", "--binding=" + GamePortNum}

//// Global Variables | Singletons

// Game Context (start stop signals)
var commandContext context.Context = nil

// Context Cancel Function
var cancelFunc func() = nil

// ServerTask Startup Function for the third-party Game application.
// Takes care of initialization. returns an error if the
// game can't be started (i.e. prerequisites are not met)
func StartGameLogic() (func(), error) {
	executeCommand()
	return cleanUpGameLogic, nil
}

// Cleanup Logic. Tries to terminate game, exits if it doesn't quit in
// a timely manner. Reports error if it could not close game.
func cleanUpGameLogic() {
	log.Println("Cleaning Up Game Logic")
	cancelFunc()

	deadlineForStop := time.Now().Add(WaitDurationForGameStop)
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

// Wrapper and secure configuration for os/exec
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

// Send a string of bytes to the third party application using ZeroMQ
// (The Game SDK will take care of setting up the "server" part of
// communication. We just connect and send a string, waiting for a
// a response). Thread Safe with ZeroMQ!
//
// dataIn :: string to sent to game (usually a JSON.)
//
// returns -> string :: response from third-party game
//         -> error :: non-nil if it couldn't send data
//                to the game.
func BytesToGame(dataIn string) (string, error) {
	// Create a Zeromq Request Port
	req, err := zeromq.MainZeroMQ.NewSocket(zmq4.Type(zmq4.REQ))
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

// Receive a string of bytes from the game.(This is
// used with BytesToGame and there should not be a
// need to call this function)
//
// req :: ZeroMQ Request Socket
//
// returns -> string :: response from third-party game
//         -> error :: non-nil if it couldn't receive
//                data from game
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

// JSON Fields for the Apply Action Command Args
type applyActionRequest struct {
	GameID string
	Relay  map[string]interface{}
}

// JSON Fields for marshalling a JSON to the Game
type actionServerPayload struct {
	State map[string]interface{}
	Relay map[string]interface{}
}

// The Apply Action Endpoint sends the payload to the game.
// This will be the most highly used endpoint as this represents
// the main transport method to games. The Game actually runs
// the code, but the application loads the data for the
// game from the database.
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
	err = redis.MainRedis.Do(radix.Cmd(&state, "HGET", GameHashSetName, args.GameID))
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
	err = redis.MainRedis.Do(radix.Cmd(nil, "HSET", MetadataSetPrefix+args.GameID, MetadataSetLastUsed, milli))
	if err != nil {
		log.Printf("A Server Error Occurred: %v\n", err)
	}

	// Response should already be in JSON format... Let's not marshall again pls.
	return policy.RawSuccessfulResponse(response)
}

// JSON Fields for the GetGameData Command Args
type getGameDataRequest struct {
	GameID string
}

// The Get Game Data Endpoint gathers all the data
// in the database for a game. The Games are public
// by default so anyone should be able to observe
//
// However, observers cannot change the game in
// any way. You have to be on the roster to apply
// an action
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
	err = redis.MainRedis.Do(radix.Cmd(&state, "HGET", GameHashSetName, args.GameID))
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
