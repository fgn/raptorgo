package verifytrace

import (
	"bytes"
	"strings"
	"testing"
)

func TestDecoderLifecycleInvariants(t *testing.T) {
	d, err := NewDecoder([]int{0, 1, 2}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Accept(0); err != nil {
		t.Fatal(err)
	}
	if err := d.Accept(0); err != nil { // Duplicate is idempotent.
		t.Fatal(err)
	}
	if err := d.Accept(99); err != nil { // Rejection is state-preserving.
		t.Fatal(err)
	}
	if err := d.Decode(); err == nil {
		t.Fatal("premature decode unexpectedly succeeded")
	}
	if err := d.Accept(2); err != nil {
		t.Fatal(err)
	}
	if err := d.Decode(); err != nil {
		t.Fatal(err)
	}
	if err := d.Deliver(); err == nil {
		t.Fatal("unverified decoder delivered")
	}
	if err := d.Verify(true); err != nil {
		t.Fatal(err)
	}
	if err := d.Deliver(); err != nil {
		t.Fatal(err)
	}
	if err := d.Accept(1); err == nil {
		t.Fatal("terminal state accepted another symbol")
	}

	events := d.Events()
	if len(events) != 11 {
		t.Fatalf("recorded %d events, want 11", len(events))
	}
	if got := events[2].Accepted; len(got) != 1 || got[0] != 0 {
		t.Fatalf("duplicate changed accepted symbols: %v", got)
	}
	if got := events[3].Accepted; len(got) != 1 || got[0] != 0 {
		t.Fatalf("rejection changed accepted symbols: %v", got)
	}
	if events[len(events)-1].Phase != Delivered {
		t.Fatalf("final phase is %q", events[len(events)-1].Phase)
	}
	if events[len(events)-1].Operation != "accept_phase_reject" {
		t.Fatalf("final operation is %q", events[len(events)-1].Operation)
	}
}

func TestWriteTLAIsDeterministic(t *testing.T) {
	d, err := NewDecoder([]int{2, 0, 1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Accept(2)
	_ = d.Accept(0)
	_ = d.Decode()
	_ = d.Verify(true)
	_ = d.Deliver()

	var first, second bytes.Buffer
	if err := d.WriteTLA(&first, "TraceData"); err != nil {
		t.Fatal(err)
	}
	if err := d.WriteTLA(&second, "TraceData"); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatal("TLA+ trace output is nondeterministic")
	}
	for _, expected := range []string{
		"ImplementationSymbolIds == {0, 1, 2}",
		`[op |-> "decode"`,
		`phase |-> "delivered"`,
	} {
		if !strings.Contains(first.String(), expected) {
			t.Errorf("trace does not contain %q", expected)
		}
	}
}

func TestVerificationFailureIsTerminal(t *testing.T) {
	d, err := NewDecoder([]int{0}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Accept(0); err != nil {
		t.Fatal(err)
	}
	if err := d.Decode(); err != nil {
		t.Fatal(err)
	}
	if err := d.Verify(false); err != nil {
		t.Fatal(err)
	}
	if err := d.Deliver(); err == nil {
		t.Fatal("verification-failed decoder delivered")
	}
	if got := d.Events()[len(d.Events())-1].Phase; got != VerificationFailed {
		t.Fatalf("final phase is %q", got)
	}
}

func TestRejectsInvalidConfiguration(t *testing.T) {
	for _, test := range []struct {
		ids      []int
		required int
	}{
		{[]int{0}, 0},
		{[]int{0}, 2},
		{[]int{-1}, 1},
		{[]int{1, 1}, 1},
	} {
		if _, err := NewDecoder(test.ids, test.required); err == nil {
			t.Fatalf("NewDecoder(%v, %d) unexpectedly succeeded", test.ids, test.required)
		}
	}
}

func TestAbortIsTerminal(t *testing.T) {
	d, err := NewDecoder([]int{0, 1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Accept(0); err != nil {
		t.Fatal(err)
	}
	if err := d.Abort(); err != nil {
		t.Fatal(err)
	}
	if err := d.Accept(1); err == nil {
		t.Fatal("aborted decoder accepted another symbol")
	}
	if err := d.Decode(); err == nil {
		t.Fatal("aborted decoder decoded")
	}
	if err := d.Deliver(); err == nil {
		t.Fatal("aborted decoder delivered")
	}
	events := d.Events()
	if events[len(events)-1].Phase != Aborted {
		t.Fatalf("final phase is %q", events[len(events)-1].Phase)
	}
}
