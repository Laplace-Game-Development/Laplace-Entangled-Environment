// Route Represents the routing and listening to connections.
// This module takes care of the communication links to users
// and clients. They will forward commands to data driven modules
// in `/data`. The Listener module makes sure to listen to
// connections over TCP and HTTP (maybe Websocket too later.)
// We also have the parser which uses the policy directives to
// break apart the user payloads into understandable commands.
// Secure takes care of any encryption necessary over the wire.
package route

import (
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"

	"laplace-entangled-env.com/internal/policy"
	"laplace-entangled-env.com/internal/util"
)

//// Configurables

// Time for shutdown. Quitting Mid Handle is really bad. This should be longer than any duration
const ShutdownDuration time.Duration = 10 * time.Second

/* Client TCP Settings */

// Time spent waiting for incomming connections before checking for control signals/shutoff/etc
const IoDeadline time.Duration = 5 * time.Millisecond

// TCP IP Mask to listen for connections on
const ListeningTCPIpAddress string = "127.0.0.1"

// TCP Port number to listen for connections on
const ListeningTCPPortNumber string = ":26005"

// Size of Packet Header for TCP commands
// Byte 1 :: Metadata/Parsing info
// Byte 2 :: More Significant byte for Command
// Byte 3 :: Lesser Significant byte for Command
const CommandBytes = 3

// Limit of Goroutines used for listening on TCP.
// 5 is a good number for testing, but a better number would be much higher.
const NumberOfTCPThreads = 5

// Constant byte string of JSON representing a data malformed error
// May be moved to Policy
var MalformedDataMsg []byte = []byte("{\"success\": false, \"error\": \"Data Was Malformed!\"}")

// Constant integer length of a JSON byte string representing a data malformed error
// May be moved to Policy
var MalformedDataMsgLen int = len([]byte("{\"success\": false, \"error\": \"Data Was Malformed!\"}"))

// HTTP Listening Host IP
const HttpHost string = "127.0.0.1"

// HTTP Host Listening Port Number
const HttpPort string = ":8080"

//// Global Variables | Singletons

// Thread Pool for Connection Listening. This stores the threads and context
// for all listening for connections.
// The goroutines themselves will spawn other goroutines so we use more than
// 3 "threads" for the full application.
//
// 1 for TCP
// 1 For HTTP
// 1 For WebSocket?
var listenerThreadPool util.ThreadPool = util.NewThreadPool(3)

// ServerTask Startup Function for Conneciton Listening. Takes care of initialization.
func StartListener() (func(), error) {
	err := listenerThreadPool.SubmitFuncUnsafe(startTCPListening)
	if err != nil {
		return nil, err
	}

	err = listenerThreadPool.SubmitFuncUnsafe(startHTTPListening)
	if err != nil {
		return nil, err
	}

	return cleanUpListener, nil
}

// CleanUp Function returned by Startup function. Stops all listeners by "Finish"ing the
// threadpool. This means that within the given "ShutdownDuration" the listeners should
// be closed.
func cleanUpListener() {
	log.Println("Cleaning Up Listener Logic")
	listenerThreadPool.Finish(time.Now().Add(ShutdownDuration))
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// General Listening Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// takes the required parameters and ensures any listener that is ready to send their command
// to data has the required components. This will parse and perform the commmand requested. It
// will then return the calculated byte slice response.
//
// requestHeader :: Common Fields for all requests including authentication and endpoint selection
// bodyFactories :: Arguments for the commands in the form of first order functions
// isSecured     :: Whether the request came over an encrypted connection (i.e. SSL/SSH/HTTPS)
//
// returns []byte :: byte slice response for user
//          error :: non-nil when an invalid command is sent or an error occurred when processing
//             typically means request was rejected.
func calculateResponse(requestHeader policy.RequestHeader, bodyFactories policy.RequestBodyFactories, isSecured bool) ([]byte, error) {
	// parse.go
	return switchOnCommand(requestHeader, bodyFactories, isSecured)
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// HTTP Listening Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// A Constant map of what commands should be made with a POST HTTP Request
// rather than a GET Request. This is for semantic and API design reasons.
//
// This should never change during runtime!
var postOnlyCmdMap map[policy.ClientCmd]bool = map[policy.ClientCmd]bool{
	policy.CmdError:      false,
	policy.CmdEmpty:      false,
	policy.CmdRegister:   true,
	policy.CmdLogin:      true,
	policy.CmdAction:     true,
	policy.CmdObserve:    true,
	policy.CmdGetUser:    true,
	policy.CmdGameCreate: true,
	policy.CmdGameJoin:   true,
	policy.CmdGameLeave:  true,
	policy.CmdGameDelete: true,
}

// Attaches Path Handlers for HTTP Web Server. Uses Paths to
// communicate command used. i.e. /user/ -> CmdGetUser
//
// ctx :: Owning Context for HTTP Listener
func startHTTPListening(ctx context.Context) {
	http.HandleFunc("/empty/", getHttpHandler(policy.CmdEmpty))
	http.HandleFunc("/error/", getHttpHandler(policy.CmdError))
	http.HandleFunc("/register/", getHttpHandler(policy.CmdRegister))
	http.HandleFunc("/login/", getHttpHandler(policy.CmdLogin))
	http.HandleFunc("/action/", getHttpHandler(policy.CmdAction))
	http.HandleFunc("/observe/", getHttpHandler(policy.CmdObserve))
	http.HandleFunc("/user/", getHttpHandler(policy.CmdGetUser))
	http.HandleFunc("/game/create/", getHttpHandler(policy.CmdGameCreate))
	http.HandleFunc("/game/join/", getHttpHandler(policy.CmdGameJoin))
	http.HandleFunc("/game/leave/", getHttpHandler(policy.CmdGameLeave))
	http.HandleFunc("/game/delete/", getHttpHandler(policy.CmdGameDelete))

	http.HandleFunc("*", http.NotFound)

	serverConfig := http.Server{
		Addr:        HttpHost + HttpPort,
		TLSConfig:   &tlsConfig, // Found in secure.go
		BaseContext: func(l net.Listener) context.Context { return ctx },
	}

	// Error will always be Non-Nil Here!
	err := serverConfig.ListenAndServe()
	if err != nil {
		log.Printf("HTTP Error: %v\n", err)
	}
}

// Creates a First Order Function for the given command.
// Useful for when adding handlers in the initialization function
// for HTTP.
func getHttpHandler(command policy.ClientCmd) func(writer http.ResponseWriter, req *http.Request) {
	return func(writer http.ResponseWriter, req *http.Request) {
		handleHttp(command, writer, req)
	}
}

// Handles a given HTTP request with the given Client Command (endpoint), and data
//
// clientCmd :: Selected Endpoint/Command
// writer    :: writer to be written to with response data for user
// req       :: Given HTTP Request with associated non-command data (args and authentication)
func handleHttp(clientCmd policy.ClientCmd, writer http.ResponseWriter, req *http.Request) {
	if checkPost(clientCmd, writer, req) {
		return
	}

	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Printf("Error Reading Body: %v\n", err)
	}

	requestAttachment := parseHeaderInfo(req, &body)

	requestHeader := policy.RequestHeader{
		Command: clientCmd,
		UserID:  requestAttachment.UserID,
		Sig:     requestAttachment.Sig,
	}

	bodyFactories := policy.RequestBodyFactories{
		ParseFactory: func(ptr interface{}) error {
			return json.Unmarshal(body, ptr)
		},
		SigVerify: func(userID string, userSig string) error {
			return SigVerification(userID, userSig, &body)
		},
	}

	calculateResponse(requestHeader, bodyFactories, req.TLS != nil)
}

// Returns whether the given HTTP request is a POST request if needed.
// see "postOnlyCmdMap"
//
// clientCmd :: Selected Endpoint/Command
// writer    :: writer to be written to with response data for user
// req       :: Given HTTP Request with associated non-command data (args and authentication)
// returns   -> bool
//              true  | continue processing the request
//              false | ignore the request. An error was already given to the user
func checkPost(clientCmd policy.ClientCmd, writer http.ResponseWriter, req *http.Request) bool {
	needsPost, exists := postOnlyCmdMap[clientCmd]
	if exists && needsPost && req.Method != http.MethodPost {
		output := policy.UnSuccessfulResponse("Post Required!")
		writeable, err := output.Digest(output.Data)
		if err != nil {
			log.Fatal("handleHttp: Could Not Write Utility Response to User!")
		}

		writer.Write(writeable)
		return true
	}

	return false
}

// Creates the Request Attachment (Authentication Portion) of the request
//
// req :: HTTP Request with associated data
// body :: slice of data representing the request body. We use a parameter
//    rather than grabbing it all to optimize the process. It needs to be
//    used by other functions, so passing it in is better than creating
//    a variable just to be garbage collected
//
// returns -> policy.RequestAttachment :: the authentication components found
//        or empty components if none are found in header/cookies/or body
func parseHeaderInfo(req *http.Request, body *[]byte) policy.RequestAttachment {
	requestAttachment := policy.RequestAttachment{}
	userIDFound := false
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
		userSigObj := policy.RequestAttachment{}

		err := json.Unmarshal(*body, &userSigObj)
		if err == nil {
			possibleUserIDs[2] = userSigObj.UserID
			possibleSigs[2] = userSigObj.Sig
		} else {
			log.Println("Illformatted JSON sent to HTTP Header")
		}
	}

	for i := 0; !userIDFound && !sigFound && i < 3; i++ {
		if !userIDFound && len(possibleUserIDs[i]) > 0 {
			requestAttachment.UserID = possibleUserIDs[i]
			userIDFound = true
		}

		if !sigFound && len(possibleSigs[i]) > 0 {
			requestAttachment.Sig = possibleSigs[i]
			sigFound = true
		}
	}

	return requestAttachment
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// TCP Listening Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Wrapper Structure with boolean fields for a TCP Connection.
// used to easily differentiate between secure and insecure
// connections. It also helps in deciding if the TCP connection
// needs to parse more requests (HTTP requests close connections
// after one requests, but TCP connections do not.)
type TCPClientConn struct {
	conn         net.Conn
	isSecured    bool
	isReadNeeded bool
}

// First byte of a TCP request. This is a struct of booleans
// about how the request is structured over TCP.
type TCPRequestPrefix struct {
	NeedsSecurity bool // First Most Sig Bit
	IsBase64Enc   bool // Second Most Sig Bit
	IsJSON        bool // Third Most Sig Bit
}

// Creates TCP Connection Listener(s) with a designated threadpool and addressing.
// It submits goroutine each time it finds a connection. If no goroutine is available
// in the threadpool the listener blocks until one is found.
//
// ctx :: Owning Context
func startTCPListening(ctx context.Context) {
	log.Println("TCP Listening on " + ListeningTCPIpAddress + ListeningTCPPortNumber + "!")
	ln, err := net.Listen("tcp", ListeningTCPIpAddress+":"+ListeningTCPPortNumber)
	if err != nil {
		log.Fatal(err)
	}

	pool := util.NewThreadPoolWithContext(NumberOfTCPThreads, ctx)

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

// TCP goroutine function, using the prepackaged information to serve the
// connected client. It parses the data it receives to compile a response.
// Function will loop until the connection is closed.
//
// ctx :: Owning Context
// clientConn :: Metadata and reference to TCP Connection
func handleTCPConnection(ctx context.Context, clientConn TCPClientConn) {
	log.Println("New Connection!")
	// Set Timeout
	clientConn.conn.SetReadDeadline(time.Now().Add(IoDeadline))
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
			util.Clear(&dataIn)
		}
	}
}

// Read and Gather Byte Response for a TCP Client Connection
//
// clientConn :: Metadata and reference to TCP Connection
// dataIn     :: byte slice data read in for
//      Command, Args, Authentication, etc.
//
// returns -> bool
//            true | command was successful
//           false | command was unsuccessful
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

	returnWithoutRequest, err := SecureTCPConnIfNeeded(&clientConn, prefix)
	if err != nil {
		log.Println(err)
		return false
	} else if returnWithoutRequest {
		return true
	}

	header, bodyFactory, err := generateRequestFromTCP(n, dataIn, prefix)
	if err != nil {
		log.Println(err)
		err = writeTCPResponse(clientConn, &MalformedDataMsg, MalformedDataMsgLen)
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

// Gather TCP Prefix.
// Prefix is the first Byte of a TCP Request.
// It instructs us how the data is structured.
//
// length :: number of bytes in data
// data   :: payload/data for request i.e. Command, Auth, and Args
//
// returns -> TCPRequestPrefix :: Boolean struct for Structuring metadata
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

// Using the structuring metadata
// and the rest of the payload data, the function generates
// a request to the server and returns the information
// needed to "calculateResponse"
//
// length :: number of bytes in data
// data   :: payload/data for request i.e. Command, Auth, and Args
// prefix :: Structuring Metadata
//
// returns {
//	    RequestHeader :: header data used for all request (Command and Authentication)
//      RequestBodyFactories ::	Transform functions for getting request arguments
//      error :: If parsing goes wrong and the request is illformed an error is returned
// }
func generateRequestFromTCP(length int, data *[]byte, prefix TCPRequestPrefix) (policy.RequestHeader, policy.RequestBodyFactories, error) {
	header := policy.RequestHeader{}
	factories := policy.RequestBodyFactories{}

	if length < 3 {
		return header, factories, errors.New("No Command In Request")
	}

	// Get Command
	cmd, err := ParseCommand((*data)[1], (*data)[2])
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
	factories.ParseFactory = func(ptr interface{}) error {
		return parseBody(ptr, prefix, &bodyPayload)
	}

	factories.SigVerify = func(userID string, userSig string) error {
		return SigVerification(userID, userSig, &bodyPayload)
	}

	return header, factories, nil
}

// After successful Read->Response should we continue communications?
//
// clientConn :: Metadata and reference to TCP Connection
//
// returns -> bool
//            true | keep the connection alive
//           false | close the connection
func computeTCPKeepAlive(clientConn TCPClientConn) bool {
	// Add Logic for Connection Overhead
	return clientConn.isReadNeeded
}

// Write byte slice to client
//
// clientConn :: Metadata and reference to TCP Connection
// response   :: byte slice of what needs to be sent to client
// length     :: number of bytes in byte slice
//
// returns -> error if an error occurs and nil otherwise.
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
