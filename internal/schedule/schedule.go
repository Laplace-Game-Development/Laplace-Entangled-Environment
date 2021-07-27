// The schedule package takes care of any "scheduled tasks". Using a combination of
// Cron and ZeroMQ, the schedule package distributes the consumption of tasks to
// multiple workers in order to speed up work as much as possible.
package schedule

import (
	"fmt"
	"log"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/event"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/mediocregopher/radix/v3"
	"github.com/robfig/cron/v3"
)

// A Cron Event represents a function that should be run
// at a certain schedule. This structure should be used
// when scheduling events at the onset of the application
type CronEvent struct {
	Schedule string
	Event    func()
}

// Ledger of Cron Events
// This shouldn't be changed by the code. Used when initially scheduling
// functions to be run.
var initialCronLedger []CronEvent = []CronEvent{
	{"5 * * * * *", eventCheckHealth},
}

//// Global Variables | Singletons

// Cron scheduling reference. This should only be used by this module.
// if you want to dynamically schedule (for whatever reason) use this
var mainCronInst *cron.Cron = nil

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Scheduler Core Functionality
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// ServerTask Startup Function for Cron Scheduling. Takes care of initialization.
func StartCronScheduler() (func(), error) {
	err := initialSchedule()
	if err != nil {
		return nil, err
	}

	return cleanUpCronScheduler, nil
}

// CleanUp Function returned by Startup function. Stops all Cron scheduling and reports
// errors that occur when doing so.
func cleanUpCronScheduler() {
	log.Println("Cleaning Cron Jobs!")
	ctx := mainCronInst.Stop()
	select {
	case <-ctx.Done():
		log.Println(ctx.Err())
	default:
	}
	log.Println("Cron Jobs Clean!")
}

// Schedule Initialization called from StartCronScheduler. Goes through the
// "initialCronLedger" and adds each entry to the Cron scheduling reference.
// It also intitializes the Cron scheduling instance.
func initialSchedule() error {
	mainCronInst = cron.New(cron.WithSeconds())

	for i, cronEvent := range initialCronLedger {
		_, err := mainCronInst.AddFunc(cronEvent.Schedule, cronEvent.Event)
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

// Function added through the "initialCronLedger." Pops entries off of a list representing
// possibly old and unused games that need their data cleaned up. Send the data to Workers.
// See healthTaskWork to see how this data is used.
func eventCheckHealth() {
	gameIDSlice := make([]string, EventHealthTaskCapacity)
	gameIDSlicePrefixed := make([]string, EventHealthTaskCapacity)

	err := redis.MainRedis.Do(radix.Cmd(&gameIDSlice, "LRANGE", event.HealthTaskQueue, "0", fmt.Sprintf("%d", EventHealthTaskCapacity-1)))
	if err != nil {
		log.Fatalln("Trouble Using Health Event: " + err.Error())
	}

	err = redis.MainRedis.Do(radix.Cmd(nil, "LTRIM", event.HealthTaskQueue, fmt.Sprintf("%d", EventHealthTaskCapacity), "-1"))
	if err != nil {
		log.Fatalln("Trouble Using Health Event: " + err.Error())
	}

	if len(gameIDSlice) == 0 {
		return
	}

	for i, s := range gameIDSlice {
		gameIDSlicePrefixed[i] = constructTaskWithPrefix(HealthTaskPrefix, s)
	}

	err = SendTasksToWorkers(gameIDSlicePrefixed...)
	if err != nil {
		log.Fatalf("Trouble Using Health Event! Error: %v", err.Error())
	}
}
