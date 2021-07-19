package route

import (
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/policy"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/util"
)

const foo = "derp"
const bar = "herp"
const baz = 1

type FoobarInterface struct {
	Foo string
	Bar string
	Baz int
}

func TestParseASN1(t *testing.T) {
	fooey := FoobarInterface{foo, bar, baz}
	marshalled, err := asn1.Marshal(fooey)
	if err != nil {
		t.Errorf("Error marshalling dummy interface! Err: %v\n", err)
	}

	t.Logf("Marshalled ASN1: %s\n", marshalled)

	fooeyAssert := FoobarInterface{}
	err = parseASN1(&fooeyAssert, &marshalled)
	if err != nil {
		t.Errorf("Error parsing/unmarshalling dummy bytes! Err: %v\n", err)
	}

	if fooey.Foo != fooeyAssert.Foo ||
		fooey.Bar != fooeyAssert.Bar ||
		fooey.Baz != fooeyAssert.Baz {
		t.Errorf("Expected Value %v Was not the actual value %v\n", fooey, fooeyAssert)
	}
}

func TestParseJson(t *testing.T) {
	fooey := FoobarInterface{foo, bar, baz}
	marshalled, err := json.Marshal(fooey)
	if err != nil {
		t.Errorf("Error marshalling dummy interface! Err: %v\n", err)
	}

	t.Logf("Marshalled JSON: %s\n", marshalled)

	fooeyAssert := FoobarInterface{}
	err = parseJson(&fooeyAssert, &marshalled)
	if err != nil {
		t.Errorf("Error parsing/unmarshalling dummy bytes! Err: %v\n", err)
	}

	if fooey.Foo != fooeyAssert.Foo ||
		fooey.Bar != fooeyAssert.Bar ||
		fooey.Baz != fooeyAssert.Baz {
		t.Errorf("Expected Value %v Was not the actual value %v\n", fooey, fooeyAssert)
	}
}

func TestBase64Decode(t *testing.T) {
	fooey := "DerpityDerp{}12345"
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(fooey)))
	base64.StdEncoding.Encode(encoded, []byte(fooey))
	t.Logf("Encoded base64 %s\n", encoded)

	fooeyAssert, err := base64Decode(&encoded)
	if err != nil {
		t.Errorf("Error parsing/unmarshalling dummy bytes! Err: %v\n", err)
	}

	fooeyAssertCasted := string(fooeyAssert)
	if fooey != fooeyAssertCasted {
		t.Errorf("Expected %s does not match Actual %s\n", fooey, fooeyAssertCasted)
	}
}

func TestSwitchCommand(t *testing.T) {
	cmd, err := ParseCommand(0, 0)
	if err != nil {
		t.Errorf("Error Parsing Command For 0 Bytes! Err: %v\n", err)
		cmd = policy.CmdEmpty
	} else if cmd != policy.CmdEmpty {
		t.Fatalf("Empty Command Was Not Expected Empty Command!\n")
	}

	// Request Empty
	fooey := FoobarInterface{}
	req, err := policy.RequestWithUserForTesting("0", false, cmd, fooey)
	if err != nil {
		t.Errorf("Error creating empty request! Err: %v\n", err)
	}

	respExpected := policy.SuccessfulResponse()
	dataExpected, err := respExpected.Digest(respExpected.Data)
	if err != nil {
		t.Errorf("Error Getting Expected Data! Err: %v\n", err)
	}

	resp, err := switchOnCommand(req.Header, req.BodyFactories, false)
	if err != nil {
		t.Errorf("Error using empty request! Err: %v\n", err)
	} else if string(resp) != string(dataExpected) {
		t.Errorf("Response to Empty Command was %s instead of %s\n", resp, dataExpected)
	}

	// Request Insecure Login
	req, err = policy.RequestWithUserForTesting("0", false, policy.CmdLogin, fooey)
	if err != nil {
		t.Errorf("Error creating empty request! Err: %v\n", err)
	}

	dataExpected = util.NewErrorJson("Unsecure Connection!")
	if err != nil {
		t.Errorf("Error Getting Expected Data! Err: %v\n", err)
	}

	resp, err = switchOnCommand(req.Header, req.BodyFactories, false)
	if err != nil {
		t.Errorf("Error using empty request! Err: %v\n", err)
	} else if string(resp) != string(dataExpected) {
		t.Errorf("Response to Empty Command was %s instead of %s\n", resp, dataExpected)
	}
}

func TestParseAttachmentBody(t *testing.T) {
	attachment := policy.RequestAttachment{UserID: "0", Sig: ""}
	marshalledSub0, err := json.Marshal(attachment)
	if err != nil {
		t.Errorf("Error marshalling dummy interface! Err: %v\n", err)
	}

	fooey := FoobarInterface{foo, bar, baz}
	marshalledSub1, err := json.Marshal(fooey)
	if err != nil {
		t.Errorf("Error marshalling dummy interface! Err: %v\n", err)
	}

	marshalled := make([]byte, len(marshalledSub0)+len(marshalledSub1))
	err = util.Concat(&marshalled, &marshalledSub0, 0)
	if err != nil {
		t.Errorf("Error Concatenating Marshalled JSONs! Err: %v\n", err)
	}

	err = util.Concat(&marshalled, &marshalledSub1, len(marshalledSub0))
	if err != nil {
		t.Errorf("Error Concatenating Marshalled JSONs! Err: %v\n", err)
	}

	fooeyAssert := FoobarInterface{}
	attachmentAssert, bodyStart, err := parseRequestAttachment(true, &marshalled)
	if err != nil {
		t.Errorf("Error Parsing TCP Request Attachment! Err: %v\n", err)
	} else if attachmentAssert.UserID != attachment.UserID ||
		attachmentAssert.Sig != attachment.Sig {
		t.Errorf("Expected %v does not match actual %v\n", attachment, attachmentAssert)
	}

	marshalledSlice := marshalled[bodyStart:]
	prefix := TCPRequestPrefix{NeedsSecurity: false, IsBase64Enc: false, IsJSON: true}
	err = parseBody(&fooeyAssert, prefix, &marshalledSlice)
	if err != nil {
		t.Errorf("Error Parsing Body! Err: %v\n", err)
	} else if fooey.Foo != fooeyAssert.Foo ||
		fooey.Bar != fooeyAssert.Bar ||
		fooey.Baz != fooeyAssert.Baz {
		t.Errorf("Expected %v does not match actual %v\n", attachment, attachmentAssert)
	}
}
