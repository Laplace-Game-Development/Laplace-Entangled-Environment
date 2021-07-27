package route

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"strings"
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

	// Cleanup for request chain
	username := "DerpityUnityTesty"
	data.DeleteUser(username)

	// Register
	prefix := []byte{0b0100_0000, 0b0000_0000, 0b0000_0001}
	header := "{}"
	body := "{\"Username\":\"" + username + "\", \"Password\":\"YoYoZ0Z0@\"}"
	payload := []byte(fmt.Sprintf("%s%s%s", prefix, header, body))
	msg := reqAndRespSSL(t, &payload)
	t.Logf("Got: %s\n", msg)

	// Login
	prefix = []byte{0b0100_0000, 0b0000_0000, 0b0000_0010}
	header = "{}"
	body = "{\"Username\":\"" + username + "\", \"Password\":\"YoYoZ0Z0@\"}"
	payload = []byte(fmt.Sprintf("%s%s%s", prefix, header, body))
	msg = reqAndRespSSL(t, &payload)
	t.Logf("Got Size: %d, Response: %s\n", len(msg), msg)

	// Store Token
	token, err := util.Base64Decode(&msg)
	if err != nil {
		t.Errorf("Base64 Did Not Decode Correctly! Err: %v\n", err)
	}
	t.Logf("Got Token: %X\n", token)
	uses := 0

	// Get User
	prefix = []byte{0b0100_0000, 0b0000_0001, 0b0000_0000}
	header = "{}"
	body = "{\"Username\":\"" + username + "\"}"
	payload = []byte(fmt.Sprintf("%s%s%s", prefix, header, body))
	msg = reqAndRespTCP(t, &payload)
	t.Logf("Got: %s\n", msg)

	// Gather UserID
	var userInfo data.UserInfo
	err = json.Unmarshal(msg, &userInfo)
	if err != nil {
		t.Fatalf("Error Parsing AuthID! %v\n", err)
	}

	// Create Game
	body = "{}"
	checksum := TestHelperGenSig(&token, body, uses)
	uses += 1
	prefix = []byte{0b0100_0000, 0b0000_0010, 0b0000_0000}
	header = "{\"UserID\":\"" + userInfo.AuthID + "\", \"Sig\":\"" + checksum + "\"}"
	payload = []byte(fmt.Sprintf("%s%s%s", prefix, header, body))
	msg = reqAndRespTCP(t, &payload)
	t.Logf("Got: %s\n", msg)

	// Gather Game
	var gameInfo data.GameMetadata
	err = json.Unmarshal(msg, &gameInfo)
	if err != nil {
		t.Fatalf("Error Parsing AuthID! %v\n", err)
	} else if gameInfo.Id == "" {
		t.Fatalf("Error Game Was Not Created!")
	}

	// Leave Game
	body = "{\"GameID\":\"" + gameInfo.Id + "\"}"
	checksum = TestHelperGenSig(&token, body, uses)
	uses += 1
	prefix = []byte{0b0100_0000, 0b0000_0010, 0b0000_0010}
	header = "{\"UserID\":\"" + userInfo.AuthID + "\", \"Sig\":\"" + checksum + "\"}"
	payload = []byte(fmt.Sprintf("%s%s%s", prefix, header, body))
	msg = reqAndRespTCP(t, &payload)
	t.Logf("Got: %s\n", msg)

	strMsg := string(msg)
	if !strings.Contains(strMsg, "\"Successful\":true") {
		t.Errorf("Did Not get Success from leaving owned game! Response: %s\n", strMsg)
	}

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

func reqAndRespSSL(t *testing.T, message *[]byte) []byte {
	t.Logf("DIALING: %s\n", ListeningTCPIpAddress+ListeningSSLPortNumber)
	sock, err := tls.Dial("tcp", ListeningTCPIpAddress+ListeningSSLPortNumber, &tlsClientConfig)
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
