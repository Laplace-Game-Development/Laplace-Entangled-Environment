package route

import (
	"bytes"
	"encoding/asn1"
	"encoding/json"
	"errors"
	"log"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/data"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/policy"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/util"
)

// commandMap is the mappings of the 2 byte codes to ClientCommands.
// predominantly useful in parsing commands for TCP.
//
// This should never change during runtime!
var commandMap map[int64]policy.ClientCmd = map[int64]policy.ClientCmd{
	0000 + 0: policy.CmdEmpty,
	0000 + 1: policy.CmdRegister,
	0000 + 2: policy.CmdLogin,
	1<<4 + 0: policy.CmdAction,
	1<<4 + 1: policy.CmdObserve,
	1<<8 + 0: policy.CmdGetUser,
	1<<9 + 0: policy.CmdGameCreate,
	1<<9 + 1: policy.CmdGameJoin,
	1<<9 + 2: policy.CmdGameLeave,
	1<<9 + 3: policy.CmdGameDelete,
}

//// Functions!

// Parse Command takes a two byte code and returns the associated command
// or an error if it doesn't exist. Used mainly in TCP Request Parsing
func ParseCommand(mostSignificant byte, leastSignificant byte) (policy.ClientCmd, error) {
	var cmd int64 = int64(mostSignificant)
	cmd = (cmd << 8) + int64(leastSignificant)

	log.Printf("Received Command: %d", cmd)

	result, exists := commandMap[cmd]

	if exists == false {
		return 0, errors.New("Invalid Command")
	}

	return result, nil
}

// Creates the Authentication Structure based on the structure
// and byte slice provided to the function
//
// isJSON :: whether the byte slice is JSON or ASN1
// data   :: byte slice of TCP Request Data
func parseRequestAttachment(isJSON bool, data *[]byte) (policy.RequestAttachment, int, error) {
	if isJSON {
		return decodeJsonAttachment(data)
	}

	return decodeASN1Attachment(data)
}

// Function for generating structs based on a byte slice. Used to create
// RequestBodyFactories.
//
// ptr should be a pointer to the interface, not the interface itself!!!!
//
// ptr    :: pointer to a struct to populate. Make sure fields are public
//     (see json.Unmarshall in golang docs)
// prefix :: Structure Metadata
// body   :: byte slice of request data
func parseBody(ptr interface{}, prefix TCPRequestPrefix, body *[]byte) error {
	var err error
	if prefix.IsBase64Enc {
		*body, err = util.Base64Decode(body)
		if err != nil {
			return err
		}
	}

	if prefix.IsJSON {
		return parseJson(ptr, body)
	}

	return parseASN1(ptr, body)
}

// General Function for generating responsens and processing request.
// Once these fields are populated the request is ready
// (see calculateResponse).
//
// requestHeader :: Common Fields for all requests including authentication and endpoint selection
// bodyFactories :: Arguments for the commands in the form of first order functions
// isSecured     :: Whether the request came over an encrypted connection (i.e. SSL/SSH/HTTPS)
//
// returns []byte :: byte slice response for user
//          error :: non-nil when an invalid command is sent or an error occurred when processing
//             typically means request was rejected.
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
		res = data.Register(header, bodyFactories, isSecureConnection)
		break

	case policy.CmdLogin:
		res = data.Login(header, bodyFactories, isSecureConnection)
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
		res = data.GetUser(header, bodyFactories, isSecureConnection)
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

// Decodes the data byte slice from JSON using policy.RequestAttachment. It
// then returns the structure for authentication purposes
//
// data :: JSON representing a RequestAttachment structure
//
// returns -> policy.RequestAttachment :: Authentication Data For Requests
//            int :: index of the rest of the data i.e. args
//            error :: if parsing goes wrong, error is returned
//                 else it returns nil
func decodeJsonAttachment(data *[]byte) (policy.RequestAttachment, int, error) {
	res := policy.RequestAttachment{}
	dec := json.NewDecoder(bytes.NewReader(*data))
	err := dec.Decode(&res)
	if err != nil {
		return res, 0, err
	}

	return res, int(dec.InputOffset()), nil
}

// Decodes the data byte slice from ASN1 using policy.RequestAttachment. It
// then returns the structure for authentication purposes
//
// data :: ASN1 representing a RequestAttachment structure
//
// returns -> policy.RequestAttachment :: Authentication Data For Requests
//            int :: index of the rest of the data i.e. args
//            error :: if parsing goes wrong, error is returned
//                 else it returns nil
func decodeASN1Attachment(data *[]byte) (policy.RequestAttachment, int, error) {
	res := policy.RequestAttachment{}
	body, err := asn1.Unmarshal(*data, &res)
	if err != nil {
		return res, 0, err
	}

	bodyStart := len(*data) - len(body)

	return res, bodyStart, nil
}

// Wrapper Function for json.UnMarshall in case we
// do our own unmarshalling for any reason
func parseJson(ptr interface{}, data *[]byte) error {
	return json.Unmarshal(*data, ptr)
}

// Wrapper Function for asn1 for unmarshalling for extra
// error handling.
func parseASN1(ptr interface{}, data *[]byte) error {
	empty, err := asn1.Unmarshal(*data, ptr)
	if err != nil {
		return err
	} else if len(empty) > 0 {
		log.Println("ASN1 Parsing Has Extra Bytes... Returning Anyways!")
	}

	return nil
}
