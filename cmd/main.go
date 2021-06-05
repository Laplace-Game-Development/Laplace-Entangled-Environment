package main

import (
	"bufio"
	"log"
	"os"

	"laplace-entangled-env.com/internal/data"
	"laplace-entangled-env.com/internal/redis"
	"laplace-entangled-env.com/internal/route"
	"laplace-entangled-env.com/internal/schedule"
	"laplace-entangled-env.com/internal/zeromq"
)

type ServerTask func() (func(), error)

var serverInitTaskList []ServerTask = []ServerTask{
	// Most Systems are Dependent on these First Two Systems
	redis.StartDatabase,
	zeromq.StartZeroMqComms,
	////////////////////////
	route.StartEncryption,
	data.StartRoomsSystem,
	schedule.StartTaskQueue,
	schedule.StartCronScheduler,
	data.StartGameLogic, // Dependent on startTaskQueue
	route.StartListener, // Dependent on startEncryption
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
