package policy

import (
	"encoding/json"
	"time"
)

// Configurables
const StaleGameDuration time.Duration = time.Duration(time.Minute * 5)

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
	ParseFactory func(ptr interface{}) error

	SigVerify func(userID string, userSig string) error
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
	CmdError ClientCmd = iota
	//                   //=====================
	//                     Empty Command
	//                   //=====================
	CmdEmpty //          //0000_0000_0000_0000
	//                   //=====================
	//                     TLS Commands
	//                   //=====================
	CmdRegister //       //0000_0000_0000_0001
	CmdLogin    //       //0000_0000_0000_0010
	//                   //=====================
	//                     Through Commands (To Third Party)
	//                   //=====================
	CmdAction  //        //0000_0000_0001_0000
	CmdObserve //        //0000_0000_0001_0001
	//                   //=====================
	//                     User Management Commands
	//                   //=====================
	CmdGetUser //        //0000_0001_0000_0000
	//                   //=====================
	//                     Game Management Commands
	//                   //=====================
	CmdGameCreate //     //0000_0010_0000_0000
	CmdGameJoin   //     //0000_0010_0000_0001
	CmdGameLeave  //     //0000_0010_0000_0010
	CmdGameDelete //     //0000_0010_0000_0011
	//                   //=====================
)

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Utility Response Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func RespWithError(err error) CommandResponse {
	return CommandResponse{ServerError: err}
}

func UnSuccessfulResponseError(err error) CommandResponse {
	return CommandResponse{
		Data:   SuccessfulData{false, err.Error()},
		Digest: json.Marshal,
	}
}

func UnSuccessfulResponse(err string) CommandResponse {
	return CommandResponse{
		Data:   SuccessfulData{false, err},
		Digest: json.Marshal,
	}
}

func SuccessfulResponse() CommandResponse {
	return CommandResponse{
		Data:   SuccessfulData{Successful: true},
		Digest: json.Marshal,
	}
}

func RawSuccessfulResponseBytes(msg *[]byte) CommandResponse {
	return CommandResponse{
		UseRaw: true,
		Raw:    *msg,
	}
}

// msg should not be a constant string!
func RawSuccessfulResponse(msg string) CommandResponse {
	return CommandResponse{
		UseRaw: true,
		Raw:    []byte(msg),
	}
}

func RawUnsuccessfulResponse(err string) CommandResponse {
	return CommandResponse{
		UseRaw: true,
		Raw:    []byte(err),
	}
}
