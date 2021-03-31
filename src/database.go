package main

import (
	"log"

	"github.com/mediocregopher/radix/v3"
)

// Redis/DB Settings
const redisIpAddress string = "127.0.0.1"
const redisPortNumber string = ":6379"

const redisKeyMax int = 512000000

// Global Variables | Singletons
var masterRedis radix.Client = nil

func startDatabase() (func(), error) {

	// Ah yes! A Thread safe pool implementation. PERRRRFECT
	pool, err := radix.NewPool("tcp", redisIpAddress+redisPortNumber, 10)
	if err != nil {
		return nil, err
	}

	masterRedis = pool

	err = masterRedis.Do(radix.Cmd(log.Writer(), "PING", "REDIS HAS BEEN PINGED!\n"))
	if err != nil {
		return nil, err
	}

	return func() { masterRedis.Close() }, nil
}
