package schedule

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"syscall"
	"time"

	"github.com/pebbe/zmq4"
	"laplace-entangled-env.com/internal/data"
	"laplace-entangled-env.com/internal/event"
	"laplace-entangled-env.com/internal/policy"
	"laplace-entangled-env.com/internal/route"
	"laplace-entangled-env.com/internal/zeromq"
)

//// Configurables

// Number of Workers to Distribute to with ZeroMQ
const NumberOfTaskWorkers uint8 = 5

// Proxy Publish Port for sending to (application)
const ProxyPubPort string = ":5565"

// Proxy Sub Port for receiving From (workers)
const ProxySubPort string = ":5566"

// Proxy Control Port for Interrupting Proxy
const ProxyControlPort string = ":5567"

// Time that a Worker should sleep/wait in the event
// of no tasks being ready
const EmptyQueueSleepDuration time.Duration = time.Duration(time.Minute)

// Number of Event Health Tasks to pop off of redis
// Maybe this should be in schedule.go
const EventHealthTaskCapacity uint8 = 50

// Labeled MagicRune as a joke for Golangs named type
// Used as seperator for communicating work to workers
// Task Name/Prefix + MagicRune + Params/Data
const MagicRune rune = '~'

//// Task Name/Prefixes

// Game Health Checking to garbage collect game data
const HealthTaskPrefix string = "healthTask"

//// Global Variables | Singletons

// Control Referency For Proxy Control Port
var zeroMQProxyControl *zmq4.Socket = nil

// Control Communication for Workers Input
// Used For Cleanup
var zeroMQWorkerChannelIn chan bool = nil

// Control Communication for Workers Output
// Used For Cleanup
var zeroMQWorkerChannelOut chan bool = nil

// ServerTask Startup Function for Schedule Task System. Creates Workers
// and communication channels with ZeroMQ.
func StartTaskQueue() (func(), error) {
	// FOR CI:
	// TODO, it may be easier to bind these to ports assigned by the database
	// TODO, alternatively we could use smart configuration generators

	// Start Asynchronous proxy (costs 1 thread)
	proxyResponse := make(chan bool)

	go startAsynchronousProxy(proxyResponse)

	response := <-proxyResponse

	if !response {
		return nil, errors.New("Could not Start Proxy (Check Logs!)")
	}

	// Set a Control
	zeroMQProxyControl, err := zeromq.MasterZeroMQ.NewSocket(zmq4.Type(zmq4.PUB))
	if err != nil {
		return nil, err
	}

	err = zeroMQProxyControl.Bind(zeromq.ZeromqMask + ProxyControlPort)
	if err != nil {
		return nil, err
	}

	// TODO Buffer to numberOfTaskWorkers
	zeroMQWorkerChannelIn = make(chan bool)
	zeroMQWorkerChannelOut = make(chan bool)

	// Start a few workers
	for i := uint8(0); i < NumberOfTaskWorkers; i++ {
		go startTaskWorker(i, zeroMQWorkerChannelIn, zeroMQWorkerChannelOut)
	}

	return cleanUpTaskQueue, nil
}

// CleanUp Function returned by Startup function. Signals Workers to Finish and
// blocks until completion.
func cleanUpTaskQueue() {
	log.Println("Signalling Task Workers for CleanUp")

	for i := uint8(0); i < NumberOfTaskWorkers; i++ {
		zeroMQWorkerChannelIn <- true
	}

	log.Println("Signalling Finished Waiting for response!")
	for i := uint8(0); i < NumberOfTaskWorkers; i++ {
		<-zeroMQWorkerChannelOut
	}

	log.Println("Task Workers Cleanup Complete!")
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Task Queue Preparation
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Starts the Proxy Communication Layer. The Application communicates to this
// block the tasks and data that need to be worked on. The Proxy then distributes
// the data using ZeroMQ's distribution algorithms.
//
// resp :: boolean channel which is used to return success/failure
//  (since the worker loops forever)
//
// Should be called in a goroutine otherwise zmq_proxy will block the main "thread"
func startAsynchronousProxy(resp chan bool) {
	// Worker-Facing Publisher
	zmqXPUB, err := zeromq.MasterZeroMQ.NewSocket(zmq4.Type(zmq4.XPUB))
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	err = zmqXPUB.Bind(zeromq.ZeromqMask + ProxyPubPort)
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	// Callsite socket
	zmqXSUB, err := zeromq.MasterZeroMQ.NewSocket(zmq4.Type(zmq4.XSUB))
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	err = zmqXSUB.Bind(zeromq.ZeromqMask + ProxySubPort)
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	// Interrupt socket
	zmqCON, err := zeromq.MasterZeroMQ.NewSocket(zmq4.Type(zmq4.SUB))
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	err = zmqCON.Connect(zeromq.ZeromqHost + ProxyControlPort)
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	err = zmqCON.SetSubscribe("")
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	resp <- true

	// Will Run Forever
	err = zmq4.ProxySteerable(zmqXSUB, zmqXPUB, nil, zmqCON)
	if err != nil {
		log.Fatal(err)
		return
	}
}

// Starts a worker with the given ID. The worker receives and consumes
// work from the proxy. It then performs the required event if it recongizes
// the Task Name/Prefix.
//
// Any errors in parsing or working the task is logged.
//
// id :: numerical id for logging
// signalChannel :: channel for control signal for cleanup
// responseChannel :: an output channel when a signal is received for cleanup
//
// The function is contructed to loop forever until signaled otherwise.
// This function should be run with a goroutine.
func startTaskWorker(id uint8, signalChannel chan bool, responseChannel chan bool) {
	// Startup
	subSocket, err := zmq4.NewSocket(zmq4.Type(zmq4.SUB))
	if err != nil {
		log.Printf("Could Not Start Task Worker! ID: %d\n", id)
		log.Println(err)
		return
	}

	err = subSocket.Connect(zeromq.ZeromqHost + ProxyPubPort)
	if err != nil {
		log.Printf("Could Not Start Task Worker! ID: %d\n", id)
		log.Println(err)
		return
	}

	err = subSocket.SetRcvtimeo(time.Duration(time.Microsecond * 30))
	if err != nil {
		log.Printf("Could Not Start Task Worker! ID: %d\n", id)
		log.Println(err)
		return
	}

	// Consume Tasks
	for {
		msg, err := subSocket.Recv(zmq4.Flag(zmq4.DONTWAIT))
		if err != nil && zmq4.AsErrno(err) == zmq4.Errno(syscall.EAGAIN) {
			log.Printf("Nothing to Consume! ID: %d\n", id)
			time.Sleep(EmptyQueueSleepDuration)
		} else if err != nil {
			log.Printf("Error Upon Consuming! ID: %d\n", id)
			log.Println(err)

			log.Printf("Cleaning Up Thread ID: %d\n", id)
			return
		} else {
			onTask(msg)
		}

		select {
		case <-signalChannel:
			responseChannel <- true
			return
		default:
		}
	}

}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Tasks Communication -- Communicating Tasks and Data to Proxy
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Function used by schedule.go to communicate the scheduled tasks to the workers.
// This takes an array of Task Name/Prefixes+MagicRune+Data strings and sends them
// to the proxy (which in turn send them to the workers.).
//
// msg :: slice of string messages to send to workers (Task Name/Prefix+MagicRune+Data)
//
// Returns an Error if communicating fails (server-shutoff or the full message could
// not be sent).
func sendTasksToWorkers(msgs []string) error {
	// Proxy Facing Publisher
	zmqPUB, err := zeromq.MasterZeroMQ.NewSocket(zmq4.Type(zmq4.PUB))
	if err != nil {
		return err
	}

	err = zmqPUB.Connect(zeromq.ZeromqHost + ProxySubPort)
	if err != nil {
		return err
	}

	for _, msg := range msgs {
		// We May Possibly Not Want To Wait Here
		num, err := zmqPUB.Send(msg, zmq4.Flag(0))
		if err != nil {
			return err
		} else if len(msg) != num {
			return errors.New("ZeroMQ did not Accept Full Job! Characters Accepted:" + fmt.Sprintf("%d", num))
		}
	}

	err = zmqPUB.Close()
	if err != nil {
		return err
	}

	return nil
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Tasks Working -- Parsed Worker Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Map of Strings to their specific events. The Functions are "work" functions which load the
// required data to call a function in the event module. These are called and used by workers.
var mapPrefixToWork map[string]func([]string) error = map[string]func([]string) error{
	HealthTaskPrefix: healthTaskWork,
}

// Parses the given message and runs the associated function based on "mapPrefixToWork"
// Parsing errors are returned as an error.
//
// msg :: string message for working (Task Name/Prefix+MagicRune+Data)
func onTask(msg string) error {
	log.Println("Got Message! | " + msg)
	if len(msg) <= 0 {
		log.Println("Message was empty!")
		return nil
	}

	task, args := parseTask(msg)

	work, exists := mapPrefixToWork[task]

	if !exists {
		return errors.New("Unknown Task Sent to Task Worker! MSG: " + msg)
	}

	return work(args)
}

// Performs the loading required for game garbage collection. It then calls
// the Game Health Check Event with the proper args.
//
// args :: the data from the msg. This should be a
func healthTaskWork(args []string) error {
	if len(args) < 1 {
		return errors.New("Task Did Not Receive Game ID!")
	}

	gameTime, err := data.GetRoomHealth(args[0])
	if err != nil {
		return err
	}

	if gameTime.Add(policy.StaleGameDuration).Before(time.Now().UTC()) {
		superUserRequest, err := route.RequestWithSuperUser(true, policy.CmdGameDelete, data.SelectGameArgs{GameID: args[0]})
		if err != nil {
			return err
		}

		resp := data.DeleteGame(superUserRequest.Header, superUserRequest.BodyFactories, superUserRequest.IsSecureConnection)
		if resp.ServerError != nil {
			return resp.ServerError
		}

	} else {
		event.SubmitGameForHealthCheck(args[0])
	}

	return nil
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Tasks Utility Functions -- Functions that help when creating and submitting tasks
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Constructs a string by joining the prefix and args with magic runes
// i.e. result = prefix + MagicRune + arg0 + MagicRune + arg1 + [MagicRune + argN] ...
func constructTaskWithPrefix(prefix string, args []string) string {
	var builder strings.Builder

	builder.WriteString(prefix)

	for _, s := range args {
		builder.WriteRune(MagicRune)
		builder.WriteString(s)
	}

	return builder.String()
}

// Parses a Task based on a MagicRune delimited string.
// Returns the Prefix and the slice of MagicRune Delimited args.
func parseTask(msg string) (string, []string) {
	slice := strings.Split(msg, string(MagicRune))
	return slice[0], slice[1:]
}
