package main

import (
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
)

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Request Definitions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// third and fourth bytes of a request
// Establish which endpoint/action the request is for
type ClientCmd int64

// First four bytes of a request.
type RequestPrefix struct {
	IsEncoded bool
	IsJSON    bool
	Command   ClientCmd
}

// Command is synonymous to a request body
/*
 * This represents the typical params found in most
 * requests that require arugments
 */

type RequestHeader struct {
	// Who the Command is From
	UserID string

	// Request Signature for Authentication
	Sig string

	// The Byte Index in Data for the Start of Body
	bodyStart uint64
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Response Definitions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Abstracted Response Information from a Command
type CommandResponse struct {
	// Data to be Digested
	Data interface{}

	// Digesting method for Data Field
	Digest func(interface{}) ([]byte, error)

	// Raw Data to be written without Digest
	Raw []byte

	// Response with Data + Digest or use Raw Field
	UseRaw bool

	// Server Error to be Logged, rejecting request.
	ServerError error
}

// Data Interface for CommandResponse Example.
// Very typical for non-reading commands
type SuccessfulData struct {
	Successful bool
	Err        string
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Command Switch
////
///////////////////////////////////////////////////////////////////////////////////////////////////

/* Commands will be 2 byte codes */
const (
	cmdError ClientCmd = iota
	//                   //=====================
	//                     Empty Command
	//                   //=====================
	cmdEmpty //          //0000_0000_0000_0000
	//                   //=====================
	//                     TLS Commands
	//                   //=====================
	cmdRegister //       //0000_0000_0000_0001
	cmdNewToken //       //0000_0000_0000_0010
	cmdStartTLS //       //0000_0000_0000_1000
	//                   //=====================
	//                     Through Commands (To Third Party)
	//                   //=====================
	cmdAction  //        //0000_0000_0001_0000
	cmdObserve //        //0000_0000_0001_0001
	//                   //=====================
	//                     User Management Commands
	//                   //=====================
	cmdGetUserByUsername //      //0000_0001_0000_0000
	//                   //=====================
	//                     Game Management Commands
	//                   //=====================
	cmdGameCreate //     //0000_0010_0000_0000
	cmdGameJoin   //     //0000_0010_0000_0001
	cmdGameLeave  //     //0000_0010_0000_0010
	cmdGameDelete //     //0000_0010_0000_0011
	//                   //=====================
)

// This should never change during runtime!
var commandMap map[int64]ClientCmd = map[int64]ClientCmd{
	1<<0 + 0: cmdEmpty,
	1<<0 + 1: cmdRegister,
	1<<0 + 2: cmdNewToken,
	1<<0 + 8: cmdStartTLS,
	1<<4 + 0: cmdAction,
	1<<4 + 1: cmdObserve,
	1<<5 + 0: cmdGetUserByUsername,
	1<<9 + 0: cmdGameCreate,
	1<<9 + 1: cmdGameJoin,
	1<<9 + 2: cmdGameLeave,
	1<<9 + 3: cmdGameDelete,
}

func parseRequestPrefix(data []byte) (RequestPrefix, error) {
	isEncoded := data[0] != byte(0)

	// Capitalize Byte
	data[1] = data[1] & 0b1101_1111

	isJson := data[1] == byte('J')
	if !isJson && data[1] != byte('A') {
		return RequestPrefix{}, errors.New("Invalid Command")
	}

	var cmd int64 = int64(data[2])
	cmd = (cmd << 8) + int64(data[3])

	result, exists := commandMap[cmd]

	if exists == false {
		return RequestPrefix{}, errors.New("Invalid Command")
	}

	return RequestPrefix{isEncoded, isJson, result}, nil
}

func parseRequestHeader(prefix RequestPrefix, data []byte) (RequestHeader, error) {
	var header RequestHeader
	var err error

	if prefix.IsJSON {
		header, err = decodeJsonHeader(data)
		return header, err
	} else {
		header, err = decodeASN1Header(data)
		return header, err
	}
}

func switchOnCommand(prefix RequestPrefix, header RequestHeader, clientConn ClientConn, body []byte) ([]byte, error) {
	var res CommandResponse

	if needsSecurity(prefix.Command) && !clientConn.isSecured {
		return newErrorJson("Unsecure Connection!"), nil
	}

	switch prefix.Command {
	// Empty Commands
	case cmdEmpty:
		res = successfulResponse()
		break

	// TLS Commands
	case cmdRegister:
		res = register(body)
		break

	case cmdNewToken:
		res = login(body)
		break

	// cmdStartTLS is an exception to this switch statement. (It occurs in main.go)

	// Through Commands (To Third Party)
	case cmdAction:
		res = applyAction(prefix, header, body)
		break

	case cmdObserve:
		res = getGameData(prefix, header, body)
		break

	// User Management Commands
	case cmdGetUserByUsername:
		res = getUserByUsername(prefix, header, body)
		break

	// Game Management Commands
	case cmdGameCreate:
		res = createGame(prefix, header, body)
		break

	case cmdGameJoin:
		res = joinGame(prefix, header, body)
		break

	case cmdGameLeave:
		res = leaveGame(prefix, header, body)
		break

	default:
		return nil, errors.New("Command is Not Defined!")
	}

	if res.ServerError != nil {
		return nil, res.ServerError
	} else if res.UseRaw {
		return res.Raw, nil
	}

	return res.Digest(res.Data)

}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Utility Encoding Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////
func base64Decode(data []byte) ([]byte, error) {
	res := make([]byte, base64.StdEncoding.DecodedLen(len(data)))
	_, err := base64.StdEncoding.Decode(data, res)
	return res, err
}

func decodeJsonHeader(data []byte) (RequestHeader, error) {
	res := RequestHeader{}
	err := json.Unmarshal(data, &res)
	if err != nil {
		return res, err
	}

	var counter int
	length := len(data)
	for counter = 0; counter < length; counter += 1 {
		if data[counter] == byte('}') {
			break
		}
	}

	return res, nil
}

func decodeASN1Header(data []byte) (RequestHeader, error) {
	res := RequestHeader{}
	body, err := asn1.Unmarshal(data, &res)
	if err != nil {
		return res, err
	}

	res.bodyStart = uint64(len(data) - len(body))

	return res, nil
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Utility Response Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func respWithError(err error) CommandResponse {
	return CommandResponse{ServerError: err}
}

func unSuccessfulResponseError(err error) CommandResponse {
	return CommandResponse{
		Data:   SuccessfulData{false, err.Error()},
		Digest: json.Marshal,
	}
}

func unSuccessfulResponse(err string) CommandResponse {
	return CommandResponse{
		Data:   SuccessfulData{false, err},
		Digest: json.Marshal,
	}
}

func successfulResponse() CommandResponse {
	return CommandResponse{
		Data:   SuccessfulData{Successful: true},
		Digest: json.Marshal,
	}
}

func rawSuccessfulResponseBytes(msg []byte) CommandResponse {
	return CommandResponse{
		UseRaw: true,
		Raw:    msg,
	}
}

// msg should not be a constant string!
func rawSuccessfulResponse(msg string) CommandResponse {
	return CommandResponse{
		UseRaw: true,
		Raw:    []byte(msg),
	}
}

func rawUnsuccessfulResponse(err string) CommandResponse {
	return CommandResponse{
		UseRaw: true,
		Raw:    []byte(err),
	}
}
