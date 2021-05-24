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
	"reflect"
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

// Super User Configurables
const superUserID string = "-1"

// This will be assigned on startup then left unchanged
var tlsConfig tls.Config = tls.Config{}

// This Map is a Set!
// This should never change during runtime!
var secureMap map[ClientCmd]bool = map[ClientCmd]bool{
	cmdRegister: true,
	cmdLogin:    true,
}

type UserInfo struct {
	AuthID   string
	Username string
}

type SuperUserRequest struct {
	Header             RequestHeader
	BodyFactories      RequestBodyFactories
	IsSecureConnection bool
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
	log.Println("Cleaning Up Encryption Logic")
}

// Secure the current TCP Listener connection. Return True if a new Connection was created
// Return an error if somethign went wrong
func secureTCPConnIfNeeded(clientConn *TCPClientConn, prefix TCPRequestPrefix) (bool, error) {
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

func needsSecurity(cmd ClientCmd) bool {
	result, exists := secureMap[cmd]
	return exists && result
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Registration
////
///////////////////////////////////////////////////////////////////////////////////////////////////

type RegisterCommandBody struct {
	Username string
	Password string
}

// TODO Add rate Limiting
func register(header RequestHeader, bodyFactories RequestBodyFactories, isSecureConnection bool) CommandResponse {
	if !isSecureConnection {
		return rawUnsuccessfulResponse("Unsecure Connection!")
	}

	rqBody := RegisterCommandBody{}
	bodyFactories.parseFactory(&rqBody)

	if rqBody.Username == "" {
		return rawUnsuccessfulResponse("Illegal Input!")
	} else if !passwordIsStrong(rqBody.Password) {
		return rawUnsuccessfulResponse("Weak Password!")
	}

	success, err := createAccount(rqBody.Username, rqBody.Password)
	if err != nil {
		return respWithError(err)
	} else if success {
		return rawSuccessfulResponse(rqBody.Username)
	} else {
		return rawUnsuccessfulResponse("Username Already Exists!")
	}
}

func passwordIsStrong(password string) bool {
	length := len(password)
	hasUpper, hasLower, hasNumber, hasSymbol, hasMystery := false, false, false, false, false
	passwordIsStrong := false
	var runic rune

	for _, c := range password {
		runic = rune(c)
		hasUpper = hasUpper || (runic >= 'A' && runic <= 'Z')
		hasLower = hasLower || (runic >= 'a' && runic <= 'z')
		hasNumber = hasNumber || (runic >= '0' && runic <= '9')
		hasSymbol = hasNumber || (runic >= '!' && runic <= '/') ||
			(runic >= ':' && runic <= '@') ||
			(runic >= '[' && runic <= '`') ||
			(runic >= '{' && runic <= '~')
		hasMystery = runic > 127

		if hasMystery || (hasUpper && hasLower && hasNumber) || (hasUpper && hasLower && hasSymbol) {
			passwordIsStrong = true
			break
		}
	}

	return length >= 8 && passwordIsStrong
}

func createAccount(username string, password string) (bool, error) {
	if len(username) > redisKeyMax {
		return false, errors.New("Attempting To Store Too Large of a Username!")
	}

	var newID int
	var success int
	checksum := sha512.Sum512([]byte(password))
	checksumHex := hex.EncodeToString(checksum[:])
	castedName := string(username)

	// It should be noted, username could be encoded in any type of way.... it could be a mess of bytes... don't trust it on reads.
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

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Login
////
///////////////////////////////////////////////////////////////////////////////////////////////////
type LoginCommandBody struct {
	Username string
	Password string
}

func login(header RequestHeader, bodyFactories RequestBodyFactories, isSecureConnection bool) CommandResponse {
	if !isSecureConnection {
		return rawUnsuccessfulResponse("Unsecure Connection!")
	}

	rqBody := LoginCommandBody{}
	bodyFactories.parseFactory(&rqBody)

	if !isValidLogin(rqBody.Username, rqBody.Password) {
		return rawUnsuccessfulResponse("Illegal Input!")
	}

	authID, err := getAuthID(rqBody.Username)
	if err != nil {
		return respWithError(err)
	}

	token, _, err := constructNewToken(authID)
	if err != nil {
		return respWithError(err)
	}

	return rawSuccessfulResponseBytes(&token)
}

func isValidLogin(username string, password string) bool {
	if len(username) > redisKeyMax {
		return false
	}

	var actualChecksumHex string
	reqChecksum := sha512.Sum512([]byte(password))
	reqChecksumHex := hex.EncodeToString(reqChecksum[:])
	castedName := string(username)

	err := masterRedis.Do(radix.Cmd(&actualChecksumHex, "HGET", userPassTable, castedName))
	return err != nil || actualChecksumHex != reqChecksumHex
}

func getAuthID(username string) (string, error) {
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

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Public AuthID Command Handler
////
///////////////////////////////////////////////////////////////////////////////////////////////////
type GetUserCommandBody struct {
	Username string
}

func getUser(header RequestHeader, bodyFactories RequestBodyFactories, isSecureConnection bool) CommandResponse {
	rqBody := GetUserCommandBody{}
	bodyFactories.parseFactory(&rqBody)

	var authID string
	err := masterRedis.Do(radix.Cmd(&authID, "HGET", userAuthIDTable, rqBody.Username))
	if err != nil {
		return respWithError(err)
	} else if len(authID) <= 0 {
		return unSuccessfulResponse("User Does Not Exist!")
	}

	return CommandResponse{
		Data:   UserInfo{AuthID: authID, Username: rqBody.Username},
		Digest: json.Marshal,
	}
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Utility Security Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// When using, make sure to defer SuperUserRequest.Cleanup on a succssful call
func requestWithSuperUser(isTask bool, cmd ClientCmd, args interface{}) (SuperUserRequest, error) {
	// shortcut bodyfactory using reflection
	bodyFactories := RequestBodyFactories{
		parseFactory: func(ptr interface{}) error {
			ptrValue := reflect.ValueOf(ptr)
			argsVal := reflect.ValueOf(args)
			ptrValue.Elem().Set(argsVal)
			return nil
		},
		sigVerify: func(userID string, userSig string) error {
			return nil
		},
	}

	// Body Start is only used in main.go and is not necessary for a manual request command
	header := RequestHeader{Command: cmd, UserID: superUserID}

	return SuperUserRequest{Header: header, BodyFactories: bodyFactories, IsSecureConnection: true}, nil
}

// Cleanup SuperUser Code
func sigVerification(userID string, signature string, content *[]byte) error {
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
	counterByte := []byte(redisReply[2])
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

	contentLen := len(*content)
	tokenLen := len(token)
	counterLen := len(redisReply[2])

	input := make([]byte, contentLen+tokenLen+counterLen)
	err = concat(&input, content, 0)
	if err != nil {
		return err
	}

	err = concat(&input, &token, contentLen)
	if err != nil {
		return err
	}

	err = concat(&input, &counterByte, contentLen+tokenLen)
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

		// SUCCESS!
		return nil
	}

	return errors.New(fmt.Sprintf("Signature is Incorrect!: %s vs %s", signature, checksum))
}
