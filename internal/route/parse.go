package route

import (
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"

	"laplace-entangled-env.com/internal/data"
	"laplace-entangled-env.com/internal/policy"
	"laplace-entangled-env.com/internal/util"
)

// This should never change during runtime!
var commandMap map[int64]policy.ClientCmd = map[int64]policy.ClientCmd{
	1<<0 + 0: policy.CmdEmpty,
	1<<0 + 1: policy.CmdRegister,
	1<<0 + 2: policy.CmdLogin,
	1<<4 + 0: policy.CmdAction,
	1<<4 + 1: policy.CmdObserve,
	1<<5 + 0: policy.CmdGetUser,
	1<<9 + 0: policy.CmdGameCreate,
	1<<9 + 1: policy.CmdGameJoin,
	1<<9 + 2: policy.CmdGameLeave,
	1<<9 + 3: policy.CmdGameDelete,
}

//// Functions!

// 1. Parse Command and add to Header
func ParseCommand(mostSignificant byte, leastSignificant byte) (policy.ClientCmd, error) {
	var cmd int64 = int64(mostSignificant)
	cmd = (cmd << 8) + int64(leastSignificant)

	result, exists := commandMap[cmd]

	if exists == false {
		return 0, errors.New("Invalid Command")
	}

	return result, nil
}

// 2. Parse Request Attachment and add to Header
func parseRequestAttachment(isJSON bool, data *[]byte) (policy.RequestAttachment, int, error) {
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
func switchOnCommand(header policy.RequestHeader, bodyFactories policy.RequestBodyFactories, isSecureConnection bool) ([]byte, error) {
	var res policy.CommandResponse

	if NeedsSecurity(header.Command) && !isSecureConnection {
		return util.NewErrorJson("Unsecure Connection!"), nil
	}

	switch header.Command {
	// Empty Commands
	case policy.CmdEmpty:
		res = policy.SuccessfulResponse()
		break

	// TLS Commands
	case policy.CmdRegister:
		res = Register(header, bodyFactories, isSecureConnection)
		break

	case policy.CmdLogin:
		res = Login(header, bodyFactories, isSecureConnection)
		break

	// cmdStartTLS is an exception to this switch statement. (It occurs in main.go)

	// Through Commands (To Third Party)
	case policy.CmdAction:
		res = data.ApplyAction(header, bodyFactories, isSecureConnection)
		break

	case policy.CmdObserve:
		res = data.GetGameData(header, bodyFactories, isSecureConnection)
		break

	// User Management Commands
	case policy.CmdGetUser:
		res = GetUser(header, bodyFactories, isSecureConnection)
		break

	// Game Management Commands
	case policy.CmdGameCreate:
		res = data.CreateGame(header, bodyFactories, isSecureConnection)
		break

	case policy.CmdGameJoin:
		res = data.JoinGame(header, bodyFactories, isSecureConnection)
		break

	case policy.CmdGameLeave:
		res = data.LeaveGame(header, bodyFactories, isSecureConnection)
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

func decodeJsonAttachment(data *[]byte) (policy.RequestAttachment, int, error) {
	res := policy.RequestAttachment{}
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

func decodeASN1Attachment(data *[]byte) (policy.RequestAttachment, int, error) {
	res := policy.RequestAttachment{}
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
