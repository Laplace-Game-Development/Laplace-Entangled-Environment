package main

import (
	"log"
	"net"
	"time"
)

// Client TCP Settings
const ioDeadline time.Duration = 5 * time.Millisecond
const listeningIpAddress string = ""
const listeningPortNumber string = "26005"
const commandBytes = 4
const numberOfGames = 20
const throttleGames = false

// These should not change during runtime
var malformedDataMsg []byte = []byte("{\"success\": false, \"error\": \"Data Was Malformed!\"}")
var malformedDataMsgLen int = len([]byte("{\"success\": false, \"error\": \"Data Was Malformed!\"}"))

// Command Logic Settings
const createGameAuthSliceLow = 2
const createGameAuthSliceHigh = 10

type ServerTask func() (func(), error)
type ProcessFunction func() error

type ClientConn struct {
	conn      net.Conn
	isSecured bool
}

func main() {
	// --- SERVER START UP ---
	// 1. Start Game Engine
	cleanUp := invokeServerStartup(startGameLogic)
	defer cleanUp()

	// 2. Start Database Connection
	cleanUp = invokeServerStartup(startDatabase)
	defer cleanUp()

	// 3. Start Encryption Service
	cleanUp = invokeServerStartup(startEncryption)
	defer cleanUp()

	// 4. Do any Game Room Initialization
	cleanUp = invokeServerStartup(startRoomsSystem)
	defer cleanUp()

	// 5. Start Task Worker Service
	cleanUp = invokeServerStartup(startTaskQueue)
	defer cleanUp()

	// 6. Start Scheduler Service
	cleanUp = invokeServerStartup(startCronScheduler)
	defer cleanUp()

	// 7. Everything is ready -- Start Listening
	startListening()
}

func startListening() {
	log.Println("Listening on " + listeningIpAddress + listeningPortNumber + "!")
	ln, err := net.Listen("tcp", listeningIpAddress+":"+listeningPortNumber)
	if err != nil {
		log.Fatal(err)
	}

	for {
		// SYN + ACK
		conn, err := ln.Accept()
		if err != nil {
			log.Println("Something Terrible Occurred!")
			log.Println(err)
			continue
		}
		go handleConnection(ClientConn{conn: conn, isSecured: false})
	}
}

//// Utility Functions
func invokeServerStartup(fn ServerTask) func() {
	cleanUp, err := fn()

	if err != nil || cleanUp == nil {
		log.Println("Trouble Starting Server!")
		log.Fatalln(err)
	}

	return cleanUp
}

func errorless(fn ProcessFunction) {
	err := fn()

	if err != nil {
		log.Fatalln(err)
	}
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Handling Connections
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func handleConnection(clientConn ClientConn) {
	log.Println("New Connection!")
	// Set Timeout
	clientConn.conn.SetReadDeadline(time.Now().Add(ioDeadline))
	defer clientConn.conn.Close()
	defer log.Println("Connection Closed!")

	// Read Bytes
	// Bytes need to be instantiated otherwise golang will not read to them
	dataIn := make([]byte, 2048)
	keepAlive := true

	for keepAlive {
		keepAlive = false
		n, err := clientConn.conn.Read(dataIn)
		if err != nil {
			log.Println(err)
			return
		}

		if n < commandBytes {
			// respond with error and help message
			// ==
			// Do we want to be helpful or deny illegal uses of the API?
			return
		}

		prefix, err := parseRequestPrefix(dataIn)
		if err != nil {
			log.Println(err)
			return
		}
		data := dataIn[commandBytes:]

		if prefix.IsEncoded {
			data, err = base64Decode(data)
			if err != nil {
				log.Println(err)
				return
			}
		}

		wasInsecure := !clientConn.isSecured
		err = monadicallySecure(&clientConn, prefix.Command)
		if err != nil {
			log.Println(err)
			return
		} else if !wasInsecure && clientConn.isSecured {
			keepAlive = true
			continue
		}

		var requestHeader RequestHeader
		if !clientConn.isSecured {
			requestHeader, err = parseRequestHeader(prefix, data)
			if err != nil {
				log.Println(err)
				n, writeErr := clientConn.conn.Write(malformedDataMsg)
				if err != nil || n < malformedDataMsgLen {
					log.Println(writeErr)
				}
				return
			}
		} else {
			// Default Everrything to Zero Value. secure.go Commands will do their own parsing.
			requestHeader = RequestHeader{}
		}

		response, serverErr := switchOnCommand(prefix, requestHeader, clientConn, data[requestHeader.bodyStart:])
		if serverErr != nil {
			log.Println(serverErr)
			return
		}

		// Tokenize and Encrypt Response Here

		n, err = clientConn.conn.Write(response)
		if err != nil || n < len(response) {
			log.Println(err)
			return
		}

		keepAlive = computeKeepAlive(clientConn)

		if keepAlive {
			clear(dataIn)
		}
	}
}

func computeKeepAlive(clientConn ClientConn) bool {
	// Add Logic for Connection Overhead
	return !clientConn.isSecured
}
