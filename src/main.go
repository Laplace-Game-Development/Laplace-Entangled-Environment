package main

import (
	"bufio"
	"log"
	"os"
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
		cleanup := invokeServerStartup(task)
		defer cleanup()
	}

	bufio.NewReader(os.Stdin).ReadBytes('\n')
	log.Println("Cleaning Up!")
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
