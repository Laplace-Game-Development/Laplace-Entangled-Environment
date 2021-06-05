package event

import (
	"github.com/mediocregopher/radix/v3"
	"laplace-entangled-env.com/internal/redis"
)

// Table / Datastructure Names
const HealthTaskQueue string = "healthTaskQueue"

///////////////////////////////////////////////////////////////////////////////////////////////////
////
//// Tasks Submissions -- Public Interface Functions
////
///////////////////////////////////////////////////////////////////////////////////////////////////

func SubmitGameForHealthCheck(gameID string) error {
	err := redis.MasterRedis.Do(radix.Cmd(nil, "RPUSH", HealthTaskQueue, gameID))
	if err != nil {
		return err
	}

	return nil
}
