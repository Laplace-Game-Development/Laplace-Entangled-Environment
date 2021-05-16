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

func applyAction(header RequestHeader, bodyFactories RequestBodyFactories, isSecureConnection bool) CommandResponse {
	return unSuccessfulResponse("Command is Not Implemented!")
}

func getGameData(header RequestHeader, bodyFactories RequestBodyFactories, isSecureConnection bool) CommandResponse {
	return unSuccessfulResponse("Command is Not Implemented!")
}
