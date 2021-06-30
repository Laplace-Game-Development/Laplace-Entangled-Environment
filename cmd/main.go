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
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/startup"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/zeromq"
)

// List of "ServerTask" functions that need to be run to start the application.
// WARNING: Some of the tasks are dependent on the startup of other tasks
var serverInitTaskList []startup.ServerTask = []startup.ServerTask{
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
	cleanup := startup.InitServerStartupOnTaskList(serverInitTaskList)
	defer cleanup()

	log.Println("Press Enter To Exit!")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
	log.Println("Cleaning Up!")
}
