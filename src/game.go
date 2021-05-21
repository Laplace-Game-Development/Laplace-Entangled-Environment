package main

import (
	"context"
	"log"
	"os/exec"
	"time"
)

// Configurables
const waitDurationForGame time.Duration = 10 * time.Second
const commandToExec string = "node"

var commandArgs []string = []string{"index.js", "--binding=5011"}

// Global Variables | Singletons
var commandContext context.Context = nil
var cancelFunc func() = nil

func startGameLogic() (func(), error) {
	// 4. Construct Singleton Queue

	executeCommand()
	return cleanUpGameLogic, nil
}

func cleanUpGameLogic() {
	log.Println("Cleaning Up Game Logic")
	cancelFunc()

	deadlineForStop := time.Now().Add(waitDurationForGame)
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

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Game Actions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func applyAction(header RequestHeader, bodyFactories RequestBodyFactories, isSecureConnection bool) CommandResponse {
	return unSuccessfulResponse("Command is Not Implemented!")
}

func getGameData(header RequestHeader, bodyFactories RequestBodyFactories, isSecureConnection bool) CommandResponse {
	return unSuccessfulResponse("Command is Not Implemented!")
}
