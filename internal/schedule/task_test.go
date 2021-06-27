package schedule

import (
	"testing"
	"time"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/util"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/zeromq"
	"github.com/mediocregopher/radix/v3"
)

//// Configurables
const unitTestTableName string = "unitTestSet"
const values int = 20
const waitTime time.Duration = time.Second * 5

func TestTask(t *testing.T) {
	cleanup, err := redis.StartDatabase()
	if err != nil {
		t.Errorf("Error starting redis connection! Err: %v\n", err)
	}
	defer cleanup()

	cleanup, err = zeromq.StartZeroMqComms()
	if err != nil {
		t.Errorf("Error starting zeromq Communication Layer! Err: %v\n", err)
	}
	defer cleanup()

	cleanup, err = StartTaskQueue()
	if err != nil {
		t.Errorf("Error starting Task Queue! Err: %v\n", err)
	}
	defer cleanup()

	t.Run("UnitTestTask", testUnitTestTask)
}

func testUnitTestTask(t *testing.T) {
	t.Log("Starting UnitTestTask! \n")
	msgs := make([]string, values)
	set := map[string]bool{}
	var temp string
	for i := 0; i < values; i++ {
		temp = util.RandStringN(128)
		msgs[i] = constructTaskWithPrefix(TestTaskPrefix, unitTestTableName, temp)
		set[temp] = false
	}

	// Delete Key In case of previous failed run attempts
	redis.MasterRedis.Do(radix.Cmd(nil, "DEL", unitTestTableName))

	SendTasksToWorkers(msgs...)

	// No easy way to tell when work is done so just sleep
	time.Sleep(waitTime)

	var uniqueMsgs []string
	redis.MasterRedis.Do(radix.Cmd(&uniqueMsgs, "SMEMBERS", unitTestTableName))
	t.Logf("Got Members! Msgs: %v\n", uniqueMsgs)

	var value bool
	var exists bool
	for _, msg := range uniqueMsgs {
		value, exists = set[msg]
		if !exists || value {
			t.Errorf("Had Erroneous value in Set! Msg: %v\n", msg)
		}

		set[msg] = true
	}

	actual := len(set)
	var expected int
	redis.MasterRedis.Do(radix.Cmd(&expected, "SCARD", unitTestTableName))
	if actual != expected {
		t.Error("Did not get all keys expected for Task Working... Consider Increasing Sleep Period!")
	}

	// Delete Key When Finished
	redis.MasterRedis.Do(radix.Cmd(nil, "DEL", unitTestTableName))
}
