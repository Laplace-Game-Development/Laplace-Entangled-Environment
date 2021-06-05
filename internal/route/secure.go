package route

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
	"laplace-entangled-env.com/internal/policy"
	"laplace-entangled-env.com/internal/redis"
	"laplace-entangled-env.com/internal/util"
)

// Redis DB Configurables
const UserPassTable string = "userPassword"
const AuthIDAtomicCounter string = "authIDAtomicCounter"
const UserAuthIDTable string = "userToAuthID"

// User Table
const AuthIDSetPrefix string = "authID:"
const AuthIDSetUsernameField string = "username"
const AuthIDSetTokenField string = "token"
const AuthIDSetTokenStaleDateTimeField string = "stale"
const AuthIDSetTokenUseCounter string = "tokenUses"

// Encryption Configurables
const CrtLocation string = "./tlscert.crt"
const KeyLocation string = "./tlskey.key"
const EscapeChar byte = byte('~')

// Token Configurables
const TokenLength int = 256
const TokenStaleTime time.Duration = time.Minute * 5
const SuperUserTokenCount int = 10

// Super User Configurables
const SuperUserID string = "-1"

// This will be assigned on startup then left unchanged
var tlsConfig tls.Config = tls.Config{}

// This Map is a Set!
// This should never change during runtime!
var secureMap map[policy.ClientCmd]bool = map[policy.ClientCmd]bool{
	policy.CmdRegister: true,
	policy.CmdLogin:    true,
}

type UserInfo struct {
	AuthID   string
	Username string
}

type SuperUserRequest struct {
	Header             policy.RequestHeader
	BodyFactories      policy.RequestBodyFactories
	IsSecureConnection bool
}

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

func NeedsSecurity(cmd policy.ClientCmd) bool {
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
func Register(header policy.RequestHeader, bodyFactories policy.RequestBodyFactories, isSecureConnection bool) policy.CommandResponse {
	if !isSecureConnection {
		return policy.RawUnsuccessfulResponse("Unsecure Connection!")
	}

	rqBody := RegisterCommandBody{}
	bodyFactories.ParseFactory(&rqBody)

	if rqBody.Username == "" {
		return policy.RawUnsuccessfulResponse("Illegal Input!")
	} else if !passwordIsStrong(rqBody.Password) {
		return policy.RawUnsuccessfulResponse("Weak Password!")
	}

	success, err := CreateAccount(rqBody.Username, rqBody.Password)
	if err != nil {
		return policy.RespWithError(err)
	} else if success {
		return policy.RawSuccessfulResponse(rqBody.Username)
	} else {
		return policy.RawUnsuccessfulResponse("Username Already Exists!")
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

func CreateAccount(username string, password string) (bool, error) {
	if len(username) > redis.RedisKeyMax {
		return false, errors.New("Attempting To Store Too Large of a Username!")
	}

	var newID int
	var success int
	checksum := sha512.Sum512([]byte(password))
	checksumHex := hex.EncodeToString(checksum[:])
	castedName := string(username)

	// It should be noted, username could be encoded in any type of way.... it could be a mess of bytes... don't trust it on reads.
	// TODO add Pipelining
	err := redis.MasterRedis.Do(radix.Cmd(&success, "HSETNX", UserPassTable, castedName, checksumHex))
	if err != nil {
		return false, err
	} else if success == 0 {
		return false, nil
	}

	err = redis.MasterRedis.Do(radix.Cmd(&newID, "INCR", AuthIDAtomicCounter))
	if err != nil {
		return false, err
	}

	err = redis.MasterRedis.Do(radix.Cmd(&success, "HSETNX", UserAuthIDTable, castedName, fmt.Sprintf("%d", newID)))
	if err != nil {
		return false, err
	} else if success == 0 {
		return false, errors.New("Atomic Counter Did Not Return Unique ID!: " + AuthIDAtomicCounter)
	}

	// TODO We can add other fields here
	err = redis.MasterRedis.Do(radix.Cmd(nil, "HMSET", fmt.Sprintf(AuthIDSetPrefix+"%d", newID),
		AuthIDSetUsernameField, castedName,
		AuthIDSetTokenField, "",
		AuthIDSetTokenStaleDateTimeField, fmt.Sprintf("0"),
		AuthIDSetTokenUseCounter, "0"))

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

func Login(header policy.RequestHeader, bodyFactories policy.RequestBodyFactories, isSecureConnection bool) policy.CommandResponse {
	if !isSecureConnection {
		return policy.RawUnsuccessfulResponse("Unsecure Connection!")
	}

	rqBody := LoginCommandBody{}
	bodyFactories.ParseFactory(&rqBody)

	if !IsValidLogin(rqBody.Username, rqBody.Password) {
		return policy.RawUnsuccessfulResponse("Illegal Input!")
	}

	authID, err := getAuthID(rqBody.Username)
	if err != nil {
		return policy.RespWithError(err)
	}

	token, _, err := ConstructNewToken(authID)
	if err != nil {
		return policy.RespWithError(err)
	}

	return policy.RawSuccessfulResponseBytes(&token)
}

func IsValidLogin(username string, password string) bool {
	if len(username) > redis.RedisKeyMax {
		return false
	}

	var actualChecksumHex string
	reqChecksum := sha512.Sum512([]byte(password))
	reqChecksumHex := hex.EncodeToString(reqChecksum[:])
	castedName := string(username)

	err := redis.MasterRedis.Do(radix.Cmd(&actualChecksumHex, "HGET", UserPassTable, castedName))
	return err != nil || actualChecksumHex != reqChecksumHex
}

func getAuthID(username string) (string, error) {
	if len(username) > redis.RedisKeyMax {
		return "", errors.New("Attempting To Use Too Large of a Username!")
	}

	var authID string
	castedName := string(username)

	err := redis.MasterRedis.Do(radix.Cmd(&authID, "HGET", UserAuthIDTable, castedName))
	if err != nil {
		return "", err
	}

	return authID, nil
}

func ConstructNewToken(authID string) ([]byte, time.Time, error) {
	authIDSet := AuthIDSetPrefix + authID
	token := make([]byte, TokenLength)
	staleDateTime := time.Now().UTC().Add(policy.StaleGameDuration)

	n, err := rand.Read(token)
	if err != nil {
		return nil, staleDateTime, err
	} else if n < TokenLength {
		return nil, staleDateTime, errors.New("rand.Read did not return full Token!")
	}

	err = redis.MasterRedis.Do(radix.Cmd(nil, "HMSET", authIDSet,
		AuthIDSetTokenField, string(token),
		AuthIDSetTokenStaleDateTimeField, fmt.Sprintf("%d", staleDateTime.Unix()),
		AuthIDSetTokenUseCounter, "0"))
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

func GetUser(header policy.RequestHeader, bodyFactories policy.RequestBodyFactories, isSecureConnection bool) policy.CommandResponse {
	rqBody := GetUserCommandBody{}
	bodyFactories.ParseFactory(&rqBody)

	var authID string
	err := redis.MasterRedis.Do(radix.Cmd(&authID, "HGET", UserAuthIDTable, rqBody.Username))
	if err != nil {
		return policy.RespWithError(err)
	} else if len(authID) <= 0 {
		return policy.UnSuccessfulResponse("User Does Not Exist!")
	}

	return policy.CommandResponse{
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
func RequestWithSuperUser(isTask bool, cmd policy.ClientCmd, args interface{}) (SuperUserRequest, error) {
	// shortcut bodyfactory using reflection
	bodyFactories := policy.RequestBodyFactories{
		ParseFactory: func(ptr interface{}) error {
			ptrValue := reflect.ValueOf(ptr)
			argsVal := reflect.ValueOf(args)
			ptrValue.Elem().Set(argsVal)
			return nil
		},
		SigVerify: func(userID string, userSig string) error {
			return nil
		},
	}

	// Body Start is only used in main.go and is not necessary for a manual request command
	header := policy.RequestHeader{Command: cmd, UserID: SuperUserID}

	return SuperUserRequest{Header: header, BodyFactories: bodyFactories, IsSecureConnection: true}, nil
}

// Cleanup SuperUser Code
func SigVerification(userID string, signature string, content *[]byte) error {
	authIDSet := AuthIDSetPrefix + userID
	redisReply := make([]string, 3)
	err := redis.MasterRedis.Do(radix.Cmd(&redisReply, "HMGET", authIDSet,
		AuthIDSetTokenField,
		AuthIDSetTokenStaleDateTimeField,
		AuthIDSetTokenUseCounter))

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
	err = util.Concat(&input, content, 0)
	if err != nil {
		return err
	}

	err = util.Concat(&input, &token, contentLen)
	if err != nil {
		return err
	}

	err = util.Concat(&input, &counterByte, contentLen+tokenLen)
	if err != nil {
		return err
	}

	checksumByte := sha256.Sum256(input)
	checksum := string(checksumByte[:])
	if signature == checksum {
		err = redis.MasterRedis.Do(radix.Cmd(nil, "HSET", authIDSet,
			AuthIDSetTokenUseCounter, fmt.Sprintf("%d", counter+1)))
		if err != nil {
			return err
		}

		// SUCCESS!
		return nil
	}

	return errors.New(fmt.Sprintf("Signature is Incorrect!: %s vs %s", signature, checksum))
}
