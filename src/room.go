package main

import (
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/mediocregopher/radix/v3"
)

// Table / Datastructure Names
const gameListName string = "gameList"
const gameHashSetName string = "gameHash"
const metadataHashSetName string = "metadataHash"
const ownerHashSetName string = "ownerMapGame"
const playerSetPrefix string = "roster:"
const gameAtomicCounter string = "gameCountInteger"
const emptyName string = "empty"

func startRoomsSystem() (func(), error) {
	var dummyContainer int64 = 0

	err := masterRedis.Do(radix.Cmd(&dummyContainer, "SETNX", gameAtomicCounter, "0"))
	if err != nil {
		return nil, err
	}

	return roomsSystemCleanup, nil
}

func roomsSystemCleanup() {
	// If there is any cleanup that needs doing.
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Game Manipulation
////
///////////////////////////////////////////////////////////////////////////////////////////////////

type GameWelcomeData struct {
	id         string
	numPlayers uint16
	data       string
}

// For Static game details
type GameMetadata struct {
	id        string
	owner     string
	createdAt int64
	lastUsed  int64
}

func createGame(data []byte) CommandResponse {
	// Get Auth ID from data
	var authID string = "1"
	var atomicClockValue int64
	var success int
	var players int

	canCreateGame, err := canCreateGame(authID)

	if err != nil {
		return respWithError(err)
	} else if !canCreateGame {
		return CommandResponse{
			data:   GameMetadata{id: "", owner: "", createdAt: 0},
			digest: json.Marshal,
		}
	}
	//// We are good to create game

	// Increment atomic counters to get next index
	err = masterRedis.Do(radix.Cmd(&atomicClockValue, "INCR", gameAtomicCounter))
	if err != nil {
		return respWithError(err)
	}

	gameID := stringIDFromNumbers(atomicClockValue)
	metadata := GameMetadata{id: gameID, owner: authID, createdAt: time.Now().UTC().Unix(), lastUsed: time.Now().UTC().Unix()}
	serializedMetadata := serializeMetadata(metadata)

	// TODO Add Pipelining
	err = masterRedis.Do(radix.Cmd(&success, "HSETNX", gameHashSetName, gameID, "{}"))
	if err != nil || success == 0 {
		return respWithError(err)
	}

	err = masterRedis.Do(radix.Cmd(&success, "HSETNX", metadataHashSetName, gameID, serializedMetadata))
	if err != nil || success == 0 {
		return respWithError(err)
	}

	err = masterRedis.Do(radix.Cmd(&players, "SADD", playerSetPrefix+gameID, authID))
	if err != nil || players != 1 {
		return respWithError(err)
	}

	err = masterRedis.Do(radix.Cmd(nil, "RPUSH", gameListName, gameID))
	if err != nil {
		return respWithError(err)
	}

	// We can also do other things here like push metadata or channel numbers under different keys/tables.
	// As long as the gameID is an identifier.

	return CommandResponse{
		data:   metadata,
		digest: json.Marshal,
	}
}

func canCreateGame(authID string) (bool, error) {
	var success int
	var games int

	// Find the number of games
	// We could also use metadataHashSetName Here
	err := masterRedis.Do(radix.Cmd(&games, "HLEN", gameHashSetName))
	if err != nil {
		return false, err
	}

	// Throttle Number of Games
	// Cannot create too many games
	if games >= numberOfGames && throttleGames {
		return false, nil
	}

	// Check if user already has a game
	err = masterRedis.Do(radix.Cmd(&success, "HEXISTS", ownerHashSetName, authID))
	if err != nil {
		// TODO Redirect to Get Game Info
		return false, err
	}

	return success != 0, nil
}

// Ah yes so Go has the "any" type just like typescript
func joinGame(data []byte) CommandResponse {
	// Get Auth ID from data
	var authID string = "1"
	var gameID string = "2"
	var gameDataSerialized string
	var success int
	var numPlayers uint16

	err := masterRedis.Do(radix.Cmd(&gameDataSerialized, "HGET", gameHashSetName, gameID))
	if err != nil {
		return respWithError(err)
	} else if gameDataSerialized == "(nil)" {
		return CommandResponse{
			data:   GameWelcomeData{id: "", numPlayers: 0, data: ""},
			digest: json.Marshal,
		}
	}

	err = masterRedis.Do(radix.Cmd(&success, "SADD", playerSetPrefix+gameID, authID))
	if err != nil {
		return respWithError(err)
	} else if success < 1 {
		log.Printf("User Tried to Add Themselves More Than Once: " + authID)
	}

	err = masterRedis.Do(radix.Cmd(&numPlayers, "SCARD", playerSetPrefix+gameID))

	return CommandResponse{
		data:   GameWelcomeData{id: gameID, numPlayers: numPlayers, data: gameDataSerialized},
		digest: json.Marshal,
	}
}

func leaveGame(data []byte) CommandResponse {
	// Get Auth ID from data
	var authID string = "1"
	var gameID string = "2"
	var doesGameExist bool
	var numPlayers uint16

	err := masterRedis.Do(radix.Cmd(&doesGameExist, "HEXISTS", gameHashSetName, gameID))
	if err != nil {
		return respWithError(err)
	}

	if !doesGameExist {
		return unSuccessfulResponse("Game Does Not Exist!")
	}

	err = masterRedis.Do(radix.Cmd(&numPlayers, "SREM", playerSetPrefix+gameID, "-1", authID))
	if err != nil {
		return respWithError(err)
	} else if numPlayers <= 0 {
		// TODO replace 0 with superuser ID
		submitGameForHealthCheck("0", gameID)
	}

	return successfulResponse()
}

func deleteGame(data []byte) CommandResponse {
	// TODO Auth ID should be checked for ownership of room or "0" for super user
	// Get Auth ID from data
	var gameID string = "2"
	var success bool

	// TODO This should be done with Pipelining!!!
	err := masterRedis.Do(radix.Cmd(&success, "HDEL", gameHashSetName, gameID))
	if err != nil {
		return respWithError(err)
	} else if !success {
		return respWithError(err)
	}

	err = masterRedis.Do(radix.Cmd(&success, "HDEL", metadataHashSetName, gameID))
	if err != nil {
		return respWithError(err)
	} else if !success {
		log.Println("Failed to Delete Metadata at: " + metadataHashSetName + " <> " + gameID)
	}

	err = masterRedis.Do(radix.Cmd(&success, "HDEL", metadataHashSetName, gameID))
	if err != nil {
		return respWithError(err)
	} else if !success {
		log.Println("Failed to Delete Metadata at: " + metadataHashSetName + " <> " + gameID)
	}

	err = masterRedis.Do(radix.Cmd(&success, "SUNIONSTORE", playerSetPrefix+gameID, emptyName))
	if err != nil {
		return respWithError(err)
	} else if !success {
		log.Println("Failed to Remove Players at: " + playerSetPrefix + gameID)
	}

	return successfulResponse()
}

func getRoomHealth(gameID string) (time.Time, error) {
	var marshalledMetadata string

	err := masterRedis.Do(radix.Cmd(&marshalledMetadata, "HGET", metadataHashSetName, gameID))
	if err != nil {
		return time.Now(), err
	} else if len(marshalledMetadata) <= 0 {
		return time.Now(), errors.New("Game Metadata does not seem to exists. GameID: " + gameID)
	}

	metadata, err := unserializeMetadata(marshalledMetadata)
	if err != nil {
		return time.Now(), err
	}

	return time.Unix(metadata.lastUsed, 0), nil
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Utility Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Provides a 12 Character ID from any given 64 bit Integer
func stringIDFromNumbers(counter int64) string {
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

func serializeMetadata(metadata GameMetadata) string {
	// We could do our own serialization here, but JSON is fine for now.
	bytes, err := json.Marshal(metadata)
	if err != nil {
		log.Println("Unable to serialize data!")
		return ""
	}

	return string(bytes)
}

func unserializeMetadata(bytes string) (GameMetadata, error) {
	// We could do our own deserialization here, but JSON is fine for now.
	var result GameMetadata

	err := json.Unmarshal([]byte(bytes), &result)
	if err != nil {
		return GameMetadata{}, err
	}

	return result, nil
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Game Actions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func applyAction(data []byte) CommandResponse {
	return unSuccessfulResponse("Command is Not Implemented!")
}

func getGameData(data []byte) CommandResponse {
	return unSuccessfulResponse("Command is Not Implemented!")
}
