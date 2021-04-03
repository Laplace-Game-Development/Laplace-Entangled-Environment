package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
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
const superUserID string = "-1"

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
	AuthID   string
	Username string
}

func getUser(prefix RequestPrefix, header RequestHeader, body []byte) CommandResponse {
	username := string(body[2:])

	var authID string
	err := masterRedis.Do(radix.Cmd(&authID, "HGET", userAuthIDTable, username))
	if err != nil {
		return respWithError(err)
	} else if len(authID) <= 0 {
		return unSuccessfulResponse("User Does Not Exist!")
	}

	return CommandResponse{
		Data:   UserInfo{AuthID: authID, Username: username},
		Digest: json.Marshal,
	}
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Utility Security Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func requestWithSuperUser(cmd ClientCmd, args interface{}) (RequestPrefix, RequestHeader, []byte, error) {
	token, err := constructNewToken(superUserID)
	if err != nil {
		return RequestPrefix{}, RequestHeader{}, nil, err
	}

	prefix := RequestPrefix{IsEncoded: false, IsJSON: false, Command: cmd}

	body, err := json.Marshal(args)
	if err != nil {
		return RequestPrefix{}, RequestHeader{}, nil, err
	}

	bodyLen := len(body)
	tokenLen := len(token)
	counterLen := len("0")

	input := make([]byte, bodyLen+tokenLen+counterLen)
	err = concat(&input, body, 0)
	if err != nil {
		return RequestPrefix{}, RequestHeader{}, nil, err
	}

	err = concat(&input, token, bodyLen)
	if err != nil {
		return RequestPrefix{}, RequestHeader{}, nil, err
	}

	err = concat(&input, []byte("0"), bodyLen+tokenLen)
	if err != nil {
		return RequestPrefix{}, RequestHeader{}, nil, err
	}

	checksumByte := sha256.Sum256(input)
	checksum := string(checksumByte[:])

	// Body Start is only used in main.go and is not necessary for a manual request command
	header := RequestHeader{UserID: superUserID, Sig: checksum}

	return prefix, header, body, nil
}

func verifySig(userID string, signature string, content []byte) error {
	authIDSet := authIDSetPrefix + userID
	redisReply := make([]string, 3)
	err := masterRedis.Do(radix.Cmd(&redisReply, "HMGET", authIDSet,
		authIDSetTokenField,
		authIDSetTokenStaleDateTimeField,
		authIDSetTokenUseCounter))

	if err != nil {
		return err
	}

	token := []byte(redisReply[0])
	staleTimeUnix, err := strconv.Atoi(redisReply[1])
	if err != nil {
		return err
	}

	staleTime := time.Unix(int64(staleTimeUnix), 0)
	counter, err := strconv.Atoi(redisReply[2])
	if err != nil {
		return err
	}

	if staleTime.Before(time.Now().UTC()) {
		return errors.New("Token Is Stale!")
	}

	contentLen := len(content)
	tokenLen := len(token)
	counterLen := len(redisReply[2])

	input := make([]byte, contentLen+tokenLen+counterLen)
	err = concat(&input, content, 0)
	if err != nil {
		return err
	}

	err = concat(&input, token, contentLen)
	if err != nil {
		return err
	}

	err = concat(&input, []byte(redisReply[2]), contentLen+tokenLen)
	if err != nil {
		return err
	}

	checksumByte := sha256.Sum256(input)
	checksum := string(checksumByte[:])
	if signature == checksum {
		err = masterRedis.Do(radix.Cmd(nil, "HSET", authIDSet,
			authIDSetTokenUseCounter, fmt.Sprintf("%d", counter+1)))
		if err != nil {
			return err
		}

		return nil
	}

	return errors.New(fmt.Sprintf("Signature is Incorrect!: %s vs %s", signature, checksum))
}
