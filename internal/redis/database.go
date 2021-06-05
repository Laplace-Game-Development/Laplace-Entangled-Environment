package redis

import (
	"log"

	"github.com/mediocregopher/radix/v3"
)

// Redis/DB Settings
const RedisIpAddress string = "127.0.0.1"
const RedisPortNumber string = ":6379"

const RedisKeyMax int = 512000000

// Global Variables | Singletons
var MasterRedis radix.Client = nil

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

func cleanUpDatabase() {
	log.Println("Cleaning Up Database Logic")
	MasterRedis.Close()
}
