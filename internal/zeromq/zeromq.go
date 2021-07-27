// The ZeroMQ module is the holder and initializer for the third-party ZeroMQ Library.
package zeromq

import (
	"log"

	"github.com/pebbe/zmq4"
)

//// Configurables

// ZeroMQ URI Host Binding Mask
const ZeromqMask string = "tcp://*"

// ZeroMQ URI Client Connection IPs
const ZeromqHost string = "tcp://127.0.0.1"

//// Global Variables | Singletons

// ZeroMQ Context Reference -- Defined on Startup
var MainZeroMQ *zmq4.Context = nil

// ZeroMQ Server Task. Starts ZeroMQ and Defines Context
func StartZeroMqComms() (func(), error) {
	ctx, err := zmq4.NewContext()
	if err != nil {
		return nil, err
	}

	MainZeroMQ = ctx

	return cleanUpZeroMq, nil
}

// ZeroMQ Cleanup. Terminates ZeroMQ Context
func cleanUpZeroMq() {
	log.Println("Terminating ZeroMQ!")

	// This caused infinite loops on cleanup with some sockets
	// err := MainZeroMQ.Term()
	// if err != nil {
	// 	log.Fatalf("ZeroMQ Cannot Be Terminated! Error: %v\n", err)
	// }

	log.Println("ZeroMQ Terminated")
}
