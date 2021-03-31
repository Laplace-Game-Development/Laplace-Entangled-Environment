package main

import (
	"encoding/json"
	"errors"
)

type ClientCmd int64
type CommandResponse struct {
	data        interface{}
	digest      func(interface{}) ([]byte, error)
	raw         []byte
	useRaw      bool
	serverError error
}

type SuccessfulData struct {
	successful bool
	err        string
}

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
}

func parseCommand(data []byte) (ClientCmd, error) {
	var cmd int64 = int64(data[0])
	cmd = (cmd << 8) + int64(data[1])

	result, exists := commandMap[cmd]

	if exists == false {
		return cmdObserve, errors.New("Invalid Command")
	}

	return result, nil
}

// TODO: Something to consider, we may want to pass the Arguments into this function as an interface
func switchOnCommand(cmd ClientCmd, clientConn ClientConn, data []byte) ([]byte, error) {
	var res CommandResponse

	if needsSecurity(cmd) && !clientConn.isSecured {
		return newErrorJson("Unsecure Connection!"), nil
	}

	switch cmd {
	// Empty Commands
	case cmdEmpty:
		res = successfulResponse()
		break

	// TLS Commands
	case cmdRegister:
		res = register(data)
		break

	case cmdNewToken:
		res = login(data)
		break

	// cmdStartTLS is an exception to this switch statement. (It occurs in main.go)

	// Through Commands (To Third Party)
	case cmdAction:
		res = applyAction(data)
		break

	case cmdObserve:
		res = getGameData(data)
		break

	// User Management Commands
	case cmdGetUserByUsername:
		res = getUserByUsername(data)
		break

	// Game Management Commands
	case cmdGameCreate:
		res = createGame(data)
		break

	case cmdGameJoin:
		res = joinGame(data)
		break

	case cmdGameLeave:
		res = leaveGame(data)
		break

	default:
		return nil, errors.New("Command is Not Defined!")
	}

	if res.serverError != nil {
		return nil, res.serverError
	} else if res.useRaw {
		return res.raw, nil
	}

	return res.digest(res.data)

}

func respWithError(err error) CommandResponse {
	return CommandResponse{serverError: err}
}

func unSuccessfulResponseError(err error) CommandResponse {
	return CommandResponse{
		data:   SuccessfulData{false, err.Error()},
		digest: json.Marshal,
	}
}

func unSuccessfulResponse(err string) CommandResponse {
	return CommandResponse{
		data:   SuccessfulData{false, err},
		digest: json.Marshal,
	}
}

func successfulResponse() CommandResponse {
	return CommandResponse{
		data:   SuccessfulData{successful: true},
		digest: json.Marshal,
	}
}

func rawSuccessfulResponseBytes(msg []byte) CommandResponse {
	return CommandResponse{
		useRaw: true,
		raw:    msg,
	}
}

// msg should not be a constant string!
func rawSuccessfulResponse(msg string) CommandResponse {
	return CommandResponse{
		useRaw: true,
		raw:    []byte(msg),
	}
}

func rawUnsuccessfulResponse(err string) CommandResponse {
	return CommandResponse{
		useRaw: true,
		raw:    []byte(err),
	}
}
