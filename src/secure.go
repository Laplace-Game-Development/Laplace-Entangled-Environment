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

// Encryption Configurables
const crtLocation string = "./tlscert.crt"
const keyLocation string = "./tlskey.key"
const escapeChar byte = byte('~')

// Token Configurables
const tokenLength int = 256
const tokenStaleTime time.Duration = time.Minute * 5
const superUserTokenCount int = 10

// This will be assigned on startup then left unchanged
var tlsConfig tls.Config = tls.Config{}

// This Map is a Set!
// This should never change during runtime!
var secureMap map[ClientCmd]bool = map[ClientCmd]bool{
	cmdRegister: true,
	cmdNewToken: true,
}

// This Map is a Set!
// This should never change during runtime
var superUserIdMap map[string]bool = map[string]bool{
	"-1":  true,
	"-2":  true,
	"-3":  true,
	"-4":  true,
	"-5":  true,
	"-6":  true,
	"-7":  true,
	"-8":  true,
	"-9":  true,
	"-10": true,
}

type UserInfo struct {
	AuthID   string
	Username string
}

type SuperUserToken struct {
	UserID    string
	Token     []byte
	StaleDate time.Time
}

type SuperUserRequest struct {
	UserToken SuperUserToken
	Prefix    RequestPrefix
	Header    RequestHeader
	Body      []byte
	Cleanup   func()
}

// Global Variables | Singletons
var superUserAvailableTokens chan SuperUserToken = make(chan SuperUserToken, superUserTokenCount)

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

	// Load Super User Tokens
	var token []byte
	var staleDate time.Time
	for userID := range superUserIdMap {
		token, staleDate, err = constructNewToken(userID)
		if err != nil {
			return nil, err
		}

		superUserAvailableTokens <- SuperUserToken{UserID: userID, Token: token, StaleDate: staleDate}
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

	token, _, err := constructNewToken(authID)
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

func constructNewToken(authID string) ([]byte, time.Time, error) {
	authIDSet := authIDSetPrefix + authID
	token := make([]byte, tokenLength)
	staleDateTime := time.Now().UTC().Add(staleGameDuration)

	n, err := rand.Read(token)
	if err != nil {
		return nil, staleDateTime, err
	} else if n < tokenLength {
		return nil, staleDateTime, errors.New("rand.Read did not return full Token!")
	}

	err = masterRedis.Do(radix.Cmd(nil, "HMSET", authIDSet,
		authIDSetTokenField, string(token),
		authIDSetTokenStaleDateTimeField, fmt.Sprintf("%d", staleDateTime.Unix()),
		authIDSetTokenUseCounter, "0"))
	if err != nil {
		return nil, staleDateTime, err
	}

	return token, staleDateTime, nil
}

func getUser(prefix RequestPrefix, header RequestHeader, body []byte) CommandResponse {
	username := string(body[:])

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

// isTask: True --> will Block
// isTask: False --> will Response with Error
func getSuperUserToken(isTask bool) (SuperUserToken, error) {
	var userToken SuperUserToken

	if isTask {
		userToken = <-superUserAvailableTokens
	} else {
		select {
		case userToken = <-superUserAvailableTokens:
			break
		default:
			return SuperUserToken{}, errors.New("Could Not Acquire Free Token!")
		}
	}

	if userToken.StaleDate.Before(time.Now().UTC().Add(time.Minute)) {
		newToken, staleDate, err := constructNewToken(userToken.UserID)
		if err != nil {
			return SuperUserToken{}, err
		}

		userToken.Token = newToken
		userToken.StaleDate = staleDate
	}

	return userToken, nil
}

// Use this to return tokens acquired from getSuperUserToken
func cleanupSuperUserToken(userToken SuperUserToken) {
	superUserAvailableTokens <- userToken
}

// When using, make sure to defer SuperUserRequest.Cleanup on a succssful call
func requestWithSuperUser(isTask bool, cmd ClientCmd, args interface{}) (SuperUserRequest, error) {
	userToken, err := getSuperUserToken(isTask)
	if err != nil {
		return SuperUserRequest{}, err
	}

	prefix := RequestPrefix{IsEncoded: false, IsJSON: false, Command: cmd}

	body, err := json.Marshal(args)
	if err != nil {
		return SuperUserRequest{}, err
	}

	bodyLen := len(body)
	tokenLen := len(userToken.Token)
	counterLen := len("0")

	input := make([]byte, bodyLen+tokenLen+counterLen)
	err = concat(&input, body, 0)
	if err != nil {
		return SuperUserRequest{}, err
	}

	err = concat(&input, userToken.Token, bodyLen)
	if err != nil {
		return SuperUserRequest{}, err
	}

	err = concat(&input, []byte("0"), bodyLen+tokenLen)
	if err != nil {
		return SuperUserRequest{}, err
	}

	checksumByte := sha256.Sum256(input)
	checksum := string(checksumByte[:])

	// Body Start is only used in main.go and is not necessary for a manual request command
	header := RequestHeader{UserID: userToken.UserID, Sig: checksum}
	cleanup := func() { cleanupSuperUserToken(userToken) }

	return SuperUserRequest{UserToken: userToken, Prefix: prefix, Header: header, Body: body, Cleanup: cleanup}, nil
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

		_, exists := superUserIdMap[userID]
		if !exists {
			err = masterRedis.Do(radix.Cmd(nil, "HSET", authIDSet,
				authIDSetTokenUseCounter, fmt.Sprintf("%d", counter+1)))
			if err != nil {
				return err
			}
		}

		return nil
	}

	return errors.New(fmt.Sprintf("Signature is Incorrect!: %s vs %s", signature, checksum))
}
