package data

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/policy"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/mediocregopher/radix/v3"
)

//// Configurables

//
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
// Register/Login Configurables

// Redis Key Password Hashing Salt
const passHashSaltKey string = "PasswordSalt"

// Number of bytes (not characters) for random salt.
const passHashSaltLen int = 128

// Password Hashing Salt
var passHashSalt string = ""

//
// Token Configurables

// Length of Characters For Secret User Authentication Token
const TokenLength int = 256

// Time A Token stays good for before it is rejected and a new login
// is required
const TokenStaleTime time.Duration = time.Minute * 5

// ServerTask Startup Function for Users. Takes care of initialization.
// Loads The Password Salt from the Hash if it does not already exist
func StartUsers() (func(), error) {
	var passHashSaltTemp string
	err := redis.MasterRedis.Do(radix.Cmd(&passHashSaltTemp, "GET", passHashSaltKey))
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
	} else {
		passHashSalt = passHashSaltTemp
	}

	return cleanUpUsers, nil
}

func cleanUpUsers() {
	log.Println("Cleaning Up User Logic")
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

	// It should be noted, username could be encoded in any type of way.... it could be a mess of bytes... don't trust it on reads.
	err := redis.MasterRedis.Do(radix.Cmd(&success, "HSETNX", UserPassTable, username, checksumHex))
	if err != nil {
		return false, err
	} else if success == 0 {
		return false, nil
	}

	err = redis.MasterRedis.Do(radix.Cmd(&newID, "INCR", AuthIDAtomicCounter))
	if err != nil {
		return false, err
	}

	err = redis.MasterRedis.Do(radix.Cmd(&success, "HSETNX", UserAuthIDTable, username, fmt.Sprintf("%d", newID)))
	if err != nil {
		return false, err
	} else if success == 0 {
		return false, errors.New("Atomic Counter Did Not Return Unique ID!: " + AuthIDAtomicCounter)
	}

	// TODO We can add other fields here
	err = redis.MasterRedis.Do(radix.Cmd(nil, "HSET", fmt.Sprintf(AuthIDSetPrefix+"%d", newID),
		AuthIDSetUsernameField, username,
		AuthIDSetTokenField, "",
		AuthIDSetTokenStaleDateTimeField, fmt.Sprintf("0"),
		AuthIDSetTokenUseCounter, "0"))

	return err == nil, err
}

// Deletes an account from the database, not that it would be particularly
// needed outside of unit testing
//
// returns bool :: true/false if the user can be deleted from the database
//        error :: if writing to the database failed it will be non-nil
func DeleteUser(username string) (bool, error) {
	var authID int
	var success int

	err := redis.MasterRedis.Do(radix.Cmd(&success, "HDEL", UserPassTable, username))
	if err != nil {
		return false, err
	} else if success == 0 {
		return false, nil
	}

	err = redis.MasterRedis.Do(radix.Cmd(&authID, "HGET", UserAuthIDTable, username))
	if err != nil {
		return false, err
	}

	err = redis.MasterRedis.Do(radix.Cmd(&success, "HDEL", UserAuthIDTable, username))
	if err != nil {
		return false, err
	} else if success == 0 {
		return false, errors.New("Could Not Delete Auth ID!")
	}

	err = redis.MasterRedis.Do(radix.Cmd(&success, "DEL", fmt.Sprintf(AuthIDSetPrefix+"%d", authID)))
	if err != nil {
		return false, err
	} else if success == 0 {
		return false, errors.New("Could Not Delete User Info!")
	}

	return true, nil
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
	if err != nil {
		log.Printf("Error in Loading Hash For User! Username: %s\n", username)
		return false
	}
	return actualChecksumHex == reqChecksumHex
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

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Public AuthID Command Handler
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// JSON Fields for the User Lookup Endpoint/Command
type GetUserCommandBody struct {
	Username string
}

// struct for ease of use when marshalling to JSON.
// Carries the fields used when a user is gathered
// from cmdGetUser
type UserInfo struct {
	AuthID   string
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
//// Signatures and Verification
////
///////////////////////////////////////////////////////////////////////////////////////////////////

type AuthToken struct {
	Token string
	Stale time.Time
	Uses  int
}

// Constructs a new token and deadline for the token going stale for
// a user. Usually occurs on a successful login. Token can be
// refreshed any number of times. It is then used for identity
// authentication in future requests.
func ConstructNewToken(authID string) ([]byte, time.Time, error) {
	authIDSet := AuthIDSetPrefix + authID
	token := make([]byte, TokenLength)
	staleDateTime := time.Now().UTC().Add(TokenStaleTime)

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

func GetToken(authID string) (AuthToken, error) {
	res := AuthToken{}
	authIDSet := AuthIDSetPrefix + authID
	redisReply := make([]string, 3)
	err := redis.MasterRedis.Do(radix.Cmd(&redisReply, "HMGET", authIDSet,
		AuthIDSetTokenField,
		AuthIDSetTokenStaleDateTimeField,
		AuthIDSetTokenUseCounter))

	if err != nil {
		return res, err
	}

	// Token
	res.Token = redisReply[0]

	// Time
	staleTimeUnix, err := strconv.Atoi(redisReply[1])
	if err != nil {
		return res, err
	}
	res.Stale = time.Unix(int64(staleTimeUnix), 0)

	res.Uses, err = strconv.Atoi(redisReply[2])
	if err != nil {
		return res, err
	}

	return res, nil
}

func IncrementTokenUses(authID string, newCount int) error {
	authIDSet := AuthIDSetPrefix + authID

	// TODO Slight Security Vulnerability
	// There is a race condition in which multiple threads can set/unset a counter depending
	// on the speed of the verification. This could be solved by making the AuthIDSetTokenUseCounter it's
	// own key in redis and WATCH MULT EXEC the set. (See Redis Transactions)
	err := redis.MasterRedis.Do(radix.Cmd(nil, "HSET", authIDSet,
		AuthIDSetTokenUseCounter, fmt.Sprintf("%d", newCount+1)))
	if err != nil {
		return err
	}

	return nil
}
