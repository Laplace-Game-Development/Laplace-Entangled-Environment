package main

import "log"

func startGameLogic() (func(), error) {
	// 1. Execute Program
	// 2. Init Pipe/Socket to Application
	// 3. Init Shared Memory for efficient IPC
	// 4. Construct Singleton Queue

	// exec node example.js --binding=5011

	log.Println("Game Logic is not implemented!")
	return func() {}, nil
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Game Actions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func applyAction(prefix RequestPrefix, header RequestHeader, body []byte) CommandResponse {
	return unSuccessfulResponse("Command is Not Implemented!")
}

func getGameData(prefix RequestPrefix, header RequestHeader, body []byte) CommandResponse {
	return unSuccessfulResponse("Command is Not Implemented!")
}
