// Laplace-Entanglement-Environment Application Package. This represents all the modules necessary
// to run the application. This service provides a realtime server for multiple games at once
// using a database, TCP, and HTTP.
package main

import (
	"bufio"
	"log"
	"os"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/data"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/route"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/schedule"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/zeromq"
)

// A startup function which returns a function to call when exitting/cleaning up. If instead an error is produced
// The application is expected to Fault.
type ServerTask func() (func(), error)

// List of "ServerTask" functions that need to be run to start the application.
// WARNING: Some of the tasks are dependent on the startup of other tasks
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

// Entry Function
func main() {
	for _, task := range serverInitTaskList {
		cleanup := invokeServerStartup(task)
		defer cleanup()
	}

	bufio.NewReader(os.Stdin).ReadBytes('\n')
	log.Println("Cleaning Up!")
}

//// Utility Functions

// Consumer for "Server Tasks". Makes sure to fault on error and return a
// cleanup function on success.
func invokeServerStartup(fn ServerTask) func() {
	cleanUp, err := fn()

	if err != nil || cleanUp == nil {
		log.Println("Trouble Starting Server!")
		log.Fatalln(err)
	}

	return cleanUp
}
