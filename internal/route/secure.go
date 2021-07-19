package route

import (
	"crypto/tls"
	"errors"
	"log"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/policy"
)

//// Configurables

//
// Encryption Configurables

// TLS Certificate File Location from root of the project
const CrtLocation string = "./tlscert.crt"

// TLS Key File Location from root of the project
const KeyLocation string = "./tlskey.key"

//
// Listener Secure Configurables

// TLS Configuration for HTTPS Server and SSL with TCP
//
// This will be assigned on startup then left unchanged
var tlsConfig tls.Config = tls.Config{}

// Set of Commands that need to be done over encrypted connections.
//
// This Map is a Set!
// This should never change during runtime!
var secureMap map[policy.ClientCmd]bool = map[policy.ClientCmd]bool{
	policy.CmdRegister: true,
	policy.CmdLogin:    true,
}

// ServerTask Startup Function for Encryption. Takes care of initialization.
// Loads Certificates and Keys from files and configures TLS.
func StartEncryption() (func(), error) {
	log.Printf("Loading Certificate From: %s \nand Key From: %s\n", CrtLocation, KeyLocation)
	cert, err := tls.LoadX509KeyPair(CrtLocation, KeyLocation)
	if err != nil {
		return nil, err
	}

	// Instead of setting the certificate we can add a callback to load certificates
	tlsConfig = tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	return cleanUpEncryption, nil
}

// CleanUp Function returned by Startup function. Doesn't do anything, but here
// for consistency.
func cleanUpEncryption() {
	log.Println("Cleaning Up Encryption Logic")
}

// Secure the current TCP Listener connection. Return True if a new Connection was created
// Return an error if somethign went wrong
func SecureTCPConnIfNeeded(clientConn *TCPClientConn, prefix TCPRequestPrefix) (bool, error) {
	if clientConn.isSecured || !prefix.NeedsSecurity {
		return false, nil
	}

	newConn := tls.Server(clientConn.conn, &tlsConfig)
	if newConn == nil {
		return false, errors.New("newConn was nil!!! Something went wrong!")
	}

	clientConn.conn = newConn
	clientConn.isSecured = true
	return true, nil
}

// returns if the given command needs an encrypted connection or not
//
// see "secureMap"
func NeedsSecurity(cmd policy.ClientCmd) bool {
	result, exists := secureMap[cmd]
	return exists && result
}
