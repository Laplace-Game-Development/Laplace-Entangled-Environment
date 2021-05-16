package main

import (
	"log"
)

type ServerTask func() (func(), error)

var serverInitTaskList []ServerTask = []ServerTask{
	startGameLogic,
	startDatabase,
	startEncryption,
	startRoomsSystem,
	startTaskQueue,
	startCronScheduler,
	startListener, // Dependent on startEncryption
}

func main() {
	for _, task := range serverInitTaskList {
		defer invokeServerStartup(task)
	}
}

//// Utility Functions
func invokeServerStartup(fn ServerTask) func() {
	cleanUp, err := fn()

	if err != nil || cleanUp == nil {
		log.Println("Trouble Starting Server!")
		log.Fatalln(err)
	}

	return cleanUp
}
