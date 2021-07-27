package zeromq

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/pebbe/zmq4"
)

//// Configurables
const reqRespConnectionString string = "tcp://127.0.0.1:12125"
const reqRespMessage string = "DERP"

var (
	cwd_arg = flag.String("cwd", "", "set cwd")
)

func TestMain(m *testing.M) {
	flag.Parse()
	if *cwd_arg != "" {
		if err := os.Chdir(*cwd_arg); err != nil {
			fmt.Println("Chdir error:", err)
		}
	}

	os.Exit(m.Run())
}

func TestAll(t *testing.T) {
	cleanup, err := StartZeroMqComms()
	if err != nil {
		t.Fatalf("Error was Not Nil in ZeroMQ Startup. Err:%v \n", err)
	}

	t.Run("Request Response", testRequestResponse)

	cleanup()
}

func testRequestResponse(t *testing.T) {
	lock := make(chan bool)
	resp, err := MainZeroMQ.NewSocket(zmq4.REP)
	if err != nil {
		t.Fatalf("Error Creating ZeroMQ Socket. Err: %v\n", err)
	}
	defer resp.Close()

	err = resp.Bind(reqRespConnectionString)
	if err != nil {
		t.Fatalf("Error Binding ZeroMQ Socket. Err: %v\n", err)
	}

	req, err := MainZeroMQ.NewSocket(zmq4.REQ)
	if err != nil {
		t.Fatalf("Error Creating ZeroMQ Socket. Err: %v\n", err)
	}
	defer req.Close()

	err = req.Connect(reqRespConnectionString)
	if err != nil {
		t.Fatalf("Error Binding ZeroMQ Socket. Err: %v\n", err)
	}

	// Sockets are not threadsafe, but we'll do it this way for testing purposes
	go func(pipe chan bool) {
		response, err := resp.Recv(zmq4.Flag(0))
		if err != nil {
			pipe <- false
		} else if response != reqRespMessage {
			pipe <- false
		} else {
			pipe <- true
		}

		resp.Send(reqRespMessage, zmq4.DONTWAIT)
	}(lock)

	n, err := req.Send(reqRespMessage, zmq4.DONTWAIT)
	if n < len(reqRespMessage) {
		t.Fatalf("Could not send full message with request socket! Sent: %d\n", n)
	}

	result := <-lock
	if !result {
		t.Fatalf("Receiving Socket Failed!")
	}
}
