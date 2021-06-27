package policy

import (
	"testing"
)

// Actual Values
const testMessage string = "FooBar"

var testMessageBytes []byte = []byte(testMessage)
var unSuccessfulJSON []byte = []byte("{\"Successful\":false,\"Err\":\"FooBar\"}")
var successfulJSON []byte = []byte("{\"Successful\":true,\"Err\":\"\"}")

func parseResponse(cr CommandResponse) ([]byte, error) {
	if cr.UseRaw {
		return cr.Raw, nil
	}

	return cr.Digest(cr.Data)
}

func equalsBytes(left []byte, right []byte) bool {
	len1 := len(left)
	len2 := len(right)

	if len1 != len2 {
		return false
	}

	for i := 0; i < len1; i++ {
		if left[i] != right[i] {
			return false
		}
	}

	return true
}

func TestJsonUtils(t *testing.T) {
	var response CommandResponse

	response = UnSuccessfulResponse(testMessage)
	actual, err := parseResponse(response)
	if err != nil {
		t.Errorf("Error in Digesting Response! Err: %v\n", err)
	} else if !equalsBytes(actual, unSuccessfulJSON) {
		t.Errorf("Expected '%s' but Got '%s'\n", unSuccessfulJSON, actual)
	}

	response = SuccessfulResponse()
	actual, err = parseResponse(response)
	if err != nil {
		t.Errorf("Error in Digesting Response! Err: %v\n", err)
	} else if !equalsBytes(actual, successfulJSON) {
		t.Errorf("Expected '%s' but Got '%s'\n", successfulJSON, actual)
	}

	response = RawSuccessfulResponse(testMessage)
	actual, err = parseResponse(response)
	if err != nil {
		t.Errorf("Error in Digesting Response! Err: %v\n", err)
	} else if !equalsBytes(actual, testMessageBytes) {
		t.Errorf("Expected '%s' but Got '%s'\n", testMessageBytes, actual)
	}

	response = RawSuccessfulResponseBytes(&testMessageBytes)
	actual, err = parseResponse(response)
	if err != nil {
		t.Errorf("Error in Digesting Response! Err: %v\n", err)
	} else if !equalsBytes(actual, testMessageBytes) {
		t.Errorf("Expected '%s' but Got '%s'\n", testMessageBytes, actual)
	}

	response = RawUnsuccessfulResponse(testMessage)
	actual, err = parseResponse(response)
	if err != nil {
		t.Errorf("Error in Digesting Response! Err: %v\n", err)
	} else if !equalsBytes(actual, testMessageBytes) {
		t.Errorf("Expected '%s' but Got '%s'\n", testMessageBytes, actual)
	}
}
