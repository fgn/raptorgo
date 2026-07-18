package objtrace

import (
	"bytes"
	"strings"
	"testing"
)

func validTrace() []Event {
	blocks := int32(2)
	sbn, esi, symbols := int32(0), int32(1), int32(1)
	return []Event{
		{V: 1, Op: "init", Result: "ok", State: "receiving", BindingOK: true, Blocks: &blocks},
		{V: 1, Op: "add_wait", Result: "ok", State: "receiving", BindingOK: true, SBN: &sbn, ESI: &esi, Symbols: &symbols},
	}
}

func TestNDJSONRoundTripCanonical(t *testing.T) {
	want := validTrace()
	encoded, err := CanonicalNDJSON(want)
	if err != nil {
		t.Fatal(err)
	}
	const expected = "{\"v\":1,\"op\":\"init\",\"result\":\"ok\",\"state\":\"receiving\",\"completed\":0,\"verification\":0,\"delivery\":0,\"bindingOK\":true,\"blocks\":2}\n" +
		"{\"v\":1,\"op\":\"add_wait\",\"result\":\"ok\",\"state\":\"receiving\",\"completed\":0,\"verification\":0,\"delivery\":0,\"bindingOK\":true,\"sbn\":0,\"esi\":1,\"symbols\":1}\n"
	if string(encoded) != expected {
		t.Fatalf("canonical NDJSON mismatch:\n%s", encoded)
	}
	got, err := ReadNDJSON(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) || got[1].ESI == nil || *got[1].ESI != 1 {
		t.Fatalf("round trip = %#v", got)
	}
}

func TestFingerprintIgnoresPacketAttribution(t *testing.T) {
	left := validTrace()
	right := validTrace()
	*right[1].SBN = 1
	*right[1].ESI = 27
	*right[1].Symbols = 3
	leftDigest, err := Fingerprint(left)
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := Fingerprint(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest != rightDigest {
		t.Fatalf("attribution changed fingerprint: %x != %x", leftDigest, rightDigest)
	}
}

func TestValidationRejectsMalformedTraces(t *testing.T) {
	tests := []struct {
		name, input, contains string
	}{
		{"unknown-field", `{"v":1,"op":"init","result":"ok","state":"receiving","completed":0,"verification":0,"delivery":0,"bindingOK":true,"blocks":1,"extra":1}` + "\n", "unknown field"},
		{"unknown-version", `{"v":2,"op":"init","result":"ok","state":"receiving","completed":0,"verification":0,"delivery":0,"bindingOK":true,"blocks":1}` + "\n", "schema version"},
		{"unknown-op", `{"v":1,"op":"surprise","result":"ok","state":"receiving","completed":0,"verification":0,"delivery":0,"bindingOK":true,"blocks":1}` + "\n", "unknown operation"},
		{"bad-token", `{"v":1,"op":"init!","result":"ok","state":"receiving","completed":0,"verification":0,"delivery":0,"bindingOK":true,"blocks":1}` + "\n", "invalid operation token"},
		{"wrong-first", `{"v":1,"op":"add_wait","result":"ok","state":"receiving","completed":0,"verification":0,"delivery":0,"bindingOK":true,"blocks":1}` + "\n", "first operation"},
		{"missing-blocks", `{"v":1,"op":"init","result":"ok","state":"receiving","completed":0,"verification":0,"delivery":0,"bindingOK":true}` + "\n", "no blocks"},
		{"blank-line", "\n", "empty line"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ReadNDJSON(strings.NewReader(test.input))
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("error = %v, want substring %q", err, test.contains)
			}
		})
	}
}

func TestRenderTLAMatchesLegacyShape(t *testing.T) {
	got, err := RenderTLA("ObjectDecoderData", 2, validTrace())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"---------------- MODULE ObjectDecoderData ----------------",
		"ImplementationBlocks == 2",
		`[op |-> "init", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE]`,
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Fatalf("render missing %q:\n%s", want, got)
		}
	}
}
