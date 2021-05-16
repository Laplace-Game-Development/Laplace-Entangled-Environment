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

func createGame(header RequestHeader, bodyFactories RequestBodyFactories, isSecureConnection bool) CommandResponse {
	err := bodyFactories.sigVerify(header.UserID, header.Sig)
	if err != nil {
		log.Printf("Unauthorized Attempt! Error: %v\n", err)
		return unSuccessfulResponse("Unauthorized!")
	}

	var atomicClockValue int64
	var success int
	var players int

	canCreateGame, err := canCreateGame(header.UserID)

	if err != nil {
		return respWithError(err)
	} else if !canCreateGame {
		return CommandResponse{
			Data:   GameMetadata{Id: "", Owner: "", CreatedAt: 0},
			Digest: json.Marshal,
		}
	}
	//// We are good to create game

	// Increment atomic counters to get next index
	err = masterRedis.Do(radix.Cmd(&atomicClockValue, "INCR", gameAtomicCounter))
	if err != nil {
		return respWithError(err)
	}

	gameID := stringIDFromNumbers(atomicClockValue)
	metadata := GameMetadata{Id: gameID, Owner: header.UserID, CreatedAt: time.Now().UTC().Unix(), LastUsed: time.Now().UTC().Unix()}
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

	err = masterRedis.Do(radix.Cmd(&players, "SADD", playerSetPrefix+gameID, header.UserID))
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
		Data:   metadata,
		Digest: json.Marshal,
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

func joinGame(header RequestHeader, bodyFactories RequestBodyFactories, isSecureConnection bool) CommandResponse {
	err := bodyFactories.sigVerify(header.UserID, header.Sig)
	if err != nil {
		log.Printf("Unauthorized Attempt! Error: %v\n", err)
		return unSuccessfulResponse("Unauthorized!")
	}

	args := SelectGameArgs{}
	err = bodyFactories.parseFactory(&args)
	if err != nil {
		log.Printf("Bad Argument! Error: %v\n", err)
		return unSuccessfulResponse("Bad Arguments!")
	}

	var gameDataSerialized string
	var success int
	var numPlayers uint16

	err = masterRedis.Do(radix.Cmd(&gameDataSerialized, "HGET", gameHashSetName, args.GameID))
	if err != nil {
		return respWithError(err)
	} else if gameDataSerialized == "" {
		return CommandResponse{
			Data:   GameWelcomeData{Id: "", NumPlayers: 0, Data: ""},
			Digest: json.Marshal,
		}
	}

	err = masterRedis.Do(radix.Cmd(&success, "SADD", playerSetPrefix+args.GameID, header.UserID))
	if err != nil {
		return respWithError(err)
	} else if success < 1 {
		log.Printf("User Tried to Add Themselves More Than Once: " + args.GameID)
	}

	err = masterRedis.Do(radix.Cmd(&numPlayers, "SCARD", playerSetPrefix+args.GameID))

	return CommandResponse{
		Data:   GameWelcomeData{Id: args.GameID, NumPlayers: numPlayers, Data: gameDataSerialized},
		Digest: json.Marshal,
	}
}

func leaveGame(header RequestHeader, bodyFactories RequestBodyFactories, isSecureConnection bool) CommandResponse {
	err := bodyFactories.sigVerify(header.UserID, header.Sig)
	if err != nil {
		log.Printf("Unauthorized Attempt! Error: %v\n", err)
		return unSuccessfulResponse("Unauthorized!")
	}

	args := SelectGameArgs{}
	err = bodyFactories.parseFactory(&args)
	if err != nil {
		log.Printf("Bad Argument! Error: %v\n", err)
		return unSuccessfulResponse("Bad Arguments!")
	}

	var doesGameExist bool
	var numPlayers uint16

	err = masterRedis.Do(radix.Cmd(&doesGameExist, "HEXISTS", gameHashSetName, args.GameID))
	if err != nil {
		return respWithError(err)
	}

	if !doesGameExist {
		return unSuccessfulResponse("Game Does Not Exist!")
	}

	err = masterRedis.Do(radix.Cmd(&numPlayers, "SREM", playerSetPrefix+args.GameID, "-1", header.UserID))
	if err != nil {
		return respWithError(err)
	} else if numPlayers <= 0 {
		submitGameForHealthCheck(args.GameID)
	}

	return successfulResponse()
}

func deleteGame(header RequestHeader, bodyFactories RequestBodyFactories, isSecureConnection bool) CommandResponse {
	err := bodyFactories.sigVerify(header.UserID, header.Sig)
	if err != nil {
		log.Printf("Unauthorized Attempt! Error: %v\n", err)
		return unSuccessfulResponse("Unauthorized!")
	}

	args := SelectGameArgs{}
	err = bodyFactories.parseFactory(&args)
	if err != nil {
		log.Printf("Bad Argument! Error: %v\n", err)
		return unSuccessfulResponse("Bad Arguments!")
	}

	var success bool

	// TODO This should be done with Pipelining!!!
	err = masterRedis.Do(radix.Cmd(&success, "HDEL", gameHashSetName, args.GameID))
	if err != nil {
		return respWithError(err)
	} else if !success {
		return respWithError(err)
	}

	err = masterRedis.Do(radix.Cmd(&success, "HDEL", metadataHashSetName, args.GameID))
	if err != nil {
		return respWithError(err)
	} else if !success {
		log.Println("Failed to Delete Metadata at: " + metadataHashSetName + " <> " + args.GameID)
	}

	err = masterRedis.Do(radix.Cmd(&success, "HDEL", metadataHashSetName, args.GameID))
	if err != nil {
		return respWithError(err)
	} else if !success {
		log.Println("Failed to Delete Metadata at: " + metadataHashSetName + " <> " + args.GameID)
	}

	err = masterRedis.Do(radix.Cmd(&success, "SUNIONSTORE", playerSetPrefix+args.GameID, emptyName))
	if err != nil {
		return respWithError(err)
	} else if !success {
		log.Println("Failed to Remove Players at: " + playerSetPrefix + args.GameID)
	}

	return successfulResponse()
}

func getRoomHealth(gameID string) (time.Time, error) {
	var marshalledMetadata string

	err := masterRedis.Do(radix.Cmd(&marshalledMetadata, "HGET", metadataHashSetName, gameID))
	if err != nil {
		return time.Now().UTC(), err
	} else if len(marshalledMetadata) <= 0 {
		return time.Now().UTC(), errors.New("Game Metadata does not seem to exists. GameID: " + gameID)
	}

	metadata, err := unserializeMetadata(marshalledMetadata)
	if err != nil {
		return time.Now().UTC(), err
	}

	return time.Unix(metadata.LastUsed, 0), nil
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
