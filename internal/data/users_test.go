package data

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/policy"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/startup"
	"github.com/mediocregopher/radix/v3"
)

const testUserNamePrefix = "DERPDERP"

func TestStartUsers(t *testing.T) {
	cleanup := startup.InitServerStartupOnTaskList(
		[]startup.ServerTask{
			redis.StartDatabase,
			StartUsers,
		})
	defer cleanup()

	if passHashSalt == "" {
		t.Errorf("Password Salt is Empty!\n")
	}

	var passHashSaltTemp string
	err := redis.MainRedis.Do(radix.Cmd(&passHashSaltTemp, "GET", passHashSaltKey))
	if err != nil {
		t.Errorf("Error Reading Redis Salt Value! Err: %v\n", err)
	}

	if passHashSalt != passHashSaltTemp {
		t.Errorf("Expected Value %s is not actual value %s\n", passHashSaltTemp, passHashSalt)
	}
}

func TestRegister(t *testing.T) {
	cleanup := startup.InitServerStartupOnTaskList(
		[]startup.ServerTask{
			redis.StartDatabase,
			StartUsers,
		})
	defer cleanup()

	validUsername := testUserNamePrefix + "TFLEX"
	DeleteUser(validUsername)

	t.Run("Create Standard User", func(t *testing.T) {
		createUserSuccess(t, validUsername, "DERPDERP𔿽")
	})
	t.Run("Create User With Bad Password 0", func(t *testing.T) {
		createUserError(t, testUserNamePrefix, "DERPDERP123")
	})
	t.Run("Create User With Bad Password 1", func(t *testing.T) {
		createUserError(t, testUserNamePrefix, "derpDerp")
	})
	t.Run("Create User With Bad Password 2", func(t *testing.T) {
		createUserError(t, testUserNamePrefix, "DERP1234!@#$")
	})
	t.Run("Create User That already exists", func(t *testing.T) {
		createUserError(t, validUsername, "DERPderp1234!@#$")
	})

	success, err := DeleteUser(validUsername)
	if err != nil {
		t.Errorf("Failure to delete user! Err: %v\n", err)
	} else if !success {
		t.Errorf("User did not exist upon deletion!\n")
	}
}

func TestLoginUser(t *testing.T) {
	cleanup := startup.InitServerStartupOnTaskList(
		[]startup.ServerTask{
			redis.StartDatabase,
			StartUsers,
		})
	defer cleanup()

	validUsername := testUserNamePrefix + "LOGIN"
	password := "SomeP@ssword123"

	// Register User
	t.Run("Create Login User", func(t *testing.T) {
		createUserSuccess(t, validUsername, password)
	})

	// Login User
	t.Run("Login User Correctly 0", func(t *testing.T) {
		loginUserSuccess(t, validUsername, password)
	})

	t.Run("Login User Correctly 1", func(t *testing.T) {
		loginUserSuccess(t, validUsername, password)
	})

	t.Run("Login User Correctly 2", func(t *testing.T) {
		loginUserSuccess(t, validUsername, password)
	})

	t.Run("Login User Incorrectly 0", func(t *testing.T) {
		loginUserError(t, validUsername, "NOTtheRightPassword")
	})

	t.Run("Login User Incorrectly 1", func(t *testing.T) {
		loginUserError(t, validUsername, "")
	})

	t.Run("Login User Incorrectly 2", func(t *testing.T) {
		loginUserError(t, "", password)
	})

	// Delete the user
	success, err := DeleteUser(validUsername)
	if err != nil {
		t.Errorf("Failure to delete user! Err: %v\n", err)
	} else if !success {
		t.Errorf("User did not exist upon deletion!\n")
	}
}

func TestGetUser(t *testing.T) {
	cleanup := startup.InitServerStartupOnTaskList(
		[]startup.ServerTask{
			redis.StartDatabase,
			StartUsers,
		})
	defer cleanup()

	validUsername := testUserNamePrefix + "GETUSER"
	password := "SomeP@ssword123"

	// Register User
	t.Run("Create Login User", func(t *testing.T) {
		createUserSuccess(t, validUsername, password)
	})

	// Get Auth ID
	authIDHold := getUserTestHelper(t, validUsername)
	randomValue := "DERPITYDERP"

	err := redis.MainRedis.Do(radix.Cmd(nil, "HSET", UserAuthIDTable, validUsername, randomValue))
	if err != nil {
		t.Errorf("Error in Setting Foobar Value in Database for GetUser! Err: %v\n", err)
	}

	actualAuthId := getUserTestHelper(t, validUsername)

	if actualAuthId != randomValue {
		t.Fatalf("Expected AuthId is not AuthID! Expected: %s\tActual: %s\n", randomValue, actualAuthId)
	}

	err = redis.MainRedis.Do(radix.Cmd(nil, "HSET", UserAuthIDTable, validUsername, authIDHold))
	if err != nil {
		t.Errorf("Error in Setting Foobar Value in Database for GetUser! Err: %v\n", err)
	}

	// Delete the user
	success, err := DeleteUser(validUsername)
	if err != nil {
		t.Errorf("Failure to delete user! Err: %v\n", err)
	} else if !success {
		t.Errorf("User did not exist upon deletion!\n")
	}
}

func createUserSuccess(t *testing.T, username string, password string) {
	body := RegisterCommandBody{Username: username, Password: password}
	req, err := policy.RequestWithUserForTesting("", false, policy.CmdRegister, body)
	if err != nil {
		t.Errorf("Failure to create Request! Err: %v\n", err)
	}

	response := Register(req.Header, req.BodyFactories, req.IsSecureConnection)
	if response.ServerError != nil {
		t.Errorf("Failure to register user! Err: %v\n", err)
	}

	if response.Raw == nil {
		t.Errorf("Creating User was not successful!\n")
	} else if string(response.Raw) != username {
		t.Errorf("Creating User was not successful! Response: %s\n", response.Raw)
	}

	// Check User Exists in DB
	var expectedID int
	err = redis.MainRedis.Do(radix.Cmd(&expectedID, "GET", AuthIDAtomicCounter))
	if err != nil {
		t.Errorf("Failure to checking user in DB! Err: %v\n", err)
	}

	var actualID int
	err = redis.MainRedis.Do(radix.Cmd(&actualID, "HGET", UserAuthIDTable, username))
	if err != nil {
		t.Errorf("Failure to checking user in DB! Err: %v\n", err)
	} else if expectedID != actualID {
		t.Errorf("ExpectedID %d, does not equal actual ID %d!\n", expectedID, actualID)
	}

	var success int
	err = redis.MainRedis.Do(radix.Cmd(&success, "EXISTS", fmt.Sprintf(AuthIDSetPrefix+"%d", actualID)))
	if err != nil {
		t.Errorf("Failure to checking user in DB! Err: %v\n", err)
	} else if success == 0 {
		t.Errorf("User does not exist at %s!\n", fmt.Sprintf(AuthIDSetPrefix+"%d", actualID))
	}
}

func createUserError(t *testing.T, username string, password string) {
	body := RegisterCommandBody{Username: username, Password: password}
	req, err := policy.RequestWithUserForTesting("", false, policy.CmdRegister, body)
	if err != nil {
		t.Errorf("Failure to create Request! Err: %v\n", err)
	}

	response := Register(req.Header, req.BodyFactories, req.IsSecureConnection)
	if response.ServerError != nil {
		t.Errorf("Failure to register user! Err: %v\n", err)
	}
	if response.Raw == nil {
		t.Errorf("Did not get a response when registering user!\n")
	} else if string(response.Raw) == username {
		t.Errorf("Creating User was successful!\n")
	}
}

func loginUserSuccess(t *testing.T, username string, password string) {
	body := LoginCommandBody{Username: username, Password: password}
	req, err := policy.RequestWithUserForTesting("", false, policy.CmdLogin, body)
	if err != nil {
		t.Errorf("Failure to create Request! Err: %v\n", err)
	}

	response := Login(req.Header, req.BodyFactories, req.IsSecureConnection)
	if response.ServerError != nil {
		t.Errorf("Failure to Login user! Err: %v\n", err)
	}

	if response.Raw == nil {
		t.Errorf("Did not get a response when logging in user!\n")
	}

	token := string(response.Raw)

	if token == "Unsecure Connection!" || token == "Illegal Input!" {
		t.Errorf("Logging in User Failed! Err: %s\n", token)
	}
}

func loginUserError(t *testing.T, username string, password string) {
	body := LoginCommandBody{Username: username, Password: password}
	req, err := policy.RequestWithUserForTesting("", false, policy.CmdLogin, body)
	if err != nil {
		t.Errorf("Failure to create Request! Err: %v\n", err)
	}

	response := Login(req.Header, req.BodyFactories, req.IsSecureConnection)
	if response.ServerError != nil {
		t.Errorf("Failure to Login user! Err: %v\n", err)
	}

	if response.Raw != nil {
		return
	}

	token := string(response.Raw)

	if token != "Unsecure Connection!" && token != "Illegal Input" {
		t.Errorf("Logging in User Failed! Err: %s\n", token)
	}
}

func getUserTestHelper(t *testing.T, username string) string {
	body := GetUserCommandBody{Username: username}
	req, err := policy.RequestWithUserForTesting("", false, policy.CmdGetUser, body)
	if err != nil {
		t.Errorf("Failure to create Request! Err: %v\n", err)
	}

	response := GetUser(req.Header, req.BodyFactories, req.IsSecureConnection)
	if response.ServerError != nil {
		t.Errorf("Failure to get user! Err: %v\n", err)
	}

	var authResponse UserInfo
	bytes, err := response.Digest(response.Data)
	json.Unmarshal(bytes, &authResponse)
	if authResponse.AuthID == "" {
		t.Fatalf("AuthID/UserID is empty!\n")
	}

	return authResponse.AuthID
}
