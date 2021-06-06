package redis

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/mediocregopher/radix/v3"
)

// Configurables
const AtomicTestKey string = "AtomicTest"
const CardinalityTestKey string = "CardHashTest"
const MultiHSetTestPrefix string = "MultiHSet:"
const MultiHSetTestFieldA string = "A"
const MultiHSetTestFieldB string = "B"
const MultiHSetTestFieldC string = "C"
const AtomicTestIterations int = 100

var MultiHSetTestIDs []string = []string{"1", "2", "3"}

func TestStartDatabase(t *testing.T) {
	cleanup, err := StartDatabase()
	if err != nil {
		t.Errorf("Connecting to Redis Resulted in Error! Err: %v\n", err)
	}

	// Run Database Transactions
	t.Run("PING-PONG", testPingPong)
	t.Run("ATOMIC-COUNT", testAtomicCounter)
	t.Run("CARD", testCardinality)
	t.Run("MULTI-SET", testMultiHSet)

	// Complete!
	cleanup()
}

// NOTE: This test starts with a lowercase letter because it is a subtest
func testPingPong(t *testing.T) {
	var message string
	err := MasterRedis.Do(radix.Cmd(&message, "PING"))
	if err != nil {
		t.Errorf("Redis Could Not Be Pinged! Err: %v\n", err)
	} else if message != "PONG" {
		t.Errorf("Did not recieve PONG from PING! Message: %s\n", message)
	}
}

func testAtomicCounter(t *testing.T) {
	var result int
	counter := 0
	var counterNext int
	err := MasterRedis.Do(radix.Cmd(nil, "DEL", AtomicTestKey))
	if err != nil {
		t.Errorf("Could Not Delete Key %s. Err: %v\n", AtomicTestKey, err)
	}

	err = MasterRedis.Do(radix.Cmd(&result, "SETNX", AtomicTestKey,
		fmt.Sprintf("%d", counter)))

	if err != nil {
		t.Errorf("Could Not Set Value at Key %s. Err: %v\n", AtomicTestKey, err)
	} else if result == 0 {
		t.Errorf("Result From Setting Deleted Value is 0 rather than 1.\n")
	}

	for i := 0; i < AtomicTestIterations; i++ {
		err = MasterRedis.Do(radix.Cmd(&counterNext, "INCR", AtomicTestKey))
		if counterNext <= counter || (counter == (^0) && counterNext == 0) {
			t.Errorf("Result was less than or equal to previous increment\n")
		}
	}

	err = MasterRedis.Do(radix.Cmd(nil, "DEL", AtomicTestKey))
	if err != nil {
		t.Errorf("Could Not Delete Key %s. Err: %v\n", AtomicTestKey, err)
	}
}

func testCardinality(t *testing.T) {
	var result int
	var cardinality int
	hashSet := map[string]bool{
		"foo":  true,
		"bar":  true,
		"derp": true,
		"lol":  true,
	}

	err := MasterRedis.Do(radix.Cmd(nil, "DEL", CardinalityTestKey))
	if err != nil {
		t.Errorf("Could Not Delete Key %s. Err: %v\n", CardinalityTestKey, err)
	}

	count := 0
	for key := range hashSet {
		err = MasterRedis.Do(radix.Cmd(&result, "SADD", CardinalityTestKey, key))
		if err != nil {
			t.Errorf("Could Not SADD to Key %s. Err: %v\n", CardinalityTestKey, err)
		} else if result == 0 {
			t.Errorf("Result From Setting Deleted Value is 0 rather than 1.\n")
		}

		count += 1
	}

	// Do It Again to Show Cardinality
	for key := range hashSet {
		err = MasterRedis.Do(radix.Cmd(&result, "SADD", CardinalityTestKey, key))
		if err != nil {
			t.Errorf("Could Not SADD to Key %s. Err: %v\n", CardinalityTestKey, err)
		} else if result != 0 {
			t.Errorf("Result From Setting Deleted Value is 1 rather than 0 (Does the value not exist?).\n")
		}
	}

	err = MasterRedis.Do(radix.Cmd(&cardinality, "SCARD", CardinalityTestKey))
	if err != nil {
		t.Errorf("Could Not SADD to Key %s. Err: %v\n", CardinalityTestKey, err)
	} else if cardinality != count {
		t.Errorf("Result From Cardinality != Count.\n %d != %d\n", cardinality, count)
	}

	err = MasterRedis.Do(radix.Cmd(nil, "DEL", CardinalityTestKey))
	if err != nil {
		t.Errorf("Could Not Delete Key %s. Err: %v\n", CardinalityTestKey, err)
	}
}

func testMultiHSet(t *testing.T) {
	var err error
	var expected string
	var expectedMilli int64
	var actualMilli int64
	var result string
	results := make([]string, 3)
	var success int

	// Cleanup Before
	for _, val := range MultiHSetTestIDs {
		err = MasterRedis.Do(radix.Cmd(nil, "DEL", MultiHSetTestPrefix+val))
		if err != nil {
			t.Errorf("Could Not Delete Key %s. Err: %v\n", MultiHSetTestPrefix+val, err)
		}
	}

	for _, val := range MultiHSetTestIDs {
		expected = "foo" + val
		expectedMilli = time.Now().UTC().Unix()

		err = MasterRedis.Do(radix.Cmd(nil, "HSET", MultiHSetTestPrefix+val,
			MultiHSetTestFieldA, expected,
			MultiHSetTestFieldB, "bar",
			MultiHSetTestFieldC, fmt.Sprintf("%d", expectedMilli)))
		if err != nil {
			t.Logf("It seems we hit an error. This could occur in redis versions < 4.0\n")
			t.Errorf("Could Set Fields %s. Err: %v\n", MultiHSetTestPrefix+val, err)
		}

		err = MasterRedis.Do(radix.Cmd(&result, "HGET", MultiHSetTestPrefix+val, MultiHSetTestFieldA))
		if err != nil {
			t.Errorf("Could Not Gather Field For Set: %s Field %s! Err: %v\n", MultiHSetTestPrefix+val, MultiHSetTestFieldA, err)
		} else if result != expected {
			t.Errorf("The Result Did Not Match expected/Expected Value. Expected: %s != Actual: %s!\n", result, expected)
		}

		expected = "foobar"
		err = MasterRedis.Do(radix.Cmd(&success, "HSET", MultiHSetTestPrefix+val, MultiHSetTestFieldA, expected))
		if err != nil {
			t.Errorf("Could Not Set Field For Set: %s Field %s! Err: %v\n", MultiHSetTestPrefix+val, MultiHSetTestFieldA, err)
		} else if success == 1 {
			t.Errorf("Changing a field value added a field. Field: %s New Value: %s\n", MultiHSetTestFieldA, expected)
		}

		err = MasterRedis.Do(radix.Cmd(&result, "HGET", MultiHSetTestPrefix+val, MultiHSetTestFieldA))
		if err != nil {
			t.Errorf("Could Not Gather Field For Set: %s Field %s! Err: %v\n", MultiHSetTestPrefix+val, MultiHSetTestFieldA, err)
		} else if result != expected {
			t.Errorf("The Result Did Not Match expected/Expected Value. Expected: %s != Actual: %s!\n", result, expected)
		}

		err = MasterRedis.Do(radix.Cmd(&results, "HMGET", MultiHSetTestPrefix+val,
			MultiHSetTestFieldA,
			MultiHSetTestFieldB,
			MultiHSetTestFieldC))

		if err != nil {
			t.Errorf("Could Not Gather All Fields For Set: %s! Err: %v\n", MultiHSetTestPrefix+val, err)
		}

		actualMilli, err = strconv.ParseInt(results[2], 10, 64)
		if err != nil || results[0] != expected || results[1] != "bar" || actualMilli != expectedMilli {
			t.Errorf("Expected Fields Were Not Found.\nExpected: %v\nFound: %v\n\n",
				[]string{expected, "bar", fmt.Sprintf("%d", expectedMilli)},
				results,
			)
		}
	}

	// Cleanup After
	for _, val := range MultiHSetTestIDs {
		err = MasterRedis.Do(radix.Cmd(nil, "DEL", MultiHSetTestPrefix+val))
		if err != nil {
			t.Errorf("Could Not Delete Key %s. Err: %v\n", MultiHSetTestPrefix+val, err)
		}
	}
}
