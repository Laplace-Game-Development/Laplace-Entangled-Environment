package zeromq

import (
	"log"

	"github.com/pebbe/zmq4"
)

// Configurables
const ZeromqMask string = "tcp://*"
const ZeromqHost string = "tcp://127.0.0.1"

// Global Variables | Singletons
var MasterZeroMQ *zmq4.Context = nil

func StartZeroMqComms() (func(), error) {
	ctx, err := zmq4.NewContext()
	if err != nil {
		return nil, err
	}

	MasterZeroMQ = ctx

	return cleanUpZeroMq, nil
}

func cleanUpZeroMq() {
	log.Println("Terminating ZeroMQ!")

	err := MasterZeroMQ.Term()
	if err != nil {
		log.Fatalf("ZeroMQ Cannot Be Terminated! Error: %v\n", err)
	}

	log.Println("ZeroMQ Terminated")
}
