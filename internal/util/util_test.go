package util

import (
	"crypto/rand"
	"encoding/json"
	"testing"
)

func TestClear(t *testing.T) {
	bytes0 := []byte{'a', 'b', 'c', 'd', 'e'}
	Clear(&bytes0)
	for _, b := range bytes0 {
		if b != 0 {
			t.Fatalf("Found Non-Clear byte!")
		}
	}

	bytes1 := make([]byte, 128)
	rand.Read(bytes1)
	Clear(&bytes1)
	for _, b := range bytes1 {
		if b != 0 {
			t.Fatalf("Found Non-Clear byte!")
		}
	}
}

type TestErrorStruct struct {
	Error string
}

func TestNewErrorJSON(t *testing.T) {
	bytes := NewErrorJson("FooBar")

	testStruct := TestErrorStruct{}
	err := json.Unmarshal(bytes, &testStruct)
	if err != nil {
		t.Fatalf("JSON is Malformed. Err: %v\n", err)
	}

	if testStruct.Error != "FooBar" {
		t.Fatalf("No Error Field Found in Json! Bytes: %s\n", string(bytes))
	}
}

func TestStrTokWithEscape(t *testing.T) {
	delim := "~"
	delimBytes := []byte(delim)
	messages := []string{"Foo", "Bar", "Derp"}
	joinedMessages := ""
	for i, msg := range messages {
		joinedMessages += msg
		if i != len(messages)-1 {
			joinedMessages += delim
		}
	}

	t.Logf("Conjoining Message %s\n", joinedMessages)
	joinedMessagesBytes0 := []byte(joinedMessages)
	t.Logf("Joined Bytes:%s\n", joinedMessagesBytes0)

	var tokCounter uint = 0
	var msgByte []byte
	for _, msg := range messages {
		msgByte, tokCounter =
			StrTokWithEscape(
				&delimBytes,
				&[]byte{},
				&joinedMessagesBytes0,
				tokCounter)

		if len(msgByte) == 0 {
			t.Fatalf("Expected %s, but Got %s.\nIndex: %d\n", msg, msgByte, tokCounter)
		}

		for i, c := range []byte(msg) {
			if msgByte[i] != c {
				t.Fatalf("Expected %s, but Got %s\nIndex: %d\n", msg, msgByte, tokCounter)
			}
		}
	}

	joinedMessages += "\\~derp"
	messages[len(messages)-1] += "\\~derp"
	t.Logf("Conjoining Message %s\n", joinedMessages)
	joinedMessagesBytes1 := []byte(joinedMessages)
	t.Logf("Joined Bytes:%s\n", joinedMessagesBytes1)

	tokCounter = 0
	for _, msg := range messages {
		msgByte, tokCounter =
			StrTokWithEscape(
				&delimBytes,
				&[]byte{'\\'},
				&joinedMessagesBytes1,
				tokCounter)

		if len(msgByte) == 0 {
			t.Fatalf("Expected %s, but Got %s\nIndex: %d\n", msg, msgByte, tokCounter)
		}

		for i, c := range []byte(msg) {
			if msgByte[i] != c {
				t.Fatalf("Expected %s, but Got %s\nIndex: %d\n", msg, msgByte, tokCounter)
			}
		}
	}
}

func TestConcat(t *testing.T) {
	foo := []byte("foo")
	bar := []byte("bar")
	foobar := []byte("foobar")
	barbar := []byte("barbar")
	concatBytes := make([]byte, 9)

	err := Concat(&concatBytes, &foo, 0)
	if err != nil {
		t.Fatalf("Concat Raised an Error! Err: %v\n", err)
	}
	for i := 0; i < len(foo); i++ {
		if foo[i] != concatBytes[i] {
			t.Fatalf("Expected %v\t But Got %v\nBytes: %v\n", foo[i], concatBytes[i], concatBytes)
		}
	}

	t.Logf("Woohoo Found: %s\n", concatBytes)

	err = Concat(&concatBytes, &bar, len(foo))
	if err != nil {
		t.Fatalf("Concat Raised an Error! Err: %v\n", err)
	}
	for i := 0; i < len(foobar); i++ {
		if foobar[i] != concatBytes[i] {
			t.Fatalf("Expected %v\t But Got %v\nBytes: %v\n", foobar[i], concatBytes[i], concatBytes)
		}
	}

	t.Logf("Woohoo Found: %s\n", concatBytes)

	err = Concat(&concatBytes, &bar, 0)
	if err != nil {
		t.Fatalf("Concat Raised an Error! Err: %v\n", err)
	}
	for i := 0; i < len(barbar); i++ {
		if barbar[i] != concatBytes[i] {
			t.Fatalf("Expected %v\t But Got %v\nBytes: %v\n", barbar[i], concatBytes[i], concatBytes)
		}
	}

	t.Logf("Woohoo Found: %s\n", concatBytes)
}
