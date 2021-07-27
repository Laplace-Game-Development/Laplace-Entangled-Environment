package util

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"testing"
)

// Number of checks done in RandStringN testing
const randomChecks = 500

// Number of characters in given strings
const numberOfChars = 128

// The Maximum Weight a random String Should See before failure
const maxPercentWeight = 20

var (
	cwd_arg = flag.String("cwd", "", "set cwd")
)

func TestMain(m *testing.M) {
	flag.Parse()
	if *cwd_arg != "" {
		if err := os.Chdir(*cwd_arg); err != nil {
			fmt.Println("Chdir error:", err)
		}
	}

	os.Exit(m.Run())
}

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

func TestRandStringN(t *testing.T) {
	checkList := map[string]int{}
	var temp string
	var value int
	var exists bool
	for i := 0; i < randomChecks; i++ {
		temp = RandStringN(numberOfChars)
		if len(temp) != numberOfChars {
			t.Errorf("Did Not Get Expected Number of Characters. Length: %d\tString %v\n", len(temp), temp)
		}

		value, exists = checkList[temp]

		if !exists {
			checkList[temp] = 1
		} else if (value * 100 / i) > maxPercentWeight {
			checkList[temp] += 1
			t.Errorf("Got too many of the same string!\nMap: %v\n", checkList)
		}
	}
}

func TestBatchReadConnection(t *testing.T) {
	message := "This is a really long message with not EOT character!"
	messageEOT := message + string(byte(4)) + "YOLOLOLOLO"

	conn := TestReader{
		msg: message,
		eof: false,
	}

	var buf bytes.Buffer

	io.Copy(&buf, &conn)

	conn.eof = false
	buf2, err := BatchReadConnection(&conn, byte(4), 512, 10)
	if err != nil {
		t.Errorf("Got Error From Batch Read! Err: %v\n", err)
	}

	expected := buf.String()
	actual := string(buf2)

	if expected != actual {
		t.Errorf("Expected did not match actual! Expected: %s | Actual %s\n", expected, actual)
	}

	conn = TestReader{
		msg: messageEOT,
		eof: false,
	}

	buf2, err = BatchReadConnection(&conn, byte(4), 512, 10)
	if err != nil {
		t.Errorf("Got Error From Batch Read! Err: %v\n", err)
	}

	actual = string(buf2)

	if expected != actual {
		t.Errorf("Expected did not match actual! Expected: %s | Actual %s\n", expected, actual)
	}
}

type TestReader struct {
	msg string
	eof bool
}

func (reader *TestReader) Read(b []byte) (int, error) {
	if !reader.eof {
		reader.eof = true
	} else {
		return 0, io.EOF
	}

	bytes := []byte(reader.msg)
	length := len(bytes)
	for i := 0; i < length; i++ {
		b[i] = bytes[i]
	}
	return length, nil
}
