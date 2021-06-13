package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/mediocregopher/radix/v3"
	"laplace-entangled-env.com/internal/event"
	"laplace-entangled-env.com/internal/policy"
	"laplace-entangled-env.com/internal/redis"
)

//// Configurables

// Table / Datastructure Names

// Redis Key For Game List
const GameListName string = "gameList"

// Redis Key for Game Set
const GameHashSetName string = "gameHash"

// Redis Key for Owner Set
const OwnerHashSetName string = "ownerMapGame"

// Redis Key Prefix for Player Roster Sets
const PlayerSetPrefix string = "roster:"

// Redis Key for Game ID Counter
const GameAtomicCounter string = "gameCountInteger"

// Redis Key for Empty Set
// useful for ease of group deletions
const EmptyName string = "empty"

// Metadata Table

// Redis Key Prefix for Game Metadata Hashmaps
const MetadataSetPrefix string = "metadataHash:"

// Redis Field/Key for Game Metadata Owner
const MetadataSetOwner string = "owner"

// Redis Field/Key for Game Metadata Creation DateTime
//    (number of milliseconds since epoch)
const MetadataSetCreatedAt string = "createdAt"

// Redis Field/Key for Game Metadata Last Used DateTime
//    (number of milliseconds since epoch)
const MetadataSetLastUsed string = "lastUsed"

// Game Throttling

// The Maximum Number of Games If
// ThrottleGames is true
const NumberOfGames = 20

// Whether to Throttle to the Maximum
// Number of Games or Not
const ThrottleGames = false

// ServerTask Startup Function for Game Rooms. Takes care of initialization.
// Sets Atomic Counter for GameIDs. Error is returned if the Database
// can't be reached.
func StartRoomsSystem() (func(), error) {
	err := redis.MasterRedis.Do(radix.Cmd(nil, "SETNX", GameAtomicCounter, "0"))
	if err != nil {
		return nil, err
	}

	return cleanUpRoomSystem, nil
}

// CleanUp Function returned by Startup function. Doesn't do anything, but here
// for consistency.
func cleanUpRoomSystem() {
	log.Println("Cleaning Up Room Logic")
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Game Manipulation
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// JSON Fields for the Join Game Command
type GameWelcomeData struct {
	Id         string
	NumPlayers uint16 `json:",string"`
	Data       string
}

// JSON Fields for the Create Game Command
// For Static game details
type GameMetadata struct {
	Id        string
	Owner     string
	CreatedAt int64 `json:",string"`
	LastUsed  int64 `json:",string"`
}

// Unmarshal Structure for Joining/Finding Games
type SelectGameArgs struct {
	GameID string
}

// Create Game Endpoint to add a Game and new Game Data to the
// the database. Each player can only own/create one game. They
// may delete and create games freely.
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

	// TODO Add Pipelining
	err = redis.MasterRedis.Do(radix.Cmd(&success, "HSETNX", GameHashSetName, gameID, "{}"))
	if err != nil || success == 0 {
		return policy.RespWithError(err)
	}

	err = SetGameMetadata(metadata)
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

// Join Game Endpoint adds the player to the roster of an existing
// game. This means they can "applyActions" to the game (see game.go)
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

// Leave Game Endpoint removes the player from the roster of an existing
// game. This means they can no longer "applyActions" to the game (see game.go)
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

// An Owner may delete their game at any time. This means the game
// metadata and state will be removed from the database.
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

	err = redis.MasterRedis.Do(radix.Cmd(&success, "DEL", MetadataSetPrefix, args.GameID))
	if err != nil {
		return policy.RespWithError(err)
	} else if !success {
		log.Println("Failed to Delete Metadata at:  " + MetadataSetPrefix + args.GameID)
	}

	err = redis.MasterRedis.Do(radix.Cmd(&success, "SUNIONSTORE", PlayerSetPrefix+args.GameID, EmptyName))
	if err != nil {
		return policy.RespWithError(err)
	} else if !success {
		log.Println("Failed to Remove Players at:  " + PlayerSetPrefix + args.GameID)
	}

	return policy.SuccessfulResponse()
}

// Returns the time in which the last recorded action was taken
// for a game. This loads the value from the Redis
// database.
//
// gameID :: Unique Identifier for game in string form
//
// returns -> time.Time :: time last action was made
//         -> error     :: non-nil if unable to read game metadata
func GetRoomHealth(gameID string) (time.Time, error) {
	var lastUpdate string

	err := redis.MasterRedis.Do(radix.Cmd(&lastUpdate, "HGET", MetadataSetPrefix+gameID, MetadataSetLastUsed))
	if err != nil {
		return time.Now().UTC(), err
	} else if len(lastUpdate) <= 0 {
		return time.Now().UTC(), errors.New("Game Metadata does not seem to exists. GameID: " + gameID)
	}

	milli, err := strconv.ParseInt(lastUpdate, 10, 64)

	return time.Unix(milli, 0), nil
}

// Returns whether a user is in a game's roster (see JoinGame and leaveGame).
//
// userID :: Unique Identifier for a user
// gameID :: Unique Identifier for game in string form
//
// returns -> bool  :: true if the user is in the roster, false otherwise
//         -> error :: non-nil if unable to read game metadata
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

// Provides a 12 character string ID from any given 64 bit Integer
// The ID uses the characters 0-9 and a-z
// This is used for GameIDs
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

// Utility Function for changing Game Metadata with Redis
//
// metadata :: New Metadata Value
//    (overwrites the id at metadata.id)
func SetGameMetadata(metadata GameMetadata) error {
	return redis.MasterRedis.Do(radix.Cmd(nil, "HSET", MetadataSetPrefix+metadata.Id,
		MetadataSetOwner, metadata.Owner,
		MetadataSetCreatedAt, fmt.Sprintf("%d", metadata.CreatedAt),
		MetadataSetLastUsed, fmt.Sprintf("%d", metadata.LastUsed)))
}

// Utility Function for selecting the game Metadata from redis
//
// gameID :: string unique identifier for game.
func GetGameMetadata(gameID string) (GameMetadata, error) {
	fields := make([]string, 3)

	err := redis.MasterRedis.Do(radix.Cmd(&fields, "HMGET", MetadataSetPrefix+gameID,
		MetadataSetOwner,
		MetadataSetCreatedAt,
		MetadataSetLastUsed))

	data := GameMetadata{}

	if err != nil {
		return data, err
	}

	data.Id = gameID
	data.Owner = fields[0]
	data.CreatedAt, err = strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return data, err
	}

	data.LastUsed, err = strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return data, err
	}

	return data, nil
}
