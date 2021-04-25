/*
 * Tristan Hilbert
 * 4/9/2021
 * Laplace Entangled Environment Server Middleware
 * 
 */


import zmq from "zeromq"
import yargs from 'yargs'
import { hideBin } from 'yargs/helpers'

const args = yargs(hideBin(process.argv)).argv;

if (args.binding === undefined) {
    throw new Error("Binding must be provided to application!");
}

// Boolean flag for ignoring any sends until we have received information from server
var hasReceivedRequest = false;

// Socket For communicating to Laplace Entangled Environment
var sock;

// Binding IP Address
const binding = args.binding;




/***********************************************************************
 * Public Interface
 * 
 ***********************************************************************/

/**
 * Starts the Message Queueing Middleware
 * @param {function} callback called with no parameters when process is completed 
 * @returns {Promise} resolves when process with no parameters is completed 
 *     or rejects with error if error occurs
 */
export function start(callback) {
    var address = "tcp://127.0.0.1:" + binding;
    sock = zmq.socket("rep");

    return new Promise(async (resolve, reject) => {
        try {
            console.log("STARTING Laplace Entangled Middleware!");
            console.log("binding to: " + address);
            await sock.bind(address);
            console.log("STARTED Laplace Entangled Middleware!");
            resolve();
            callback != undefined && callback();
        } catch (err) {
            console.error(err);
            reject(err);
        }
    });
}

/**
 * Checks if the program is the middleware or the client-sdk
 * @returns {boolean} true
 */
export function isServer(){
    return true;
}

/**
 * Send Gamestate to the client and/or wait for a response
 * @param {any} state JS Object of Game State
 * @param {function} callback Callback Function once Message is received
 * 
 */
export function sendGameState(state, callback) {
    /*
     * Alone, sock.recieve with no configuration will block 
     * the Application until a message is received
     */

    if(hasReceivedRequest){
        sock.send(["", JSON.stringify(state)]).then(
            socketReceive.bind(null, callback),
            onError.bind(null, callback)
        );
    }else {
        socketReceive(callback);
    }
}

// cmdRegister
export function register(username, password, callback) {
    callback(null);
}

// cmdNewToken
export function newToken(username, password, callback) {
    callback(null);
}

// cmdStartTLS
export function startTLS(callback) {
    callback(null);
}

// cmdObserve
export function observeGame(gameID, callback){
    callback(null);
}

// cmdGetUser
export function getAuthID(username, callback){
    callback(null);
}

// cmdGameCreate
export function createGame(callback){
    callback(null);
}

// cmdGameJoin
export function joinGame(gameID, callback){
    callback(null);
}


// cmdGameLeave
export function leaveGame(callback){
    callback(null);
}


// cmdGameDelete
export function deleteGame(gameID, callback){
    callback(null);
}

/***********************************************************************
 * Private Utilities
 * 
 ***********************************************************************/

function socketReceive(callback) {
    sock.on("message", onReceiveSuccess.bind(null, callback));
}

function onReceiveSuccess(callback, messages) {
    console.log("Laplace Entangled Recieved: ");
    console.log(messages);

    hasReceivedRequest = true;
    callback(null, messages);
}

function onError(callback, error){
    callback(error);   
}