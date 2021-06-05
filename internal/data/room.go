package data

import (
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/mediocregopher/radix/v3"
	"laplace-entangled-env.com/internal/event"
	"laplace-entangled-env.com/internal/policy"
	"laplace-entangled-env.com/internal/redis"
)

// Table / Datastructure Names
const GameListName string = "gameList"
const GameHashSetName string = "gameHash"
const MetadataHashSetName string = "metadataHash"
const OwnerHashSetName string = "ownerMapGame"
const PlayerSetPrefix string = "roster:"
const GameAtomicCounter string = "gameCountInteger"
const EmptyName string = "empty"

// Other Constants
const NumberOfGames = 20
const ThrottleGames = false

func StartRoomsSystem() (func(), error) {
	err := redis.MasterRedis.Do(radix.Cmd(nil, "SETNX", GameAtomicCounter, "0"))
	if err != nil {
		return nil, err
	}

	return cleanUpRoomSystem, nil
}

func cleanUpRoomSystem() {
	log.Println("Cleaning Up Room Logic")
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Game Manipulation
////
///////////////////////////////////////////////////////////////////////////////////////////////////

type GameWelcomeData struct {
	Id         string
	NumPlayers uint16 `json:",string"`
	Data       string
}

// For Static game details
type GameMetadata struct {
	Id        string
	Owner     string
	CreatedAt int64 `json:",string"`
	LastUsed  int64 `json:",string"`
}

// Used for Join Game, Leave Game
type SelectGameArgs struct {
	GameID string
}

func CreateGame(header policy.RequestHeader, bodyFactories policy.RequestBodyFactories, isSecureConnection bool) policy.CommandResponse {
	err := bodyFactories.SigVerify(header.UserID, header.Sig)
	if err != nil {
		log.Printf("Unauthorized Attempt! Error: %v\n", err)
		return policy.UnSuccessfulResponse("Unauthorized!")
	}

	var atomicClockValue int64
	var success int
	var players int

	canCreateGame, err := CanCreateGame(header.UserID)

	if err != nil {
		return policy.RespWithError(err)
	} else if !canCreateGame {
		return policy.CommandResponse{
			Data:   GameMetadata{Id: "", Owner: "", CreatedAt: 0},
			Digest: json.Marshal,
		}
	}
	//// We are good to create game

	// Increment atomic counters to get next index
	err = redis.MasterRedis.Do(radix.Cmd(&atomicClockValue, "INCR", GameAtomicCounter))
	if err != nil {
		return policy.RespWithError(err)
	}

	gameID := StringIDFromNumbers(atomicClockValue)
	metadata := GameMetadata{Id: gameID, Owner: header.UserID, CreatedAt: time.Now().UTC().Unix(), LastUsed: time.Now().UTC().Unix()}
	serializedMetadata := SerializeMetadata(metadata)

	// TODO Add Pipelining
	err = redis.MasterRedis.Do(radix.Cmd(&success, "HSETNX", GameHashSetName, gameID, "{}"))
	if err != nil || success == 0 {
		return policy.RespWithError(err)
	}

	err = redis.MasterRedis.Do(radix.Cmd(&success, "HSETNX", MetadataHashSetName, gameID, serializedMetadata))
	if err != nil || success == 0 {
		return policy.RespWithError(err)
	}

	err = redis.MasterRedis.Do(radix.Cmd(&players, "SADD", PlayerSetPrefix+gameID, header.UserID))
	if err != nil || players != 1 {
		return policy.RespWithError(err)
	}

	err = redis.MasterRedis.Do(radix.Cmd(nil, "RPUSH", GameListName, gameID))
	if err != nil {
		return policy.RespWithError(err)
	}

	// We can also do other things here like push metadata or channel numbers under different keys/tables.
	// As long as the gameID is an identifier.

	return policy.CommandResponse{
		Data:   metadata,
		Digest: json.Marshal,
	}
}

func CanCreateGame(authID string) (bool, error) {
	var success int
	var games int

	// Find the number of games
	// We could also use metadataHashSetName Here
	err := redis.MasterRedis.Do(radix.Cmd(&games, "HLEN", GameHashSetName))
	if err != nil {
		return false, err
	}

	// Throttle Number of Games
	// Cannot create too many games
	if games >= NumberOfGames && ThrottleGames {
		return false, nil
	}

	// Check if user already has a game
	err = redis.MasterRedis.Do(radix.Cmd(&success, "HEXISTS", OwnerHashSetName, authID))
	if err != nil {
		// TODO Redirect to Get Game Info
		return false, err
	}

	return success != 0, nil
}

func JoinGame(header policy.RequestHeader, bodyFactories policy.RequestBodyFactories, isSecureConnection bool) policy.CommandResponse {
	err := bodyFactories.SigVerify(header.UserID, header.Sig)
	if err != nil {
		log.Printf("Unauthorized Attempt! Error: %v\n", err)
		return policy.UnSuccessfulResponse("Unauthorized!")
	}

	args := SelectGameArgs{}
	err = bodyFactories.ParseFactory(&args)
	if err != nil {
		log.Printf("Bad Argument! Error: %v\n", err)
		return policy.UnSuccessfulResponse("Bad Arguments!")
	}

	var gameDataSerialized string
	var success int
	var numPlayers uint16

	err = redis.MasterRedis.Do(radix.Cmd(&gameDataSerialized, "HGET", GameHashSetName, args.GameID))
	if err != nil {
		return policy.RespWithError(err)
	} else if gameDataSerialized == "" {
		return policy.CommandResponse{
			Data:   GameWelcomeData{Id: "", NumPlayers: 0, Data: ""},
			Digest: json.Marshal,
		}
	}

	err = redis.MasterRedis.Do(radix.Cmd(&success, "SADD", PlayerSetPrefix+args.GameID, header.UserID))
	if err != nil {
		return policy.RespWithError(err)
	} else if success < 1 {
		log.Printf("User Tried to Add Themselves More Than Once: " + args.GameID)
	}

	err = redis.MasterRedis.Do(radix.Cmd(&numPlayers, "SCARD", PlayerSetPrefix+args.GameID))

	return policy.CommandResponse{
		Data:   GameWelcomeData{Id: args.GameID, NumPlayers: numPlayers, Data: gameDataSerialized},
		Digest: json.Marshal,
	}
}

func LeaveGame(header policy.RequestHeader, bodyFactories policy.RequestBodyFactories, isSecureConnection bool) policy.CommandResponse {
	err := bodyFactories.SigVerify(header.UserID, header.Sig)
	if err != nil {
		log.Printf("Unauthorized Attempt! Error: %v\n", err)
		return policy.UnSuccessfulResponse("Unauthorized!")
	}

	args := SelectGameArgs{}
	err = bodyFactories.ParseFactory(&args)
	if err != nil {
		log.Printf("Bad Argument! Error: %v\n", err)
		return policy.UnSuccessfulResponse("Bad Arguments!")
	}

	var doesGameExist bool
	var numPlayers uint16

	err = redis.MasterRedis.Do(radix.Cmd(&doesGameExist, "HEXISTS", GameHashSetName, args.GameID))
	if err != nil {
		return policy.RespWithError(err)
	}

	if !doesGameExist {
		return policy.UnSuccessfulResponse("Game Does Not Exist!")
	}

	err = redis.MasterRedis.Do(radix.Cmd(&numPlayers, "SREM", PlayerSetPrefix+args.GameID, "-1", header.UserID))
	if err != nil {
		return policy.RespWithError(err)
	} else if numPlayers <= 0 {
		event.SubmitGameForHealthCheck(args.GameID)
	}

	return policy.SuccessfulResponse()
}

func DeleteGame(header policy.RequestHeader, bodyFactories policy.RequestBodyFactories, isSecureConnection bool) policy.CommandResponse {
	err := bodyFactories.SigVerify(header.UserID, header.Sig)
	if err != nil {
		log.Printf("Unauthorized Attempt! Error: %v\n", err)
		return policy.UnSuccessfulResponse("Unauthorized!")
	}

	args := SelectGameArgs{}
	err = bodyFactories.ParseFactory(&args)
	if err != nil {
		log.Printf("Bad Argument! Error: %v\n", err)
		return policy.UnSuccessfulResponse("Bad Arguments!")
	}

	var success bool

	// TODO This should be done with Pipelining!!!
	err = redis.MasterRedis.Do(radix.Cmd(&success, "HDEL", GameHashSetName, args.GameID))
	if err != nil {
		return policy.RespWithError(err)
	} else if !success {
		return policy.RespWithError(err)
	}

	err = redis.MasterRedis.Do(radix.Cmd(&success, "HDEL", MetadataHashSetName, args.GameID))
	if err != nil {
		return policy.RespWithError(err)
	} else if !success {
		log.Println("Failed to Delete Metadata at: " + MetadataHashSetName + " <> " + args.GameID)
	}

	err = redis.MasterRedis.Do(radix.Cmd(&success, "HDEL", MetadataHashSetName, args.GameID))
	if err != nil {
		return policy.RespWithError(err)
	} else if !success {
		log.Println("Failed to Delete Metadata at: " + MetadataHashSetName + " <> " + args.GameID)
	}

	err = redis.MasterRedis.Do(radix.Cmd(&success, "SUNIONSTORE", PlayerSetPrefix+args.GameID, EmptyName))
	if err != nil {
		return policy.RespWithError(err)
	} else if !success {
		log.Println("Failed to Remove Players at: " + PlayerSetPrefix + args.GameID)
	}

	return policy.SuccessfulResponse()
}

func GetRoomHealth(gameID string) (time.Time, error) {
	var marshalledMetadata string

	err := redis.MasterRedis.Do(radix.Cmd(&marshalledMetadata, "HGET", MetadataHashSetName, gameID))
	if err != nil {
		return time.Now().UTC(), err
	} else if len(marshalledMetadata) <= 0 {
		return time.Now().UTC(), errors.New("Game Metadata does not seem to exists. GameID: " + gameID)
	}

	metadata, err := UnserializeMetadata(marshalledMetadata)
	if err != nil {
		return time.Now().UTC(), err
	}

	return time.Unix(metadata.LastUsed, 0), nil
}

func IsUserInGame(userID string, gameID string) (bool, error) {
	var isInGame int
	err := redis.MasterRedis.Do(radix.Cmd(&isInGame, "SISMEMBER", PlayerSetPrefix+gameID, userID))
	if err != nil {
		return false, err
	}

	return isInGame > 0, nil
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Utility Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Provides a 12 Character ID from any given 64 bit Integer
func StringIDFromNumbers(counter int64) string {
	res := make([]byte, 13)
	var seg int64 = 0
	const fullFiveBits = 16 | 8 | 4 | 2 | 1

	for i := 0; i < 13; i++ {
		seg = counter & fullFiveBits

		// seg = [0, 31]
		if seg <= 25 {
			res[i] = 'a' + byte(seg)
		} else {
			res[i] = '0' + byte(seg)
		}
	}

	return string(res)
}

func SerializeMetadata(metadata GameMetadata) string {
	// We could do our own serialization here, but JSON is fine for now.
	bytes, err := json.Marshal(metadata)
	if err != nil {
		log.Println("Unable to serialize data!")
		return ""
	}

	return string(bytes)
}

func UnserializeMetadata(bytes string) (GameMetadata, error) {
	// We could do our own deserialization here, but JSON is fine for now.
	var result GameMetadata

	err := json.Unmarshal([]byte(bytes), &result)
	if err != nil {
		return GameMetadata{}, err
	}

	return result, nil
}
