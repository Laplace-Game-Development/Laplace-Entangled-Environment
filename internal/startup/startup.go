package startup

import "log"

// Call a series of functions (ServerTask) and fatally log if an
// error occurs
//
// taskList :: slice of functions supplied for startup, started in order
//       from beginning to end
//
// Returns a reference to cleanup all tasks
func InitServerStartupOnTaskList(taskList []ServerTask) func() {
	length := len(taskList)
	cleanUps := make([]func(), length)

	for i, task := range taskList {
		cleanUps[i] = invokeServerStartup(task)
	}

	return func() {
		for i := length - 1; i >= 0; i-- {
			cleanUps[i]()
		}
	}
}

//// Utility Functions

// A startup function which returns a function to call when exitting/cleaning up. If instead an error is produced
// The application is expected to Fault.
type ServerTask func() (func(), error)

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
