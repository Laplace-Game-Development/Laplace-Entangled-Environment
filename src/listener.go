package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"
)

// Handler Constants

// Time for shutdown. Quitting Mid Handle is really bad. This should be longer than any duration
const shutdownDuration time.Duration = 10 * time.Minute

// Client TCP Settings
const ioDeadline time.Duration = 5 * time.Millisecond
const listeningTCPIpAddress string = ""
const listeningTCPPortNumber string = "26005"
const commandBytes = 3
const numberOfGames = 20
const throttleGames = false

// 5 is a good number for testing, but a better number would be much higher.
const numberOfTCPThreads = 5

// These should not change during runtime
var malformedDataMsg []byte = []byte("{\"success\": false, \"error\": \"Data Was Malformed!\"}")
var malformedDataMsgLen int = len([]byte("{\"success\": false, \"error\": \"Data Was Malformed!\"}"))

// Command Logic Settings
const createGameAuthSliceLow = 2
const createGameAuthSliceHigh = 10

// HTTP Configurations
const httpHost string = "127.0.0.1"
const httpPort string = ":8080"

//// Global Variables | Singletons

// 1 for TCP
// 1 For HTTP
// 1 For WebSocket
var listenerThreadPool ThreadPool = NewThreadPool(3)

func startListener() (func(), error) {
	err := listenerThreadPool.SubmitFuncUnsafe(startTCPListening)
	if err != nil {
		return nil, err
	}

	err = listenerThreadPool.SubmitFuncUnsafe(startHTTPListening)
	if err != nil {
		return nil, err
	}

	return func() {
		listenerThreadPool.Finish(time.Now().Add(shutdownDuration))
	}, nil
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// General Listening Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func calculateResponse(requestHeader RequestHeader, bodyFactories RequestBodyFactories, isSecured bool) ([]byte, error) {
	// parse.go
	return switchOnCommand(requestHeader, bodyFactories, isSecured)
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// HTTP Listening Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// This should never change during runtime!
var postOnlyCmdMap map[ClientCmd]bool = map[ClientCmd]bool{
	cmdError:      false,
	cmdEmpty:      false,
	cmdRegister:   true,
	cmdLogin:      true,
	cmdAction:     true,
	cmdObserve:    true,
	cmdGetUser:    true,
	cmdGameCreate: true,
	cmdGameJoin:   true,
	cmdGameLeave:  true,
	cmdGameDelete: true,
}

type UserSigTuple struct {
	UserID    string
	Signature string
}

func startHTTPListening(ctx context.Context) {
	http.HandleFunc("/empty/", getHttpHandler(cmdEmpty))
	http.HandleFunc("/error/", getHttpHandler(cmdError))
	http.HandleFunc("/register/", getHttpHandler(cmdRegister))
	http.HandleFunc("/login/", getHttpHandler(cmdLogin))
	http.HandleFunc("/action/", getHttpHandler(cmdAction))
	http.HandleFunc("/observe/", getHttpHandler(cmdObserve))
	http.HandleFunc("/user/", getHttpHandler(cmdGetUser))
	http.HandleFunc("/game/create/", getHttpHandler(cmdGameCreate))
	http.HandleFunc("/game/join/", getHttpHandler(cmdGameJoin))
	http.HandleFunc("/game/leave/", getHttpHandler(cmdGameLeave))
	http.HandleFunc("/game/delete/", getHttpHandler(cmdGameDelete))

	http.HandleFunc("*", http.NotFound)

	serverConfig := http.Server{
		Addr:        httpHost + httpPort,
		TLSConfig:   &tlsConfig, // Found in secure.go
		BaseContext: func(l net.Listener) context.Context { return ctx },
	}

	// Error will always be Non-Nil Here!
	err := serverConfig.ListenAndServe()
	if err != nil {
		log.Printf("HTTP Error: %v\n", err)
	}
}

func getHttpHandler(command ClientCmd) func(writer http.ResponseWriter, req *http.Request) {
	return func(writer http.ResponseWriter, req *http.Request) {
		handleHttp(command, writer, req)
	}
}

func handleHttp(clientCmd ClientCmd, writer http.ResponseWriter, req *http.Request) {
	if checkPost(clientCmd, writer, req) {
		return
	}

	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Printf("Error Reading Body: %v\n", err)
	}

	userID, sig := parseHeaderInfo(req, &body)

	requestHeader := RequestHeader{
		Command: clientCmd,
		UserID:  userID,
		Sig:     sig,
	}

	bodyFactories := RequestBodyFactories{
		parseFactory: func(ptr interface{}) error {
			return json.Unmarshal(body, ptr)
		},
		sigVerify: func(userID string, userSig string) error {
			return sigVerification(userID, userSig, &body)
		},
	}

	calculateResponse(requestHeader, bodyFactories, req.TLS != nil)
}

func checkPost(clientCmd ClientCmd, writer http.ResponseWriter, req *http.Request) bool {
	needsPost, exists := postOnlyCmdMap[clientCmd]
	if exists && needsPost && req.Method != http.MethodPost {
		output := unSuccessfulResponse("Post Required!")
		writeable, err := output.Digest(output.Data)
		if err != nil {
			log.Fatal("handleHttp: Could Not Write Utility Response to User!")
		}

		writer.Write(writeable)
		return true
	}

	return false
}

func parseHeaderInfo(req *http.Request, body *[]byte) (string, string) {
	userID := ""
	userIDFound := false
	sig := ""
	sigFound := false

	possibleUserIDs := make([]string, 3)
	possibleSigs := make([]string, 3)

	// Check Header
	possibleUserIDs[0] = req.Header.Get("laplace-user-id")
	possibleSigs[0] = req.Header.Get("laplace-signature")

	// Check Cookies
	userIDCookie, cookieErr := req.Cookie("laplaceUserId")
	if cookieErr != nil {
		log.Println("UserID Cookie Could Not Be Parsed")
	} else {
		possibleUserIDs[1] = userIDCookie.Value
	}

	sigCookie, cookieErr := req.Cookie("laplaceSig")
	if cookieErr == nil {
		log.Println("Signature Cookie Could Not Be Parsed")
	} else {
		possibleSigs[1] = sigCookie.Value
	}

	// Check Body
	if req.Body != nil {
		userSigObj := UserSigTuple{}

		err := json.Unmarshal(*body, &userSigObj)
		if err == nil {
			possibleUserIDs[2] = userSigObj.UserID
			possibleSigs[2] = userSigObj.Signature
		} else {
			log.Println("Illformatted JSON sent to HTTP Header")
		}
	}

	for i := 0; !userIDFound && !sigFound && i < 3; i++ {
		if !userIDFound && len(possibleUserIDs[i]) > 0 {
			userID = possibleUserIDs[i]
			userIDFound = true
		}

		if !sigFound && len(possibleSigs[i]) > 0 {
			sig = possibleSigs[i]
			sigFound = true
		}
	}

	return userID, sig
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// TCP Listening Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

type TCPClientConn struct {
	conn         net.Conn
	isSecured    bool
	isReadNeeded bool
}

// First byte of a request.
type TCPRequestPrefix struct {
	NeedsSecurity bool // First Most Sig Bit (rest of the bits are ignored)
	IsBase64Enc   bool // Second Most Sig Bit
	IsJSON        bool // Third Most Sig Bit
}

// TCP Entrypoint
func startTCPListening(ctx context.Context) {
	log.Println("TCP Listening on " + listeningTCPIpAddress + listeningTCPPortNumber + "!")
	ln, err := net.Listen("tcp", listeningTCPIpAddress+":"+listeningTCPPortNumber)
	if err != nil {
		log.Fatal(err)
	}

	pool := NewThreadPoolWithContext(numberOfTCPThreads, ctx)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// SYN + ACK
		conn, err := ln.Accept()
		if err != nil {
			log.Println("Error Occurred In TCP Handshake!")
			log.Println(err)
			continue
		}
		pool.SubmitFuncBlock(func(ctx context.Context) {
			handleTCPConnection(ctx, TCPClientConn{conn: conn, isSecured: false, isReadNeeded: false})
		})
	}
}

// TCP Coroutine Entry
func handleTCPConnection(ctx context.Context, clientConn TCPClientConn) {
	log.Println("New Connection!")
	// Set Timeout
	clientConn.conn.SetReadDeadline(time.Now().Add(ioDeadline))
	defer clientConn.conn.Close()
	defer log.Println("Connection Closed!")

	// Read Bytes
	// Bytes need to be instantiated otherwise golang will not read to them
	dataIn := make([]byte, 2048)

	// In the off chance we start and shutdown straight after without handling
	select {
	case <-ctx.Done():
		return
	default:
	}

	keepAlive := true

	for keepAlive {
		keepAlive = false

		// If We need to shutdown
		select {
		case <-ctx.Done():
			return
		default:
		}

		keepAlive = readAndRespondTCP(clientConn, &dataIn) && computeTCPKeepAlive(clientConn)

		if keepAlive {
			clear(&dataIn)
		}
	}
}

// Read and Gather Byte Response for a TCP Client Connection
func readAndRespondTCP(clientConn TCPClientConn, dataIn *[]byte) bool {
	n, err := clientConn.conn.Read(*dataIn)
	if err != nil {
		log.Println(err)
		return false
	}

	prefix, err := parseTCPPrefix(n, dataIn)
	if err != nil {
		log.Println(err)
		return false
	}

	returnWithoutRequest, err := secureTCPConnIfNeeded(&clientConn, prefix)
	if err != nil {
		log.Println(err)
		return false
	} else if returnWithoutRequest {
		return true
	}

	header, bodyFactory, err := generateRequestFromTCP(n, dataIn, prefix)
	if err != nil {
		log.Println(err)
		err = writeTCPResponse(clientConn, &malformedDataMsg, malformedDataMsgLen)
		if err != nil {
			log.Println(err)
		}

		return false
	}

	response, err := calculateResponse(header, bodyFactory, clientConn.isSecured)

	// Tokenize and Encrypt Response Here
	err = writeTCPResponse(clientConn, &response, len(response))
	if err != nil {
		log.Println(err)
	}

	return true
}

// Gather TCP Prefix
// Prefix is the first Byte of a TCP Request
func parseTCPPrefix(length int, data *[]byte) (TCPRequestPrefix, error) {
	if length < 1 {
		return TCPRequestPrefix{}, errors.New("Packet Has No Prefix")
	}

	firstByte := (*data)[0]

	prefix := TCPRequestPrefix{
		NeedsSecurity: (firstByte & 0b1000_0000) != 0,
		IsBase64Enc:   (firstByte & 0b0100_0000) != 0,
		IsJSON:        (firstByte & 0b0001_0000) != 0,
	}

	return prefix, nil
}

func generateRequestFromTCP(length int, data *[]byte, prefix TCPRequestPrefix) (RequestHeader, RequestBodyFactories, error) {
	header := RequestHeader{}
	factories := RequestBodyFactories{}

	if length < 3 {
		return header, factories, errors.New("No Command In Request")
	}

	// Get Command
	cmd, err := parseCommand((*data)[1], (*data)[2])
	if err != nil {
		return header, factories, err
	}

	header.Command = cmd

	// Add Attachment to Header
	bodyAttachmentAndPayload := (*data)[3:]
	attachment, bodyStart, err := parseRequestAttachment(prefix.IsJSON, &bodyAttachmentAndPayload)
	if err != nil {
		return header, factories, err
	}
	header.Sig = attachment.Sig
	header.UserID = attachment.UserID

	bodyPayload := bodyAttachmentAndPayload[bodyStart:]
	factories.parseFactory = func(ptr interface{}) error {
		return parseBody(ptr, prefix, &bodyPayload)
	}

	factories.sigVerify = func(userID string, userSig string) error {
		return sigVerification(userID, userSig, &bodyPayload)
	}

	return header, factories, nil
}

// After successful Read->Response should we continue communications?
func computeTCPKeepAlive(clientConn TCPClientConn) bool {
	// Add Logic for Connection Overhead
	return clientConn.isReadNeeded
}

func writeTCPResponse(clientConn TCPClientConn, response *[]byte, length int) error {
	numSent := 0

	for numSent < length {
		n, err := clientConn.conn.Write((*response)[numSent:])
		if err != nil {
			return err
		}

		numSent += n
	}

	return nil
}
