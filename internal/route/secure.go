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
	"strconv"
	"time"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/policy"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/util"
	"github.com/mediocregopher/radix/v3"
)

//// Configurables

// Redis DB Configurables

// Redis Key for the User Password HashMap
const UserPassTable string = "userPassword"

// Redis Key for the Atomic UserID Counter
const AuthIDAtomicCounter string = "authIDAtomicCounter"

// Redis Key for the Username to UserID HashMap
const UserAuthIDTable string = "userToAuthID"

//
// User Table

// Redis HashTable Key Prefix for User IDs. Concatenated
// with a UserID for a HashTable they own
const AuthIDSetPrefix string = "authID:"

// Username Key/Field for Redis UserID HashTable
const AuthIDSetUsernameField string = "username"

// Token Key/Field for Redis UserID HashTable
const AuthIDSetTokenField string = "token"

// Token Deadline DateTime Key/Field for Redis UserID HashTable
const AuthIDSetTokenStaleDateTimeField string = "stale"

// Token Use Counter Key/Field for Redis UserID HashTable
const AuthIDSetTokenUseCounter string = "tokenUses"

//
// Encryption Configurables

// TLS Certificate File Location from root of the project
const CrtLocation string = "./tlscert.crt"

// TLS Key File Location from root of the project
const KeyLocation string = "./tlskey.key"

//
// Token Configurables

// Length of Characters For Secret User Authentication Token
const TokenLength int = 256

// Time A Token stays good for before it is rejected and a new login
// is required
const TokenStaleTime time.Duration = time.Minute * 5

//
// Register/Login Configurables

// Redis Key Password Hashing Salt
const passHashSaltKey string = "PasswordSalt"

// Number of bytes (not characters) for random salt.
const passHashSaltLen int = 128

// Password Hashing Salt
var passHashSalt string = ""

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

// struct for ease of use when marshalling to JSON.
// Carries the fields used when a user is gathered
// from cmdGetUser
type UserInfo struct {
	AuthID   string
	Username string
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

	var passHashSaltTemp string
	err = redis.MasterRedis.Do(radix.Cmd(&passHashSaltTemp, "GET", passHashSaltKey))
	if err != nil {
		return nil, err
	} else if passHashSaltTemp == "" {
		byteTemp := make([]byte, passHashSaltLen)
		n, err := rand.Read(byteTemp)
		if err != nil || n < 128 {
			return nil, err
		}

		passHashSalt = string(byteTemp)
		redis.MasterRedis.Do(radix.Cmd(nil, "SET", passHashSaltKey, passHashSalt))
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

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Registration
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// JSON Fields for the Register Endpoint/Command
type RegisterCommandBody struct {
	Username string
	Password string
}

// Register Endpoint. Registers a user to the database. It requires a unique username/identifier
// and a relatively strong password
//
// TODO(TFlexSoom): Add rate Limiting
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

// Takes a string and returns if it would be a strong password.
// returns -> true if it is strong and false otherwise
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

// Adds an account to the database, hashing the password and associating
// starting values in all typical fields in redis
//
// returns bool :: true/false if the user can be added to the database
//        error :: if writing to the database failed it will be non-nil
func CreateAccount(username string, password string) (bool, error) {
	if len(username) > redis.RedisKeyMax {
		return false, errors.New("Attempting To Store Too Large of a Username!")
	}

	var newID int
	var success int
	checksum := sha512.Sum512([]byte(passHashSalt + password))
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
	err = redis.MasterRedis.Do(radix.Cmd(nil, "HSET", fmt.Sprintf(AuthIDSetPrefix+"%d", newID),
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

// JSON Fields for the Login Endpoint/Command
type LoginCommandBody struct {
	Username string
	Password string
}

// Login a user to receive a valid token to continue making requests
// under. The connection must be secure and correctly formatted
// otherwise an error will be returned.
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

// Returns if the given login is valid or invalid based on
// username and hashed password. If it exists in the UserPass
// hashMap then it is a valid Username + Password Combination.
func IsValidLogin(username string, password string) bool {
	if len(username) > redis.RedisKeyMax {
		return false
	}

	var actualChecksumHex string
	reqChecksum := sha512.Sum512([]byte(passHashSalt + password))
	reqChecksumHex := hex.EncodeToString(reqChecksum[:])
	castedName := string(username)

	err := redis.MasterRedis.Do(radix.Cmd(&actualChecksumHex, "HGET", UserPassTable, castedName))
	return err != nil || actualChecksumHex != reqChecksumHex
}

// Returns the UserID (numerical but put into a string for
// ease of response) for a given username. Used for login
// with specific error handling used for the use case flow.
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

// Constructs a new token and deadline for the token going stale for
// a user. Usually occurs on a successful login. Token can be
// refreshed any number of times. It is then used for identity
// authentication in future requests.
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

	err = redis.MasterRedis.Do(radix.Cmd(nil, "HSET", authIDSet,
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

// JSON Fields for the User Lookup Endpoint/Command
type GetUserCommandBody struct {
	Username string
}

// Endpoint Returns the User ID associated with the supplied username. Useful for finding friends
// and connecting other information.
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

// Typical Verification of users for authentication. Used in most
// other endpoints as SigVerify in RequestBodyFactories
//
// Takes the userID, Signature (hash of token and content), and content
// to see if the user can indeed make the request (they are who they say
// they are).
//
// returns an error if they are not who they say they are.
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
