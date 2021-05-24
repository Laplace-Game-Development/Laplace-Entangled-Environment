package main

import (
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
)

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Request Definitions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

//// Incomming Request Definitions

// 2nd and 3rd bytes of a TCP Request
// Establish which endpoint/action the request is for
type ClientCmd int64

// JSON or ASN1 Attachment for Authentication and Regulation
type RequestAttachment struct {
	// Who the Command is From
	UserID string

	// Request Signature for Authentication
	Sig string
}

//// Private Request Definitions For Parsing
type RequestHeader struct {
	// Command To Be Handled (i.e. Sent in TCP Prefix)
	Command ClientCmd

	// Who the Command is From
	UserID string

	// Request Signature for Authenticated Requests
	Sig string
}

type RequestBodyFactories struct {
	// Interface Parameter should be a pointer
	parseFactory func(ptr interface{}) error

	sigVerify func(userID string, userSig string) error
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
	cmdLogin    //       //0000_0000_0000_0010
	//                   //=====================
	//                     Through Commands (To Third Party)
	//                   //=====================
	cmdAction  //        //0000_0000_0001_0000
	cmdObserve //        //0000_0000_0001_0001
	//                   //=====================
	//                     User Management Commands
	//                   //=====================
	cmdGetUser //        //0000_0001_0000_0000
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
	1<<0 + 2: cmdLogin,
	1<<4 + 0: cmdAction,
	1<<4 + 1: cmdObserve,
	1<<5 + 0: cmdGetUser,
	1<<9 + 0: cmdGameCreate,
	1<<9 + 1: cmdGameJoin,
	1<<9 + 2: cmdGameLeave,
	1<<9 + 3: cmdGameDelete,
}

//// Functions!

// 1. Parse Command and add to Header
func parseCommand(mostSignificant byte, leastSignificant byte) (ClientCmd, error) {
	var cmd int64 = int64(mostSignificant)
	cmd = (cmd << 8) + int64(leastSignificant)

	result, exists := commandMap[cmd]

	if exists == false {
		return 0, errors.New("Invalid Command")
	}

	return result, nil
}

// 2. Parse Request Attachment and add to Header
func parseRequestAttachment(isJSON bool, data *[]byte) (RequestAttachment, int, error) {
	if isJSON {
		return decodeJsonAttachment(data)
	}

	return decodeASN1Attachment(data)
}

// 4. Create a factory for body Payload Structs for Handling Commands

// ptr should be a pointer to the interface, not the interface itself!!!!
func parseBody(ptr interface{}, prefix TCPRequestPrefix, body *[]byte) error {
	var err error
	if prefix.IsBase64Enc {
		*body, err = base64Decode(body)
		if err != nil {
			return err
		}
	}

	if prefix.IsJSON {
		return parseJson(ptr, body)
	}

	return parseASN1(ptr, body)
}

// 5. Switch based on RequestHeader.Command
func switchOnCommand(header RequestHeader, bodyFactories RequestBodyFactories, isSecureConnection bool) ([]byte, error) {
	var res CommandResponse

	if needsSecurity(header.Command) && !isSecureConnection {
		return newErrorJson("Unsecure Connection!"), nil
	}

	switch header.Command {
	// Empty Commands
	case cmdEmpty:
		res = successfulResponse()
		break

	// TLS Commands
	case cmdRegister:
		res = register(header, bodyFactories, isSecureConnection)
		break

	case cmdLogin:
		res = login(header, bodyFactories, isSecureConnection)
		break

	// cmdStartTLS is an exception to this switch statement. (It occurs in main.go)

	// Through Commands (To Third Party)
	case cmdAction:
		res = applyAction(header, bodyFactories, isSecureConnection)
		break

	case cmdObserve:
		res = getGameData(header, bodyFactories, isSecureConnection)
		break

	// User Management Commands
	case cmdGetUser:
		res = getUser(header, bodyFactories, isSecureConnection)
		break

	// Game Management Commands
	case cmdGameCreate:
		res = createGame(header, bodyFactories, isSecureConnection)
		break

	case cmdGameJoin:
		res = joinGame(header, bodyFactories, isSecureConnection)
		break

	case cmdGameLeave:
		res = leaveGame(header, bodyFactories, isSecureConnection)
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
func base64Decode(data *[]byte) ([]byte, error) {
	res := make([]byte, base64.StdEncoding.DecodedLen(len(*data)))
	_, err := base64.StdEncoding.Decode(*data, res)
	return res, err
}

func decodeJsonAttachment(data *[]byte) (RequestAttachment, int, error) {
	res := RequestAttachment{}
	err := json.Unmarshal(*data, &res)
	if err != nil {
		return res, 0, err
	}

	var counter int
	length := len(*data)
	for counter = 0; counter < length; counter += 1 {
		if (*data)[counter] == byte('}') {
			break
		}
	}

	bodyFactoryStart := counter

	return res, bodyFactoryStart, nil
}

func decodeASN1Attachment(data *[]byte) (RequestAttachment, int, error) {
	res := RequestAttachment{}
	body, err := asn1.Unmarshal(*data, &res)
	if err != nil {
		return res, 0, err
	}

	bodyStart := len(*data) - len(body)

	return res, bodyStart, nil
}

func parseJson(ptr interface{}, data *[]byte) error {
	return json.Unmarshal(*data, ptr)
}

func parseASN1(ptr interface{}, data *[]byte) error {
	empty, err := asn1.Unmarshal(*data, ptr)
	if err != nil {
		return err
	} else if len(empty) > 0 {
		log.Println("ASN1 Parsing Has Extra Bytes... Returning Anyways!")
	}

	return nil
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

func rawSuccessfulResponseBytes(msg *[]byte) CommandResponse {
	return CommandResponse{
		UseRaw: true,
		Raw:    *msg,
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
