package main

import (
	"crypto/rand"
	"crypto/sha512"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/mediocregopher/radix/v3"
)

// Redis DB Configurables
const userPassTable string = "userPassword"
const authIDAtomicCounter string = "authIDAtomicCounter"
const userAuthIDTable string = "userToAuthID"

// User Table
const authIDSetPrefix string = "authID:"
const authIDSetUsernameField string = "username"
const authIDSetTokenField string = "token"
const authIDSetTokenStaleDateTimeField string = "stale"
const authIDSetTokenUseCounter string = "tokenUses"

// Encryption Configurables
const crtLocation string = "./tlscert.crt"
const keyLocation string = "./tlskey.key"
const escapeChar byte = byte('~')

// Token Configurables
const tokenLength int = 256
const tokenStaleTime time.Duration = time.Minute * 5

// This will be assigned on startup then left unchanged
var tlsConfig tls.Config = tls.Config{}

// This Map is a Set!
// This should never change during runtime!
var secureMap map[ClientCmd]bool = map[ClientCmd]bool{
	cmdRegister: true,
	cmdNewToken: true,
}

func startEncryption() (func(), error) {
	log.Printf("Loading Certificate From: %s \nand Key From: %s\n", crtLocation, keyLocation)
	cert, err := tls.LoadX509KeyPair(crtLocation, keyLocation)
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

func cleanUpEncryption() {
	log.Println("Encryption Goes Beep Boop!")
}

func monadicallySecure(clientConn *ClientConn, cmd ClientCmd) error {
	if clientConn.isSecured || cmd != cmdStartTLS {
		return nil
	}

	newConn := tls.Server(clientConn.conn, &tlsConfig)
	if newConn == nil {
		return errors.New("newConn was nil!!! Something went wrong!")
	}

	clientConn.conn = newConn
	clientConn.isSecured = true
	return nil
}

func needsSecurity(cmd ClientCmd) bool {
	result, exists := secureMap[cmd]
	return exists && result
}

func register(data []byte) CommandResponse {
	// cmd := data[0:2]
	seperator := data[2:6] // 4 bytes for a utf-32 character
	content := data[6:]
	escape := []byte{escapeChar}

	username, next := strTokWithEscape(seperator, escape, content, 0)
	if username == nil {
		return rawUnsuccessfulResponse("Illegal Input!")
	}

	password, _ := strTokWithEscape(seperator, escape, content, next)
	if password == nil {
		return rawUnsuccessfulResponse("Illegal Input!")
	}

	// We can make other fields later!

	success, err := createAccount(username, password)
	if err != nil {
		return respWithError(err)
	} else if success {
		return rawSuccessfulResponseBytes(username)
	} else {
		return rawUnsuccessfulResponse("Username Already Exists!")
	}
}

func createAccount(username []byte, password []byte) (bool, error) {
	if len(username) > redisKeyMax {
		return false, errors.New("Attempting To Store Too Large of a Username!")
	}

	var newID int
	var success int
	checksum := sha512.Sum512(password)
	checksumHex := hex.EncodeToString(checksum[:])
	castedName := string(username)

	// TODO add Pipelining
	err := masterRedis.Do(radix.Cmd(&success, "HSETNX", userPassTable, castedName, checksumHex))
	if err != nil {
		return false, err
	} else if success == 0 {
		return false, nil
	}

	err = masterRedis.Do(radix.Cmd(&newID, "INCR", authIDAtomicCounter))
	if err != nil {
		return false, err
	}

	err = masterRedis.Do(radix.Cmd(&success, "HSETNX", userAuthIDTable, castedName, fmt.Sprintf("%d", newID)))
	if err != nil {
		return false, err
	} else if success == 0 {
		return false, errors.New("Atomic Counter Did Not Return Unique ID!: " + authIDAtomicCounter)
	}

	// TODO We can add other fields here
	err = masterRedis.Do(radix.Cmd(nil, "HMSET", fmt.Sprintf(authIDSetPrefix+"%d", newID),
		authIDSetUsernameField, castedName,
		authIDSetTokenField, "",
		authIDSetTokenStaleDateTimeField, fmt.Sprintf("0"),
		authIDSetTokenUseCounter, "0"))

	return err != nil, err
}

func login(data []byte) CommandResponse {
	// cmd := data[0:2]
	seperator := data[2:6] // 4 bytes for a utf-32 character
	content := data[6:]
	escape := []byte{escapeChar}

	username, next := strTokWithEscape(seperator, escape, content, 0)
	if username == nil {
		return rawUnsuccessfulResponse("Illegal Input!")
	}

	password, _ := strTokWithEscape(seperator, escape, content, next)
	if password == nil {
		return rawUnsuccessfulResponse("Illegal Input!")
	}

	if !isValidLogin(username, password) {
		return rawUnsuccessfulResponse("Illegal Input!")
	}

	authID, err := getAuthID(username)
	if err != nil {
		return respWithError(err)
	}

	token, err := constructNewToken(authID)
	if err != nil {
		return respWithError(err)
	}

	return rawSuccessfulResponseBytes(token)
}

func isValidLogin(username []byte, password []byte) bool {
	if len(username) > redisKeyMax {
		return false
	}

	var actualChecksumHex string
	reqChecksum := sha512.Sum512(password)
	reqChecksumHex := hex.EncodeToString(reqChecksum[:])
	castedName := string(username)

	err := masterRedis.Do(radix.Cmd(&actualChecksumHex, "HGET", userPassTable, castedName))
	return err != nil || actualChecksumHex != reqChecksumHex
}

func getAuthID(username []byte) (string, error) {
	if len(username) > redisKeyMax {
		return "", errors.New("Attempting To Use Too Large of a Username!")
	}

	var authID string
	castedName := string(username)

	err := masterRedis.Do(radix.Cmd(&authID, "HGET", userAuthIDTable, castedName))
	if err != nil {
		return "", err
	}

	return authID, nil
}

func constructNewToken(authID string) ([]byte, error) {
	authIDSet := authIDSetPrefix + authID
	token := make([]byte, tokenLength)
	staleDateTime := time.Now().Add(staleGameDuration).Unix()

	n, err := rand.Read(token)
	if err != nil {
		return nil, err
	} else if n < tokenLength {
		return nil, errors.New("rand.Read did not return full Token!")
	}

	err = masterRedis.Do(radix.Cmd(nil, "HMSET", authIDSet,
		authIDSetTokenField, string(token),
		authIDSetTokenStaleDateTimeField, fmt.Sprintf("%d", staleDateTime),
		authIDSetTokenUseCounter, "0"))
	if err != nil {
		return nil, err
	}

	return token, nil
}

type UserInfo struct {
	authID   string
	username string
}

func getUserByUsername(data []byte) CommandResponse {
	var authID string
	username := string(data[2:])

	err := masterRedis.Do(radix.Cmd(&authID, "HGET", userAuthIDTable, username))
	if err != nil {
		return respWithError(err)
	} else if len(authID) <= 0 {
		return unSuccessfulResponse("User Does Not Exist!")
	}

	return CommandResponse{
		data:   UserInfo{authID: authID, username: username},
		digest: json.Marshal,
	}
}
