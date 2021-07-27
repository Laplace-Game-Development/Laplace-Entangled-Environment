// Policy is the type definitions and decoupling pattern for route and data.
// routes listen and inquire for commands to send to data
// data then sends back the responses for routes to use
// the format of communication between these modules is described here.
// It helps to define the structures used in requests and responses so
// the functions can be used internall (like with schedule)
package policy

import (
	"encoding/json"
	"reflect"
	"time"
)

////  Configurables

// The amount of time a game can go without an action for. If nothing occurs
// for x time on a game then it should be deleted
const StaleGameDuration time.Duration = time.Duration(time.Minute * 5)

// UserID For Request Made From the Server rather than
// from a user. Useful for papertrails.
const SuperUserID string = "-1"

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Request Definitions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

//// Incomming Request Definitions

// Establish which endpoint/action the request is for
//
// Typically the 2nd and 3rd bytes of a TCP Request
type ClientCmd int64

// TCP Request Attachment
// JSON or ASN1 Attachment for Authentication and Regulation
//
// Parsed Into Request Header
type RequestAttachment struct {
	// Who the Command is From
	UserID string

	// Request Signature for Authentication
	Sig string
}

//// Private Request Definitions For Parsing

// The Request Header represents the metadata for requests that
// all requests need. This represents the selected endpoint and
// authentication data needed for some commands (these values)
// may be empty for public commands.
type RequestHeader struct {
	// Command To Be Handled (i.e. Sent in TCP Prefix)
	Command ClientCmd

	// Who the Command is From
	UserID string

	// Request Signature for Authenticated Requests
	Sig string
}

// The Request Body Represents the data for the command. Since
// each command has different arguments (and using byte slices
// can only go so far) we use factory functions to gather all
// the required data. (I use Factories here but they are more
// defered Transformation/Map Functions).
type RequestBodyFactories struct {
	// Interface Parameter should be a pointer
	ParseFactory func(ptr interface{}) error

	SigVerify func(userID string, userSig string) error
}

// Required Fields for any connection
// see calculateResponse
// or see switchOnCommand
//
// The structure is wrapped for easy of returning from a
// constructing function.
type InternalUserRequest struct {
	Header             RequestHeader
	BodyFactories      RequestBodyFactories
	IsSecureConnection bool
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Response Definitions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Abstracted Response Information from a Command
//
// The Command Response is a structure that with certain
// values filled is communicable back to the route/listner
// module functions to then transform back into
// packets/bytes/http whatever.
//
// While you are perfectly allowed to define these on your own
// there are premade functions below for common responses.
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

// Data Interface for JSON parsing. Isn't really used
// to communicate within the Application, but makes
// creating the Json with golangs Json package easier.
//
// The Struct has to be public so the package can parse,
// but refrain from using in parameters/return types etc.
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

// Enum Consisting of all the ClientCommands.
// The comments also map what each command should map to in TCP
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

// Reject the request and tell the user to come back tomorrow...
//    there are server issues today
//
// err : Error received from doing something
//   (something that contains a string to be logged)
func RespWithError(err error) CommandResponse {
	return CommandResponse{ServerError: err}
}

// Reject the request by telling the user what they did wrong...
// "Sorry Hacker but our server doesn't work like that"
// ...
// Maybe something a bit nicer...
//
// err : Error received from doing something
//   (something that contains a string to be sent to the user)
func UnSuccessfulResponseError(err error) CommandResponse {
	return CommandResponse{
		Data:   SuccessfulData{false, err.Error()},
		Digest: json.Marshal,
	}
}

// Reject the request by telling the user what they did wrong...
// "Sorry Hacker but our server doesn't work like that"
// ...
// Maybe something a bit nicer...
//
// err : a string to be sent to the user
func UnSuccessfulResponse(err string) CommandResponse {
	return CommandResponse{
		Data:   SuccessfulData{false, err},
		Digest: json.Marshal,
	}
}

// Accept the request and respond with the mystical "Successful: true"
func SuccessfulResponse() CommandResponse {
	return CommandResponse{
		Data:   SuccessfulData{Successful: true},
		Digest: json.Marshal,
	}
}

// Send bytes back to the user
//
// msg :: byte slice of what you want to be sent back
func RawSuccessfulResponseBytes(msg *[]byte) CommandResponse {
	return CommandResponse{
		UseRaw: true,
		Raw:    *msg,
	}
}

// Send bytes back to the user
//
// msg :: string of what you want to be sent back
//
// WARNING: msg should not be a constant string!
func RawSuccessfulResponse(msg string) CommandResponse {
	return CommandResponse{
		UseRaw: true,
		Raw:    []byte(msg),
	}
}

// Send bytes back to the user
//
// msg :: string of what you want to be sent back
//
// WARNING: msg should not be a constant string!
// No different from RawSuccessfulResponse, but we may
// change that in the future.
func RawUnsuccessfulResponse(err string) CommandResponse {
	return CommandResponse{
		UseRaw: true,
		Raw:    []byte(err),
	}
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Internal Request Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Function to construct internal request with from a "Super User". Useful for
// using endpoints with a specific papertrail. Super Users have a lot
// more privaledges in checks than other users, but they can do this because
// they skip the parsing steps. SigVerify automatically returns a nil error.
// This could(/should) never happen from outside the system.
//
// isTask :: true if task is making the request and false otherwise
//      (old parameter and not necessary)
// cmd    :: Selected Endpoint to be requested
// args   :: struct to use for args for endpoint
// returns -> InternalUserRequest struct for making the request.
func RequestWithSuperUser(isTask bool, cmd ClientCmd, args interface{}) (InternalUserRequest, error) {
	// shortcut bodyfactory using reflection
	bodyFactories := RequestBodyFactories{
		ParseFactory: func(ptr interface{}) error {
			ptrValue := reflect.ValueOf(ptr)
			argsVal := reflect.ValueOf(args)
			ptrValue.Elem().Set(argsVal)
			return nil
		},
		SigVerify: func(userID string, userSig string) error {
			return nil
		},
	}

	// Body Start is only used in main.go and is not necessary for a manual request command
	header := RequestHeader{Command: cmd, UserID: SuperUserID}

	return InternalUserRequest{Header: header, BodyFactories: bodyFactories, IsSecureConnection: true}, nil
}

// Function to construct internal request with from a given user.
// Used for unit testing
//
// userID :: String User ID for Database
// isTask :: true if task is making the request and false otherwise
//      (old parameter and not necessary)
// cmd    :: Selected Endpoint to be requested
// args   :: struct to use for args for endpoint
// returns -> InternalUserRequest struct for making the request.
func RequestWithUserForTesting(userID string, isTask bool, cmd ClientCmd, args interface{}) (InternalUserRequest, error) {
	// shortcut bodyfactory using reflection
	bodyFactories := RequestBodyFactories{
		ParseFactory: func(ptr interface{}) error {
			if args == nil {
				return nil
			}

			ptrValue := reflect.ValueOf(ptr)
			argsVal := reflect.ValueOf(args)
			ptrValue.Elem().Set(argsVal)
			return nil
		},
		SigVerify: func(userID string, userSig string) error {
			return nil
		},
	}

	// Body Start is only used in main.go and is not necessary for a manual request command
	header := RequestHeader{Command: cmd, UserID: userID}

	return InternalUserRequest{Header: header, BodyFactories: bodyFactories, IsSecureConnection: true}, nil
}
