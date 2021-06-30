package data

import (
	"encoding/json"
	"testing"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/policy"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/startup"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/zeromq"
	"github.com/mediocregopher/radix/v3"
)

const iterations int = 5000

func TestCreateDeleteRoom(t *testing.T) {
	cleanup := startup.InitServerStartupOnTaskList(
		[]startup.ServerTask{
			redis.StartDatabase,
			zeromq.StartZeroMqComms,
		})
	defer cleanup()

	// Set To Max Atomic Count for overflow check
	err := redis.MasterRedis.Do(radix.Cmd(nil, "SET", GameAtomicCounter, "9223372036854775806"))
	if err != nil {
		t.Errorf("Error Setting Game Atomic Counter to Max! %v\n", err)
	}

	// userIDs := []string{"-10", "-11", "-12", "-13", "-14", "-15", "-16", "-17", "-18"}
	userIDs := []string{"-10"}
	lenUserIDs := len(userIDs)
	var request policy.InternalUserRequest
	var response policy.CommandResponse

	deleteGamesForUsers(userIDs, t)

	// Create a Bunch
	t.Logf("Creating and Deleting a bunch of Games!")
	for i := 0; i < iterations; i++ {
		request, err = policy.RequestWithUserForTesting(
			userIDs[i%lenUserIDs],
			false,
			policy.CmdGameCreate,
			nil,
		)
		if err != nil {
			t.Fatalf("Error Creating Request Payload for creating Game!")
		}

		response = CreateGame(request.Header, request.BodyFactories, request.IsSecureConnection)
		if response.ServerError != nil {
			t.Fatalf("Got Error From Create Game Request! Err: %v\n", response.ServerError)
		}

		if (i+1)%lenUserIDs == 0 {
			deleteGamesForUsers(userIDs, t)
		}
	}

	deleteGamesForUsers(userIDs, t)
}

func TestCanCreateGame(t *testing.T) {
	cleanup := startup.InitServerStartupOnTaskList(
		[]startup.ServerTask{
			redis.StartDatabase,
			zeromq.StartZeroMqComms,
		})
	defer cleanup()

	userID := "-100"
	deleteGamesForUsers([]string{userID}, t)

	// Create Valid Game
	metadata, jsonResponse := createGameForUser(userID, t)
	if metadata.Id == "" || metadata.Owner == "" || metadata.CreatedAt == 0 || metadata.LastUsed == 0 {
		t.Errorf("Game Was Not Created!: %s\n", jsonResponse)
	}

	// Try Duplicate Game
	metadata, jsonResponse = createGameForUser(userID, t)
	if metadata.Id != "" || metadata.Owner != "" || metadata.CreatedAt != 0 || metadata.LastUsed != 0 {
		t.Errorf("Duplicate Game Was Successfully Created! Response: %s\n", jsonResponse)
	}

	deleteGamesForUsers([]string{userID}, t)
}

func TestLeaveJoinGame(t *testing.T) {
	cleanup := startup.InitServerStartupOnTaskList(
		[]startup.ServerTask{
			redis.StartDatabase,
			zeromq.StartZeroMqComms,
		})
	defer cleanup()

	userIDs := []string{"-200", "201", "202", "203", "204", "205", "206"}
	ownerID := userIDs[0]
	length := len(userIDs)
	deleteGamesForUsers([]string{ownerID}, t)

	// Create Valid Game
	metadata, jsonResponse := createGameForUser(ownerID, t)
	if metadata.Id == "" || metadata.Owner == "" || metadata.CreatedAt == 0 || metadata.LastUsed == 0 {
		t.Errorf("Game Was Not Created!: %s\n", jsonResponse)
	}

	// Join With Each member and verify params are correct
	var welcomeData GameWelcomeData
	for i := 1; i < length; i++ {
		welcomeData, jsonResponse = joinGameForUser(userIDs[i], metadata.Id, t)
		if welcomeData.Id != metadata.Id || welcomeData.NumPlayers != uint16(i+1) {
			t.Errorf("Error Welcome Data For Joining was unexpected! Player: %s\nData: %s\n", userIDs[i], jsonResponse)
		}
	}

	// Joining Non-Existant Game Results in empty data
	welcomeData, jsonResponse = joinGameForUser(userIDs[1], "derpderp", t)
	if welcomeData.Id != "" || welcomeData.NumPlayers != 0 || welcomeData.Data != "" {
		t.Errorf("Error Welcome Data For Joining Non-Existant Game was Non-empty! Player: %s\nData: %s\n", userIDs[1], jsonResponse)
	}

	// Joining The Same Game Results in Empty Data
	welcomeData, jsonResponse = joinGameForUser(userIDs[1], metadata.Id, t)
	if welcomeData.Id != metadata.Id || welcomeData.NumPlayers != uint16(length) {
		t.Errorf("Error Welcome Data For Joining was unexpected! Player: %s\nData: %s\n", userIDs[1], jsonResponse)
	}

	var success policy.SuccessfulData
	// Leave with 3 members
	for i := 1; i <= 3; i++ {
		success, jsonResponse = leaveGameForUser(userIDs[length-i], metadata.Id, t)
		if !success.Successful {
			t.Errorf("Error Success Data For Leaving was unexpected! Player: %s\nData: %s\n", userIDs[length-i], jsonResponse)
		}
	}

	// leaving a second time fails
	success, jsonResponse = leaveGameForUser(userIDs[length-1], metadata.Id, t)
	if success.Successful {
		t.Errorf("Error Success Data For Leaving was unexpected! Player: %s\nData: %s\n", userIDs[length-1], jsonResponse)
	}

	// Check Stats In Tables are right!
	var numPlayers int
	err := redis.MasterRedis.Do(radix.Cmd(&numPlayers, "SCARD", PlayerSetPrefix+metadata.Id))
	if err != nil {
		t.Errorf("Redis Error checking Cardinality of Player Set! Err: %v\n", err)
	} else if numPlayers != length-3 {
		t.Errorf("Number of Players is incorrect in Set! Number: %d Instead of %d!\n", numPlayers, length-3)
	}

	// remove the rest of the players
	for i := 0; i < length-3; i++ {
		success, jsonResponse = leaveGameForUser(userIDs[i], metadata.Id, t)
		if !success.Successful {
			t.Errorf("Error Success Data For Leaving was unexpected! Player: %s\nData: %s\n", userIDs[i], jsonResponse)
		}
	}

	// Check Stats In Tables are right!
	err = redis.MasterRedis.Do(radix.Cmd(&numPlayers, "SCARD", PlayerSetPrefix+metadata.Id))
	if err != nil {
		t.Errorf("Redis Error checking Cardinality of Player Set! Err: %v\n", err)
	} else if numPlayers != 0 {
		t.Errorf("Number of Players is incorrect in Set! Number: %d Instead of %d!\n", numPlayers, length-3)
	}

	deleteGamesForUsers([]string{ownerID}, t)
}

func deleteGamesForUsers(userIDs []string, t *testing.T) {
	var err error
	var request policy.InternalUserRequest
	var response policy.CommandResponse
	lenUserIDs := len(userIDs)

	for k := 0; k < lenUserIDs; k++ {
		request, err = policy.RequestWithUserForTesting(
			userIDs[k],
			false,
			policy.CmdGameDelete,
			nil,
		)
		if err != nil {
			t.Errorf("Error Creating Request Payload for creating Game!")
		}

		response = DeleteGame(request.Header, request.BodyFactories, request.IsSecureConnection)
		if response.ServerError != nil {
			t.Errorf("Got Error From Create Game Request! Err: %v\n", response.ServerError)
		}
	}
}

func createGameForUser(userID string, t *testing.T) (GameMetadata, []byte) {
	var response policy.CommandResponse
	var parsedResponse GameMetadata

	request, err := policy.RequestWithUserForTesting(userID, false, policy.CmdGameCreate, nil)
	if err != nil {
		t.Errorf("Error Creating Request Payload for creating Game!")
	}

	response = CreateGame(request.Header, request.BodyFactories, request.IsSecureConnection)
	if response.ServerError != nil {
		t.Errorf("Got Error From Create Game Request! Err: %v\n", response.ServerError)
	}

	jsonResponse, err := response.Digest(response.Data)
	if err != nil {
		t.Errorf("Error Digesting Response From Create Game! Err: %v\n", err)
	}

	err = json.Unmarshal(jsonResponse, &parsedResponse)
	if err != nil {
		t.Errorf("Error Unmarshalling Response From Create Game! Err: %v\n", err)
	}

	return parsedResponse, jsonResponse
}

func joinGameForUser(userID string, gameID string, t *testing.T) (GameWelcomeData, []byte) {
	gameArgs := SelectGameArgs{
		GameID: gameID,
	}

	request, err := policy.RequestWithUserForTesting(userID, false, policy.CmdGameJoin, gameArgs)
	if err != nil {
		t.Errorf("Error Creating Request Payload for creating Game!")
	}

	response := JoinGame(request.Header, request.BodyFactories, request.IsSecureConnection)
	jsonResponse, err := response.Digest(response.Data)
	if err != nil {
		t.Errorf("Error Digesting Response From Create Game! Err: %v\n", err)
	}

	var welcomeData GameWelcomeData
	err = json.Unmarshal(jsonResponse, &welcomeData)
	if err != nil {
		t.Errorf("Error Unmarshalling Response From Create Game! Err: %v\n", err)
	}

	return welcomeData, jsonResponse
}

func leaveGameForUser(userID string, gameID string, t *testing.T) (policy.SuccessfulData, []byte) {
	gameArgs := SelectGameArgs{
		GameID: gameID,
	}

	request, err := policy.RequestWithUserForTesting(userID, false, policy.CmdGameLeave, gameArgs)
	if err != nil {
		t.Errorf("Error Creating Request Payload for creating Game!")
	}

	response := LeaveGame(request.Header, request.BodyFactories, request.IsSecureConnection)
	jsonResponse, err := response.Digest(response.Data)
	if err != nil {
		t.Errorf("Error Digesting Response From Create Game! Err: %v\n", err)
	}

	var success policy.SuccessfulData
	err = json.Unmarshal(jsonResponse, &success)
	if err != nil {
		t.Errorf("Error Unmarshalling Response From Create Game! Err: %v\n", err)
	}

	return success, jsonResponse
}
