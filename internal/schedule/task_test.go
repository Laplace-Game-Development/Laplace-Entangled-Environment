package schedule

import (
	"testing"
	"time"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/redis"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/startup"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/util"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/zeromq"
	"github.com/mediocregopher/radix/v3"
)

//// Configurables
const unitTestTableName string = "unitTestSet"
const values int = 20
const waitTime time.Duration = time.Second * 5

func TestTask(t *testing.T) {
	cleanup := startup.InitServerStartupOnTaskList(
		[]startup.ServerTask{
			redis.StartDatabase,
			zeromq.StartZeroMqComms,
			StartTaskQueue,
		})
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
	redis.MainRedis.Do(radix.Cmd(nil, "DEL", unitTestTableName))

	SendTasksToWorkers(msgs...)

	// No easy way to tell when work is done so just sleep
	time.Sleep(waitTime)

	var uniqueMsgs []string
	redis.MainRedis.Do(radix.Cmd(&uniqueMsgs, "SMEMBERS", unitTestTableName))
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
	redis.MainRedis.Do(radix.Cmd(&expected, "SCARD", unitTestTableName))
	if actual != expected {
		t.Error("Did not get all keys expected for Task Working... Consider Increasing Sleep Period!")
	}

	// Delete Key When Finished
	redis.MainRedis.Do(radix.Cmd(nil, "DEL", unitTestTableName))
}
