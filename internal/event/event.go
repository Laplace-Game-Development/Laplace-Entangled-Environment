// The Event module represents the possible tasks a command can submit
// to be done outside of the request response loop.
// This majorly includes garbage collecting for old/unused games.
package event

import (
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/mediocregopher/radix/v3"
)

//// Table / Datastructure Names

// Name of Redis Key Storing the Health Tasks
const HealthTaskQueue string = "healthTaskQueue"

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Tasks Submissions -- Public Interface Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

// Submits a perspective game ID to be added to the Game Health Check scheduled tasks in redis.
//
// gameID :: string gameID as the unique identifier in the games Hash Tables in Redis.
//
// returns -> error if we are unable to submit the id to the database. nil otherwise.
func SubmitGameForHealthCheck(gameID string) error {
	err := redis.MainRedis.Do(radix.Cmd(nil, "RPUSH", HealthTaskQueue, gameID))
	if err != nil {
		return err
	}

	return nil
}
