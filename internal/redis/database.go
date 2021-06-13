package redis

import (
	"log"

	"github.com/mediocregopher/radix/v3"
)

//// Redis/DB Settings

// Redis IP Connecting Address
const RedisIpAddress string = "127.0.0.1"

// Redis Connection Port Number
const RedisPortNumber string = ":6379"

// The Maximum Length of a String Key Redis can receive
const RedisKeyMax int = 512000000

//// Global Variables | Singletons

// Global Reference for Redis -- Threadsafe
var MasterRedis radix.Client = nil

// ServerTask Startup Function for Redis Database. Takes care of initialization.
func StartDatabase() (func(), error) {

	// Ah yes! A Thread safe pool implementation. PERRRRFECT
	pool, err := radix.NewPool("tcp", RedisIpAddress+RedisPortNumber, 10)
	if err != nil {
		return nil, err
	}

	MasterRedis = pool

	err = MasterRedis.Do(radix.Cmd(log.Writer(), "PING", "REDIS HAS BEEN PINGED!\n"))
	if err != nil {
		return nil, err
	}

	return cleanUpDatabase, nil
}

// CleanUp Function returned by Startup function. Closes the Redis Client
func cleanUpDatabase() {
	log.Println("Cleaning Up Database Logic")
	MasterRedis.Close()
}
