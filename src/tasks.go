package main

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"syscall"
	"time"

	"github.com/mediocregopher/radix/v3"
	"github.com/pebbe/zmq4"
	"github.com/robfig/cron/v3"
)

// ZeroMQ/Task Server Settings
const numberOfTaskWorkers uint8 = 5
const zeromqMask string = "tcp://*"
const zeromqHost string = "tcp://127.0.0.1"
const proxyPubPort string = ":5565"
const proxySubPort string = ":5566"
const proxyControlPort string = ":5567"

// Table / Datastructure Names
const healthTaskQueue string = "healthTaskQueue"

// Configurables
var emptyQueueSleepDuration time.Duration = time.Duration(time.Minute)
var eventHealthTaskCapacity uint8 = 50
var magicRune rune = '~'
var staleGameDuration time.Duration = time.Duration(time.Minute * 5)

// Task Prefixes
const healthTaskPrefix string = "healthTask"

// Global Variables | Singletons
var masterZeroMQ *zmq4.Context = nil
var zeroMQProxyControl *zmq4.Socket = nil
var zeroMQWorkerChannelIn chan bool = nil
var zeroMQWorkerChannelOut chan bool = nil
var masterCron *cron.Cron = nil
var cronEntries = make(chan cron.EntryID)

func startTaskQueue() (func(), error) {
	// FOR CI:
	// TODO, it may be easier to bind these to ports assigned by the database
	// TODO, alternatively we could use smart configuration generators

	ctx, err := zmq4.NewContext()
	if err != nil {
		return nil, err
	}

	masterZeroMQ = ctx

	// Start Asynchronous proxy (costs 1 thread)
	proxyResponse := make(chan bool)

	go startAsynchronousProxy(proxyResponse)

	response := <-proxyResponse

	if !response {
		return nil, errors.New("Could not Start Proxy (Check Logs!)")
	}

	// Set a Control
	zeroMQProxyControl, err := ctx.NewSocket(zmq4.Type(zmq4.PUB))
	if err != nil {
		return nil, err
	}

	err = zeroMQProxyControl.Bind(zeromqMask + proxyControlPort)
	if err != nil {
		return nil, err
	}

	// TODO Buffer to numberOfTaskWorkers
	zeroMQWorkerChannelIn = make(chan bool)
	zeroMQWorkerChannelOut = make(chan bool)

	// Start a few workers
	for i := uint8(0); i < numberOfTaskWorkers; i++ {
		go startTaskWorker(i, zeroMQWorkerChannelIn, zeroMQWorkerChannelOut)
	}

	// Start Events/Scheduler
	err = schedule()
	if err != nil {
		return nil, err
	}

	return cleanUpTaskQueue, nil
}

func schedule() error {
	masterCron = cron.New(cron.WithSeconds())

	// Schedule Task Events Here
	entryID, err := masterCron.AddFunc("5 * * * * *", eventCheckHealth)
	if err != nil {
		return err
	}

	cronEntries <- entryID

	return nil
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Task Queue Preparation
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Start on seperate thread due to zmq_proxy
func startAsynchronousProxy(resp chan bool) {
	ctx := masterZeroMQ

	// Worker-Facing Publisher
	zmqXPUB, err := ctx.NewSocket(zmq4.Type(zmq4.XPUB))
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	err = zmqXPUB.Bind(zeromqMask + proxyPubPort)
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	// Callsite socket
	zmqXSUB, err := ctx.NewSocket(zmq4.Type(zmq4.XSUB))
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	err = zmqXSUB.Bind(zeromqMask + proxySubPort)
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	// Interrupt socket
	zmqCON, err := ctx.NewSocket(zmq4.Type(zmq4.SUB))
	if err != nil {
		log.Println(err)
		resp <- false
		return
	}

	err = zmqCON.Connect(zeromqHost + proxyControlPort)
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

func startTaskWorker(id uint8, signalChannel chan bool, responseChannel chan bool) {
	// Startup
	subSocket, err := zmq4.NewSocket(zmq4.Type(zmq4.SUB))
	if err != nil {
		log.Printf("Could Not Start Task Worker! ID: %d\n", id)
		log.Println(err)
		return
	}

	err = subSocket.Connect(zeromqHost + proxyPubPort)
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
			time.Sleep(emptyQueueSleepDuration)
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

func cleanUpTaskQueue() {
	log.Println("Signalling Task Workers for CleanUp")

	for i := uint8(0); i < numberOfTaskWorkers; i++ {
		zeroMQWorkerChannelIn <- true
	}

	log.Println("Signalling Finished Waiting for response!")
	for i := uint8(0); i < numberOfTaskWorkers; i++ {
		<-zeroMQWorkerChannelOut
	}

	log.Println("Task Workers Cleanup Complete!")

	log.Println("Cleaning Cron Jobs!")
	ctx := masterCron.Stop()
	select {
	case <-ctx.Done():
		log.Println(ctx.Err())
	default:
	}
	log.Println("Cron Jobs Clean!")
}

func sendTaskToWorkers(msgs []string) error {
	// Proxy Facing Publisher
	zmqPUB, err := masterZeroMQ.NewSocket(zmq4.Type(zmq4.PUB))
	if err != nil {
		return err
	}

	err = zmqPUB.Connect(zeromqHost + proxySubPort)
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
//// Tasks Submissions -- Public Interface Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func submitGameForHealthCheck(authID string, gameID string) error {
	err := masterRedis.Do(radix.Cmd(nil, "RPUSH", healthTaskQueue, gameID))
	if err != nil {
		return err
	}

	return nil
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Tasks Events -- Cron Subscribed Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func eventCheckHealth() {
	gameIDSlice := make([]string, eventHealthTaskCapacity)
	gameIDSlicePrefixed := make([]string, eventHealthTaskCapacity)

	err := masterRedis.Do(radix.Cmd(&gameIDSlice, "LPOP", healthTaskQueue, fmt.Sprintf("%d", eventHealthTaskCapacity)))
	if err != nil {
		log.Fatalln("Trouble Using Health Event: " + err.Error())
	}

	if len(gameIDSlice) == 0 {
		return
	}

	temp := make([]string, 1)

	for i, s := range gameIDSlice {
		temp[0] = s
		gameIDSlicePrefixed[i] = constructTaskWithPrefix(healthTaskPrefix, temp)
	}

	err = sendTaskToWorkers(gameIDSlice)
	if err != nil {
		log.Fatalf("Trouble Using Health Event! Error: %v", err.Error())
	}
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Tasks Working -- Parsed Worker Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func onTask(msg string) error {
	log.Println("Got Message! | " + msg)
	if len(msg) <= 0 {
		log.Println("Message was empty!")
		return nil
	}

	task, args := parseTask(msg)

	if task == healthTaskPrefix {
		return healthTaskWork(args)
	} else {
		return errors.New("Unknown Task Sent to Task Worker! MSG: " + msg)
	}
}

func healthTaskWork(args []string) error {
	if len(args) < 1 {
		return errors.New("Task Did Not Receive Game ID!")
	}

	gameTime, err := getRoomHealth(args[0])
	if err != nil {
		return err
	}

	if gameTime.Add(staleGameDuration).Before(time.Now()) {
		// TODO Generate Sig for Game Deletion
		resp := deleteGame(
			RequestPrefix{Command: cmdGameDelete},
			RequestHeader{UserID: "-1", Sig: "REALLY RANDOM STRING"},
			[]byte(""))
		if resp.ServerError != nil {
			return resp.ServerError
		}

	} else {
		// TODO replace 0 with super user
		submitGameForHealthCheck("0", args[0])
	}

	return nil
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Tasks Utility Functions -- Funcitons that help behind the scenes
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func constructTaskWithPrefix(prefix string, args []string) string {
	var builder strings.Builder

	builder.WriteString(prefix)

	for _, s := range args {
		builder.WriteRune(magicRune)
		builder.WriteString(s)
	}

	return builder.String()
}

func parseTask(msg string) (string, []string) {
	slice := strings.Split(msg, string(magicRune))
	return slice[0], slice[1:]
}
