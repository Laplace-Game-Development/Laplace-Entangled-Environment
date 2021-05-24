package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"time"

	"github.com/mediocregopher/radix/v3"
	"github.com/pebbe/zmq4"
)

// Configurables
const waitDurationForGameStart time.Duration = 10 * time.Second
const waitDurationForGameAction time.Duration = 3 * time.Second
const commandToExec string = "node"
const gamePortNum string = "5011"
const gamePort string = ":" + gamePortNum

var commandArgs []string = []string{"index.js", "--binding=5011"}

// Global Variables | Singletons
var commandContext context.Context = nil
var cancelFunc func() = nil

func startGameLogic() (func(), error) {
	executeCommand()
	return cleanUpGameLogic, nil
}

func cleanUpGameLogic() {
	log.Println("Cleaning Up Game Logic")
	cancelFunc()

	deadlineForStop := time.Now().Add(waitDurationForGameStart)
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

	cmd := exec.CommandContext(commandContext, commandToExec, commandArgs...)

	// Making sure to nil these for security reasons
	cmd.Stdout = nil
	cmd.Stderr = nil

	go func() {
		err := cmd.Run()
		if err != nil {
			log.Printf("Error Recieved From Game %v\n", err)
		}
	}()
}

func bytesToGame(dataIn string) (string, error) {
	// Create a Zeromq Request Port
	req, err := masterZeroMQ.NewSocket(zmq4.Type(zmq4.REQ))
	if err != nil {
		return "", err
	}

	// Not necessary, but good practice
	defer req.Close()

	err = req.Connect(zeromqHost + gamePort)
	if err != nil {
		return "", err
	}

	num, err := req.Send(dataIn, zmq4.Flag(0))
	if err != nil {
		return "", err
	} else if len(dataIn) != num {
		return "", errors.New("ZeroMQ did not Accept Full Job! Characters Accepted:" + fmt.Sprintf("%d", num))
	}

	return bytesFromGame(req)
}

func bytesFromGame(req *zmq4.Socket) (string, error) {
	poller := zmq4.NewPoller()

	poller.Add(req, zmq4.POLLIN)
	sockets, err := poller.Poll(waitDurationForGameAction)
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

func applyAction(header RequestHeader, bodyFactories RequestBodyFactories, isSecureConnection bool) CommandResponse {
	// 1. Verify Request
	err := bodyFactories.sigVerify(header.UserID, header.Sig)
	if err != nil {
		log.Printf("Unauthorized Attempt! Error: %v\n", err)
		return unSuccessfulResponse("Unauthorized!")
	}

	// 2. Get Request Data
	args := applyActionRequest{}
	err = bodyFactories.parseFactory(&args)
	if err != nil {
		log.Printf("Bad Argument! Error: %v\n", err)
		return unSuccessfulResponse("Bad Arguments!")
	}

	// 3. Verify User is In Game
	isInGame, err := isUserInGame(header.UserID, args.GameID)
	if err != nil {
		log.Printf("Error Verifying User is in game: %v\n", err)
		return unSuccessfulResponse("User Not In Game")
	} else if !isInGame {
		return unSuccessfulResponse("User Not In Game")
	}

	// 4. Load Game State Data
	var state string
	err = masterRedis.Do(radix.Cmd(&state, "HGET", gameHashSetName, args.GameID))
	if err != nil || len(state) <= 0 {
		return respWithError(err)
	}

	// 5. Send to Server Application
	payload := actionServerPayload{
		Relay: args.Relay,
	}

	// This needs to be done for typesafety... Might be better to do custom marshalling for this
	err = json.Unmarshal([]byte(state), &payload.State)
	if err != nil {
		return respWithError(err)
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return respWithError(err)
	}

	response, err := bytesToGame(string(payloadBytes))

	// Response should already be in JSON format... Let's not marshall again pls.
	return rawSuccessfulResponse(response)
}

type getGameDataRequest struct {
	GameID string
}

func getGameData(header RequestHeader, bodyFactories RequestBodyFactories, isSecureConnection bool) CommandResponse {
	// 1. Get Game Info From Request
	err := bodyFactories.sigVerify(header.UserID, header.Sig)
	if err != nil {
		log.Printf("Unauthorized Attempt! Error: %v\n", err)
		return unSuccessfulResponse("Unauthorized!")
	}

	// 2. Get Request Data
	args := getGameDataRequest{}
	err = bodyFactories.parseFactory(&args)
	if err != nil {
		log.Printf("Bad Argument! Error: %v\n", err)
		return unSuccessfulResponse("Bad Arguments!")
	}

	// 3. Load Game State Data
	var state string
	err = masterRedis.Do(radix.Cmd(&state, "HGET", gameHashSetName, args.GameID))
	if err != nil {
		return respWithError(err)
	} else if len(state) <= 0 {
		return unSuccessfulResponse("Game Does Not Exist")
	}

	// 4. Send to Server Application
	payload := actionServerPayload{
		Relay: map[string]interface{}{}, // Empty JSON Object
	}

	// This needs to be done for typesafety... Might be better to do custom marshalling for this
	err = json.Unmarshal([]byte(state), &payload.State)
	if err != nil {
		return respWithError(err)
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return respWithError(err)
	}

	response, err := bytesToGame(string(payloadBytes))

	// Response should already be in JSON format... Let's not marshall again pls.
	return rawSuccessfulResponse(response)
}
