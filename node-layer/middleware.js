/*
 * Tristan Hilbert
 * 4/9/2021
 * Laplace Entangled Environment Server Middleware
 * 
 */


import zmq, { socket } from "zeromq"
import yargs from 'yargs'
import { hideBin } from 'yargs/helpers'

const args = yargs(hideBin(process.argv));

if (args.binding === undefined) {
    throw new Error("Binding must be provided to application!");
}

// Boolean flag for ignoring any sends until we have received information from server
var hasReceivedRequest = false;

// Socket For communicating to Laplace Entangled Environment
var sock;

// Binding IP Address
const binding = args.binding;

/**
 * Starts the Message Queueing Middleware
 * @param {function} callback called with no parameters when process is completed 
 * @returns {Promise} resolves when process with no parameters is completed 
 *     or rejects with error if error occurs
 */
export function start(callback) {
    sock = zmq.Reply();

    return Promise((resolve, reject) => {
        try {
            console.log("STARTING Laplace Entangled Middleware!");
            await sock.bind(binding);
            console.log("STARTED Laplace Entangled Middleware!");
            resolve();
            callback();
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

export function sendGameState(state, callback) {
    /*
     * Alone, sock.recieve with no configuration will block 
     * the Application until a message is received
     */

    if(hasReceivedRequest){
        sock.send(["", JSON.stringify(state)]).then(
            socketReceive.bind(null, callback, sock.receive),
            onError.bind(null, callback)
        );
    }else {
        socketReceive(callback, sock.receive)
    }
}

function socketReceive(callback, promiseFunc) {
    promiseFunc().then(
        onReceiveSuccess.bind(null, callback),
        onError.bind(null, callback)
    );
}

function onReceiveSuccess(callback, messages) {
    console.log("Laplace Entangled Recieved: ");
    console.log(messages);

    hasReceivedRequest = true;
    callback(messages);
}

function onError(callback, error){
    console.error(err);
    callback(null);   
}