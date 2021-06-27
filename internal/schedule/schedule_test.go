package schedule

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/event"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/zeromq"
	"github.com/mediocregopher/radix/v3"
	"github.com/pebbe/zmq4"
)

//// Configurables
const eventHealthRecNum int = 10

func TestStartupAndCleanup(t *testing.T) {
	cleanup, err := StartCronScheduler()
	if err != nil {
		t.Errorf("Error occurred in starting Cron Scheduler! Err: %v\n", err)
	}
	cleanup()
}

func TestEventHealthCheck(t *testing.T) {
	cleanup, err := redis.StartDatabase()
	if err != nil {
		t.Errorf("Error occurred in starting Redis! Err: %v\n", err)
	}
	defer cleanup()

	cleanup, err = zeromq.StartZeroMqComms()
	if err != nil {
		t.Errorf("Error occurred in starting zeromq! Err: %v\n", err)
	}
	defer cleanup()

	// Empty Queue Should Return
	eventCheckHealth()

	// Publish some records
	nums := make([]int64, eventHealthRecNum)
	cmds := make([]radix.CmdAction, eventHealthRecNum)
	check := map[string]bool{}

	for i := 0; i < eventHealthRecNum; i++ {
		nums[i] = rand.Int63()
		cmds[i] = radix.Cmd(nil, "LPUSH", event.HealthTaskQueue, fmt.Sprintf("%d", nums[i]))
		check[fmt.Sprintf("%d", nums[i])] = false
	}

	err = redis.MasterRedis.Do(radix.Pipeline(cmds...))
	if err != nil {
		t.Errorf("Error occurred in sendin records to Redis! Err: %v\n", err)
	}

	// Receive Records via Zeromq
	zmqREP, err := zeromq.MasterZeroMQ.NewSocket(zmq4.Type(zmq4.REP))
	if err != nil {
		t.Errorf("Error occured creating zeromq socket! Err: %v\n", err)
	}

	zmqREP.Bind(zeromq.ZeromqMask + ProxyFEPort)

	go eventCheckHealth()

	var msg string
	var args []string
	var exists bool
	var value bool
	for i := 0; i < eventHealthRecNum; i++ {
		t.Log("Waiting For Message")
		msg, err = zmqREP.Recv(zmq4.Flag(0))
		for msg == "" {
			msg, err = zmqREP.Recv(zmq4.Flag(0))
		}
		t.Logf("Got Message: %s\n", msg)

		t.Logf("Sending Confirmation!\n")
		zmqREP.Send("OK", zmq4.Flag(0))

		_, args = parseTask(msg)
		if len(args) == 0 {
			t.Errorf("Message was illformatted: %s\n", msg)
		}

		value, exists = check[args[0]]
		if !exists {
			t.Errorf("Got a GameID that did not exist! Msg: %s\n", msg)
		} else if value {
			t.Errorf("Got a repeated GameID! Msg: %s\n", msg)
		} else {
			t.Logf("Message Looks Unique!\n")
		}

		check[args[0]] = true
	}

	// Do it once more to verify no errors or problems
	eventCheckHealth()

	// Not Closing Here Seemed to cause a leak...
	zmqREP.Close()
}
