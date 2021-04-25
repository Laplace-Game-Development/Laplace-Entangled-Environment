package main

import (
	"context"
	"log"
	"net"
	"time"
)

// Handler Constants

// Time for shutdown. Quitting Mid Handle is really bad. This should be longer than any duration
const shutdownDuration time.Duration = 10 * time.Minute

// Client TCP Settings
const ioDeadline time.Duration = 5 * time.Millisecond
const listeningTCPIpAddress string = ""
const listeningTCPPortNumber string = "26005"
const commandBytes = 4
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

//// Global Variables | Singletons

// 1 for TCP
// 1 For HTTP
// 1 For WebSocket
var listenerThreadPool ThreadPool = NewThreadPool(3)

type ClientConn struct {
	conn      net.Conn
	isSecured bool
}

func startListener() (func(), error) {
	err := listenerThreadPool.SubmitFuncUnsafe(startTCPListening)
	if err != nil {
		return nil, err
	}

	return func() {
		listenerThreadPool.Finish(time.Now().Add(shutdownDuration))
	}, nil
}

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
			handleConnection(ctx, ClientConn{conn: conn, isSecured: false})
		})
	}
}

func handleConnection(ctx context.Context, clientConn ClientConn) {
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

		// If We need to shutdown
		select {
		case <-ctx.Done():
			return
		default:
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
