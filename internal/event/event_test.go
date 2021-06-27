package event

import (
	"testing"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/mediocregopher/radix/v3"
)

func TestEvent(t *testing.T) {
	cleanup, err := redis.StartDatabase()
	if err != nil {
		t.Errorf("Error starting redis connection! Err: %v\n", err)
	}
	defer cleanup()

	expected := "DERPDERP"
	err = SubmitGameForHealthCheck(expected)
	if err != nil {
		t.Errorf("An Error Occurred when adding to Queue! Err: %v\n", err)
	}

	var actual string
	err = redis.MasterRedis.Do(radix.Cmd(&actual, "LINDEX", HealthTaskQueue, "-1"))
	if err != nil {
		t.Errorf("An Error Occurred When Reading From Redis! Err: %v\n", err)
	}

	if actual != expected {
		t.Errorf("Expected '%s' but got '%s'!\n", expected, actual)
	}

	err = redis.MasterRedis.Do(radix.Cmd(&actual, "RPOP", HealthTaskQueue))
	if err != nil {
		t.Errorf("An Error Occurred When Reading From Redis! Err: %v\n", err)
	}
}