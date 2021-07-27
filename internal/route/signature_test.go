package route

import (
	"encoding/json"
	"testing"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/data"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/policy"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/startup"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/util"
)

const testUserNamePrefix = "DERPDERPSIG"

func TestSigVerify(t *testing.T) {
	cleanup := startup.InitServerStartupOnTaskList(
		[]startup.ServerTask{
			redis.StartDatabase,
			data.StartUsers,
		})
	defer cleanup()

	username := testUserNamePrefix + "VERIFY"
	password := "SomeP@ssword123"

	// Register User
	regBody := data.RegisterCommandBody{Username: username, Password: password}
	req, err := policy.RequestWithUserForTesting("", false, policy.CmdRegister, regBody)
	if err != nil {
		t.Errorf("Failure to create Request! Err: %v\n", err)
	}

	response := data.Register(req.Header, req.BodyFactories, req.IsSecureConnection)
	if response.ServerError != nil {
		t.Errorf("Failure to Register user! Err: %v\n", err)
	}

	// Login User
	loginBody := data.LoginCommandBody{Username: username, Password: password}
	req, err = policy.RequestWithUserForTesting("", false, policy.CmdLogin, loginBody)
	if err != nil {
		t.Errorf("Failure to create Request! Err: %v\n", err)
	}

	response = data.Login(req.Header, req.BodyFactories, req.IsSecureConnection)
	if response.ServerError != nil {
		t.Errorf("Failure to Login user! Err: %v\n", err)
	}

	token, err := util.Base64Decode(&response.Raw)
	if err != nil {
		t.Errorf("Base64 Did Not Decode Correctly! Err: %v\n", err)
	}
	t.Logf("Token Received: %X\n", token)
	counter := 0

	// Get User ID
	getUserBody := data.GetUserCommandBody{Username: username}
	req, err = policy.RequestWithUserForTesting("", false, policy.CmdGetUser, getUserBody)
	if err != nil {
		t.Errorf("Failure to create Request! Err: %v\n", err)
	}

	response = data.GetUser(req.Header, req.BodyFactories, req.IsSecureConnection)
	if response.ServerError != nil {
		t.Errorf("Failure to Get user! Err: %v\n", err)
	}

	var authResponse data.UserInfo
	bytes, err := response.Digest(response.Data)
	if err != nil {
		t.Errorf("Error Digesting Response! Err: %v\n", err)
	}
	err = json.Unmarshal(bytes, &authResponse)
	if err != nil {
		t.Errorf("Error Unmarshalling Response! Err: %v\n", err)
	}

	// Test Verification
	content := "derp1234!@#$"
	contentByte := []byte(content)
	signature := TestHelperGenSig(&token, content, counter)
	// Remember, each success increments counter!
	err = SigVerification(authResponse.AuthID, signature, &contentByte)
	if err != nil {
		t.Errorf("Error Verifying Signature! Err: %v\n", err)
	}

	counter += 1

	signature = TestHelperGenSig(&token, content, counter)
	err = SigVerification(authResponse.AuthID, signature, &contentByte)
	if err != nil {
		t.Errorf("Error Verifying Signature! Err: %v\n", err)
	}

	counter += 1

	signature = TestHelperGenSig(&token, content, counter)
	err = SigVerification(authResponse.AuthID, signature, &contentByte)
	if err != nil {
		t.Errorf("Error Verifying Signature! Err: %v\n", err)
	}

	counter += 1

	signature = TestHelperGenSig(&token, content, counter)
	err = SigVerification(authResponse.AuthID, signature, &contentByte)
	if err != nil {
		t.Errorf("Error Verifying Signature! Err: %v\n", err)
	}

	// Bad Signature Results in Error
	signature = TestHelperGenSig(&token, content, counter)
	err = SigVerification(authResponse.AuthID, signature, &contentByte)
	if err == nil {
		t.Errorf("No Error In Verifying Bad Signature!")
	}

	empty := []byte{}
	err = SigVerification(authResponse.AuthID, "", &empty)
	if err == nil {
		t.Errorf("No Error In Verifying Bad Signature!")
	}

	// Delete the user
	success, err := data.DeleteUser(username)
	if err != nil {
		t.Errorf("Failure to delete user! Err: %v\n", err)
	} else if !success {
		t.Errorf("User did not exist upon deletion!\n")
	}
}
