package main

import (
	"fmt"
	"log"

	"github.com/mediocregopher/radix/v3"
	"github.com/robfig/cron/v3"
)

type CronEvent struct {
	Schedule string
	Event    func()
}

// Ledger of Cron Events
// This shouldn't be changed by the code.
var initialCronLedger []CronEvent = []CronEvent{
	{"5 * * * * *", eventCheckHealth},
}

// Global Variables | Singletons
var masterCron *cron.Cron = nil

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Scheduler Core Functionality
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func startCronScheduler() (func(), error) {
	err := initialSchedule()
	if err != nil {
		return nil, err
	}

	return cleanUpCronScheduler, nil
}

func cleanUpCronScheduler() {
	log.Println("Cleaning Cron Jobs!")
	ctx := masterCron.Stop()
	select {
	case <-ctx.Done():
		log.Println(ctx.Err())
	default:
	}
	log.Println("Cron Jobs Clean!")
}

func initialSchedule() error {
	masterCron = cron.New(cron.WithSeconds())

	for i, cronEvent := range initialCronLedger {
		_, err := masterCron.AddFunc(cronEvent.Schedule, cronEvent.Event)
		if err != nil {
			log.Printf("Error Reached at Cron Event Index: %d\n", i)
			return err
		}
	}

	return nil
}

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Schedule Events -- Cron Subscribed Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func eventCheckHealth() {
	gameIDSlice := make([]string, eventHealthTaskCapacity)
	gameIDSlicePrefixed := make([]string, eventHealthTaskCapacity)

	err := masterRedis.Do(radix.Cmd(&gameIDSlice, "LPOP", healthTaskQueue, fmt.Sprintf("%d", eventHealthTaskCapacity)))
	if err != nil {
		log.Fatalln("Trouble Using Health Event: " + err.Error())
	}

	if len(gameIDSlice) == 0 {
		return
	}

	temp := make([]string, 1)

	for i, s := range gameIDSlice {
		temp[0] = s
		gameIDSlicePrefixed[i] = constructTaskWithPrefix(healthTaskPrefix, temp)
	}

	err = sendTasksToWorkers(gameIDSlicePrefixed)
	if err != nil {
		log.Fatalf("Trouble Using Health Event! Error: %v", err.Error())
	}
}
