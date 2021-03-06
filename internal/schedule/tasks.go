package schedule

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"syscall"
	"time"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/data"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/event"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/policy"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/zeromq"
	"github.com/mediocregopher/radix/v3"
	"github.com/pebbe/zmq4"
)

//// Configurables

// Number of Workers to Distribute to with ZeroMQ
const NumberOfTaskWorkers uint8 = 10

// Proxy Publish Port for sending to (application)
const ProxyFEPort string = ":5565"

// Proxy Sub Port for receiving From (workers)
const ProxyBEPort string = ":5566"

// Proxy Control Port for Interrupting Proxy
const ProxyControlPort string = ":5567"

// Time that a Worker should sleep/wait in the event
// of no tasks being ready
const EmptyQueueSleepDuration time.Duration = time.Duration(time.Second * time.Duration(NumberOfTaskWorkers))

// Time that a worker should spend waiting to receive
// for new work
const RecvIOTimeoutDuration time.Duration = time.Duration(time.Microsecond)

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

// Unit Testing Prefix for adding to the database using workers
const TestTaskPrefix string = "unitTest0"

//// Global Variables | Singletons

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

	log.Println("Clearing Proxy")

	// Set a Control
	zeroMQProxyControl, err := zeromq.MainZeroMQ.NewSocket(zmq4.Type(zmq4.REQ))
	if err != nil {
		log.Fatalf("Error Clearing Proxy! Err: %v\n", err)
	}

	err = zeroMQProxyControl.Bind(zeromq.ZeromqMask + ProxyControlPort)
	if err != nil {
		log.Fatalf("Error Clearing Proxy! Err: %v\n", err)
	}

	zeroMQProxyControl.Send("TERMINATE", zmq4.Flag(0))
	zeroMQProxyControl.Recv(zmq4.Flag(0))

	log.Println("Clearing Proxy Complete!")
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Task Queue Preparation
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Starts the Proxy Communication Layer for a 0MQ Queue Device.
// The Application communicates to this block the tasks and data that need
// to be worked on. The Proxy then distributes
// the data using ZeroMQ's distribution algorithms.
//
// resp :: boolean channel which is used to return success/failure
//  (since the worker loops forever)
//
// Should be called in a goroutine otherwise zmq_proxy will block the main "thread"
func startAsynchronousProxy(resp chan bool) {
	// Worker-Facing Publisher/ROUTER
	zmqXREP, err := zeromq.MainZeroMQ.NewSocket(zmq4.Type(zmq4.ROUTER))
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	err = zmqXREP.Bind(zeromq.ZeromqMask + ProxyFEPort)
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	// Callsite socket
	zmqXREQ, err := zeromq.MainZeroMQ.NewSocket(zmq4.Type(zmq4.DEALER))
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	err = zmqXREQ.Bind(zeromq.ZeromqMask + ProxyBEPort)
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	// Interrupt socket
	// Use a Router here for STATISTICS as a potential return rather than
	// documentation's reported SUB socket.
	zmqCON, err := zeromq.MainZeroMQ.NewSocket(zmq4.Type(zmq4.REP))
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

	resp <- true

	// Will Run Forever
	err = zmq4.ProxySteerable(zmqXREQ, zmqXREP, nil, zmqCON)
	if err != nil {
		// This cannot be Fatal otherwise test will fail
		log.Printf("Proxy Ended with Error! Err: %v\n", err)
	}

	log.Println("Proxy Signaled To Be Terminated!")
	zmqCON.Send("OK", zmq4.Flag(0))

	zmqXREQ.Close()
	zmqXREP.Close()
	zmqCON.Close()
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
	subSocket, err := zmq4.NewSocket(zmq4.Type(zmq4.REP))
	if err != nil {
		log.Printf("Could Not Start Task Worker! ID: %d\n", id)
		log.Println(err)
		return
	}

	err = subSocket.Connect(zeromq.ZeromqHost + ProxyBEPort)
	if err != nil {
		log.Printf("Could Not Start Task Worker! ID: %d\n", id)
		log.Println(err)
		return
	}

	err = subSocket.SetRcvtimeo(time.Duration(time.Second))
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
			subSocket.Send("OK", zmq4.DONTWAIT)
			onTask(msg)
			subSocket.Send("DONE", zmq4.DONTWAIT)
		}

		select {
		case <-signalChannel:
			responseChannel <- true
			log.Printf("Cleaning Up Thread ID: %d\n", id)
			subSocket.Close()
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
func SendTasksToWorkers(msgs ...string) error {
	// Proxy Facing Publisher
	zmqREQ, err := zeromq.MainZeroMQ.NewSocket(zmq4.Type(zmq4.DEALER))
	if err != nil {
		return err
	}

	log.Println("Connecting to Worker Proxy!")
	err = zmqREQ.Connect(zeromq.ZeromqHost + ProxyFEPort)
	if err != nil {
		return err
	}

	count := 0
	for _, msg := range msgs {
		if len(msg) == 0 {
			break
		}

		if count > 0 {
			time.Sleep(time.Second)
		}

		log.Println("Sending Delimitter")
		_, err := zmqREQ.Send("", zmq4.SNDMORE)
		if err != nil {
			return err
		}

		log.Printf("Sending Message: %s\n", msg)
		num, err := zmqREQ.Send(msg, zmq4.Flag(0))
		if err != nil {
			return err
		} else if len(msg) != num {
			return errors.New("ZeroMQ did not Accept Full Job! Characters Accepted:" + fmt.Sprintf("%d", num))
		}

		count += 1
	}

	log.Println("Waiting for Confirmations")
	for i := 0; i < count; i++ {
		_, err = zmqREQ.Recv(zmq4.Flag(0))
		if err != nil {
			return err
		}
		log.Printf("Confirmation Received!")
	}
	log.Println("Received all Confirmations")

	log.Println("Closing connection to Worker Proxy!")
	err = zmqREQ.Close()
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
	TestTaskPrefix:   testTaskWork,
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
		superUserRequest, err := policy.RequestWithSuperUser(true, policy.CmdGameDelete, data.SelectGameArgs{GameID: args[0]})
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

// Adds a given set of arguments to the redis database for testing
//
// args:: the data from the msg
// args[0] :: redis Set Key
// args[1] :: string value
//
// Only For Unit Testing
func testTaskWork(args []string) error {
	if len(args) < 2 {
		return errors.New("Task Did Not Receive Set Key and Value!")
	}

	log.Printf("Unit Test Work Running!\nAdding %s to %s\n", args[0], args[1])
	return redis.MainRedis.Do(radix.Cmd(nil, "SADD", args[0], args[1]))
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Tasks Utility Functions -- Functions that help when creating and submitting tasks
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Constructs a string by joining the prefix and args with magic runes
// i.e. result = prefix + MagicRune + arg0 + MagicRune + arg1 + [MagicRune + argN] ...
func constructTaskWithPrefix(prefix string, args ...string) string {
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
