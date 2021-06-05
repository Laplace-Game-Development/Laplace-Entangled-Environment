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

// ZeroMQ/Task Server Settings
const NumberOfTaskWorkers uint8 = 5
const ProxyPubPort string = ":5565"
const ProxySubPort string = ":5566"
const ProxyControlPort string = ":5567"

// Configurables
const EmptyQueueSleepDuration time.Duration = time.Duration(time.Minute)
const EventHealthTaskCapacity uint8 = 50
const MagicRune rune = '~'

// Task Prefixes
const HealthTaskPrefix string = "healthTask"

// Global Variables | Singletons
var zeroMQProxyControl *zmq4.Socket = nil
var zeroMQWorkerChannelIn chan bool = nil
var zeroMQWorkerChannelOut chan bool = nil

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

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Task Queue Preparation
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Start on seperate thread due to zmq_proxy
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

var mapPrefixToWork map[string]func([]string) error = map[string]func([]string) error{
	HealthTaskPrefix: healthTaskWork,
}

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
//// Tasks Utility Functions -- Functions that help behind the scenes
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func constructTaskWithPrefix(prefix string, args []string) string {
	var builder strings.Builder

	builder.WriteString(prefix)

	for _, s := range args {
		builder.WriteRune(MagicRune)
		builder.WriteString(s)
	}

	return builder.String()
}

func parseTask(msg string) (string, []string) {
	slice := strings.Split(msg, string(MagicRune))
	return slice[0], slice[1:]
}
