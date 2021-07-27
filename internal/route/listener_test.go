package route

import (
	"crypto/tls"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/data"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/schedule"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/startup"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/util"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/zeromq"
)

const socketReadDuration time.Duration = time.Minute
const socketBatchReadSize int = 512
const socketBatchReadSizeMax int = 10

// TLS Configuration for HTTPS Server and SSL with TCP
//
// This will be assigned on startup then left unchanged
var tlsClientConfig tls.Config = tls.Config{}

func TestTCPListener(t *testing.T) {
	cleanup := startup.InitServerStartupOnTaskList(
		[]startup.ServerTask{
			redis.StartDatabase,
			zeromq.StartZeroMqComms,
			////////////////////////
			StartEncryption,
			data.StartUsers,
			data.StartRoomsSystem,
			schedule.StartTaskQueue,
			schedule.StartCronScheduler,
			StartListener, // Dependent on startEncryption
		})

	tlsClientConfig = tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}

	// Wait for startup
	time.Sleep(time.Second * 5)

	/*
	 *******************************************************
	 * Example Request Chain for The following Actions
	 * 1. Registering
	 * 2. Logging In
	 * 3. Creating a Game
	 * 4. Getting a User
	 * 5. Leaving a Game
	 *
	 *****************************************************
	 */

	t.Run("Register", func(t *testing.T) {
		prefix := []byte{0b0010_0000, 0b0000_0000, 0b0000_0001}
		header := "{}"
		body := "{ Username: 'DerpityUnityTesty', Password:'YoYoZ0Z0@ }"
		payload := []byte(fmt.Sprintf("%s%s%s", prefix, header, body))
		msg := reqAndRespSecureTCP(t, &payload)
		t.Logf("Got: %s\n", msg)
	})

	cleanup()
}

func reqAndRespTCP(t *testing.T, message *[]byte) []byte {
	addr, err := net.ResolveTCPAddr("tcp", ListeningTCPIpAddress+ListeningTCPPortNumber)
	if err != nil {
		t.Fatalf("Error Creating Address! Err: %v\n", err)
	}

	t.Logf("DIALING: %s\n", addr)
	sock, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		t.Fatalf("Error Dialing TCP Address! Err: %v\n", err)
	}
	defer sock.Close()

	t.Logf("SENDING: %s\n", *message)
	n, err := sock.Write(*message)
	if err != nil {
		t.Fatalf("Error Sending Message to TCP Socket! Err: %v\n", err)
	} else if n != len(*message) {
		t.Fatalf("Error Length does not match number of characters sent!\n")
	}

	sock.SetReadDeadline(time.Now().Add(socketReadDuration))
	bytes, err := util.BatchReadConnection(sock, byte(4), socketBatchReadSize, socketBatchReadSizeMax)
	if err != nil {
		t.Fatalf("Error Reading Message From TCP Socket! Err: %v\n", err)
	}

	return bytes
}

func reqAndRespSecureTCP(t *testing.T, message *[]byte) []byte {
	addr, err := net.ResolveTCPAddr("tcp", ListeningTCPIpAddress+ListeningTCPPortNumber)
	if err != nil {
		t.Fatalf("Error Creating Address! Err: %v\n", err)
	}

	t.Logf("DIALING: %s\n", addr)
	sock, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		t.Fatalf("Error Dialing TCP Address! Err: %v\n", err)
	}
	defer sock.Close()

	prefix := []byte{0b1000_0000, 0b0000_0000, 0b0000_0000}
	header := "{}"
	body := "{}"
	payload := []byte(fmt.Sprintf("%s%s%s", prefix, header, body))

	t.Logf("SENDING: %s\n", payload)
	n, err := sock.Write(payload)
	if err != nil {
		t.Fatalf("Error Sending Message to TCP Socket! Err: %v\n", err)
	} else if n != len(payload) {
		t.Fatalf("Error Length does not match number of characters sent!\n")
	}

	secureSock := tls.Client(sock, &tlsClientConfig)

	secureSock.SetReadDeadline(time.Now().Add(socketReadDuration))
	secBufBytes, err := util.BatchReadConnection(secureSock, byte(4), socketBatchReadSize, socketBatchReadSizeMax)
	if err != nil {
		t.Fatalf("Error Reading Message From TCP Socket! Err: %v\n", err)
	}

	t.Logf("GOT FROM SECURING CONNECITON: %s\n", secBufBytes)
	stringifiedSecBuf := string(secBufBytes)
	if stringifiedSecBuf != string(SecureConnectionMsg) {
		t.Fatalf("Got Unexpected String From Listener!\n")
	}

	t.Logf("SENDING: %s\n", *message)
	n, err = secureSock.Write(*message)
	if err != nil {
		t.Fatalf("Error Sending Message to TCP Socket! Err: %v\n", err)
	} else if n != len(*message) {
		t.Fatalf("Error Length does not match number of characters sent!\n")
	}

	secureSock.SetReadDeadline(time.Now().Add(socketReadDuration))
	bytes, err := util.BatchReadConnection(secureSock, byte(4), socketBatchReadSize, socketBatchReadSizeMax)
	if err != nil {
		t.Fatalf("Error Reading Message From TCP Socket! Err: %v\n", err)
	}

	return bytes
}
