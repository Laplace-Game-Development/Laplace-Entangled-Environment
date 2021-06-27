package data

import (
	"encoding/json"
	"testing"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/policy"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/mediocregopher/radix/v3"
)

const iterations int = 5000

func TestCreateDeleteRoom(t *testing.T) {
	cleanup, err := redis.StartDatabase()
	if err != nil {
		t.Errorf("Error Starting Redis Connection! Err: %v\n", err)
	}
	defer cleanup()

	cleanup, err = StartRoomsSystem()
	if err != nil {
		t.Errorf("Error Starting Rooms Service! Err: %v\n", err)
	}
	defer cleanup()

	// Set To Max Atomic Count for overflow check
	redis.MasterRedis.Do(radix.Cmd(nil, "SET", GameAtomicCounter, "9223372036854775806"))

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

func TestCanCreateGame(t *testing.T) {
	cleanup, err := redis.StartDatabase()
	if err != nil {
		t.Errorf("Error Starting Redis Connection! Err: %v\n", err)
	}
	defer cleanup()

	cleanup, err = StartRoomsSystem()
	if err != nil {
		t.Errorf("Error Starting Rooms Service! Err: %v\n", err)
	}
	defer cleanup()

	userID := "-100"
	var response policy.CommandResponse

	deleteGamesForUsers([]string{userID}, t)

	request, err := policy.RequestWithUserForTesting(userID, false, policy.CmdGameCreate, nil)
	if err != nil {
		t.Errorf("Error Creating Request Payload for creating Game!")
	}

	// Create Valid Game
	response = CreateGame(request.Header, request.BodyFactories, request.IsSecureConnection)
	if response.ServerError != nil {
		t.Errorf("Got Error From Create Game Request! Err: %v\n", response.ServerError)
	}

	jsonResponse, err := response.Digest(response.Data)
	if err != nil {
		t.Errorf("Error Digesting Response From Create Game! Err: %v\n", err)
	}

	t.Logf("Got Response From Creating Game:\n%s\n", jsonResponse)

	var parsedResponse GameMetadata
	err = json.Unmarshal(jsonResponse, &parsedResponse)
	if err != nil {
		t.Errorf("Error Unmarshalling Response From Create Game! Err: %v\n", err)
	} else if parsedResponse.Id == "" || parsedResponse.Owner == "" || parsedResponse.CreatedAt == 0 || parsedResponse.LastUsed == 0 {
		t.Errorf("Game Was Not Created!: %s\n", jsonResponse)
	}

	// Try Duplicate Game
	response = CreateGame(request.Header, request.BodyFactories, request.IsSecureConnection)
	if response.ServerError != nil {
		t.Errorf("Got Error From Create Game Request! Err: %v\n", response.ServerError)
	}

	jsonResponse, err = response.Digest(response.Data)
	if err != nil {
		t.Errorf("Error Digesting Response From Create Game! Err: %v\n", err)
	}

	err = json.Unmarshal(jsonResponse, &parsedResponse)
	if err != nil {
		t.Errorf("Error Unmarshalling Response From Create Game! Err: %v\n", err)
	} else if parsedResponse.Id != "" || parsedResponse.Owner != "" || parsedResponse.CreatedAt != 0 || parsedResponse.LastUsed != 0 {
		t.Errorf("Duplicate Game Was Successfully Created! Response: %s\n", jsonResponse)
	}

	deleteGamesForUsers([]string{userID}, t)
}
