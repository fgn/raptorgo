package objtrace

import (
	"errors"
	"testing"

	"github.com/fgn/raptorgo"
)

func finalizationFixture(t *testing.T) (raptorgo.OTI, []byte, [][]byte) {
	t.Helper()
	oti := raptorgo.OTI{
		TransferLength: 13, SymbolSize: 4, SourceBlocks: 2, SubBlocks: 2, SymbolAlignment: 1,
	}
	object := []byte{3, 19, 41, 67, 101, 149, 211, 17, 53, 109, 181, 13, 97}
	encoder, err := raptorgo.NewObjectEncoder(object, oti, raptorgo.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	packets := make([][]byte, 0, 4)
	for sbn := uint8(0); sbn < oti.SourceBlocks; sbn++ {
		for esi := uint32(0); esi < 2; esi++ {
			packet, packetErr := encoder.Packet(sbn, esi, 1, true)
			if packetErr != nil {
				t.Fatal(packetErr)
			}
			packets = append(packets, packet)
		}
	}
	return oti, object, packets
}

func newRecordedDecoder(
	t *testing.T,
	oti raptorgo.OTI,
	object []byte,
	limits raptorgo.Limits,
	verificationError, deliveryError error,
) (*Recorder, *raptorgo.ObjectDecoder, error) {
	t.Helper()
	recorder := NewRecorder(oti, "recorder-test", object)
	decoder, err := raptorgo.NewObjectDecoder(
		oti, limits, "recorder-test", recorder.Verifier(verificationError), recorder.Deliverer(deliveryError),
	)
	if decoder != nil {
		recorder.BindDecoder(decoder)
	}
	return recorder, decoder, err
}

func TestRecorderFinalizationAndTerminalOrdering(t *testing.T) {
	oti, object, packets := finalizationFixture(t)
	recorder, _, err := newRecordedDecoder(t, oti, object, raptorgo.DefaultLimits(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, packet := range packets {
		if _, addErr := recorder.AddPacket(packet); addErr != nil {
			t.Fatal(addErr)
		}
	}
	if _, addErr := recorder.AddPacket(packets[0]); !errors.Is(addErr, raptorgo.ErrObjectTerminal) {
		t.Fatalf("terminal add error = %v", addErr)
	}

	events := recorder.Events()
	wantOps := []string{
		"init", "add_wait", "block_complete", "add_wait", "verify_enter",
		"verified", "deliver_enter", "add_return", "terminal_add",
	}
	if len(events) != len(wantOps) {
		t.Fatalf("events = %#v", events)
	}
	for index, want := range wantOps {
		if events[index].Op != want {
			t.Fatalf("event %d op = %q, want %q", index, events[index].Op, want)
		}
	}
	if events[4].State != "decoded" || events[4].Completed != 2 || events[4].Verification != 1 {
		t.Fatalf("verify-enter = %#v", events[4])
	}
	if events[6].State != "verified" || events[6].Delivery != 1 {
		t.Fatalf("deliver-enter = %#v", events[6])
	}
	if events[7].State != "delivered" || events[7].Result != "ok" {
		t.Fatalf("add-return = %#v", events[7])
	}
	if events[1].SBN == nil || events[1].ESI == nil || events[1].Symbols == nil {
		t.Fatalf("packet attribution missing: %#v", events[1])
	}
}

func TestRecorderClassifiesReceivingRejections(t *testing.T) {
	oti, object, packets := finalizationFixture(t)

	t.Run("invalid", func(t *testing.T) {
		recorder, _, err := newRecordedDecoder(t, oti, object, raptorgo.DefaultLimits(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, addErr := recorder.AddPacket([]byte{0}); !errors.Is(addErr, raptorgo.ErrInvalidPacket) {
			t.Fatalf("error = %v", addErr)
		}
		last := recorder.Events()[1]
		if last.Op != "add_reject_invalid" || last.Result != "invalid" || last.State != "receiving" {
			t.Fatalf("event = %#v", last)
		}
	})

	t.Run("limit", func(t *testing.T) {
		limits := raptorgo.DefaultLimits()
		limits.MaxPacketBytes = raptorgo.PayloadIDEncodedSize + 1
		recorder, _, err := newRecordedDecoder(t, oti, object, limits, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, addErr := recorder.AddPacket(packets[0]); !errors.Is(addErr, raptorgo.ErrLimitExceeded) {
			t.Fatalf("error = %v", addErr)
		}
		last := recorder.Events()[1]
		if last.Op != "add_reject_limit" || last.Result != "limit" {
			t.Fatalf("event = %#v", last)
		}
	})

	t.Run("conflict", func(t *testing.T) {
		recorder, _, err := newRecordedDecoder(t, oti, object, raptorgo.DefaultLimits(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, addErr := recorder.AddPacket(packets[0]); addErr != nil {
			t.Fatal(addErr)
		}
		conflict := append([]byte(nil), packets[0]...)
		conflict[len(conflict)-1] ^= 1
		if _, addErr := recorder.AddPacket(conflict); !errors.Is(addErr, raptorgo.ErrObjectDecoder) {
			t.Fatalf("error = %v", addErr)
		}
		last := recorder.Events()[2]
		if last.Op != "add_reject_conflict" || last.Result != "conflict" || last.State != "receiving" {
			t.Fatalf("event = %#v", last)
		}
	})
}

func TestRecorderConstructionFinalization(t *testing.T) {
	oti := raptorgo.OTI{TransferLength: 0, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	injected := errors.New("injected")
	tests := []struct {
		name                   string
		verification, delivery error
		state                  string
		result                 string
		verificationCount      uint32
		deliveryCount          uint32
	}{
		{"success", nil, nil, "delivered", "ok", 1, 1},
		{"verification-failure", injected, nil, "verification-failed", "verification_error", 1, 0},
		{"delivery-failure", nil, injected, "delivery-failed", "delivery_error", 1, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder, decoder, err := newRecordedDecoder(
				t, oti, nil, raptorgo.DefaultLimits(), test.verification, test.delivery,
			)
			if decoder == nil {
				t.Fatal("constructor returned nil decoder")
			}
			if test.result == "ok" && err != nil {
				t.Fatal(err)
			}
			events := recorder.Events()
			if len(events) != 1 {
				t.Fatalf("events = %#v", events)
			}
			event := events[0]
			if event.Op != "construct_final" || event.State != test.state || event.Result != test.result ||
				event.Verification != test.verificationCount || event.Delivery != test.deliveryCount || !event.BindingOK {
				t.Fatalf("event = %#v", event)
			}
		})
	}
}

func TestRecorderReentrantTerminalEventIsInCallbackOrder(t *testing.T) {
	oti, object, packets := finalizationFixture(t)
	recorder := NewRecorder(oti, "reentrant", object)
	recorderVerifier := recorder.VerifierFunc(func(raptorgo.VerificationRequest) error {
		state, err := recorder.AddPacket(packets[0])
		if state != raptorgo.ObjectDecoded || !errors.Is(err, raptorgo.ErrObjectTerminal) {
			t.Fatalf("reentrant add state=%s error=%v", state, err)
		}
		return nil
	})
	decoder, err := raptorgo.NewObjectDecoder(
		oti, raptorgo.DefaultLimits(), "reentrant", recorderVerifier, recorder.Deliverer(nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	recorder.BindDecoder(decoder)
	for _, packet := range packets {
		if _, addErr := recorder.AddPacket(packet); addErr != nil {
			t.Fatal(addErr)
		}
	}
	events := recorder.Events()
	var callbackOps []string
	for _, event := range events {
		if event.Op == "verify_enter" || event.Op == "terminal_add" || event.Op == "verified" {
			callbackOps = append(callbackOps, event.Op)
		}
	}
	want := []string{"verify_enter", "terminal_add", "verified"}
	if len(callbackOps) != len(want) {
		t.Fatalf("callback ops = %v", callbackOps)
	}
	for index := range want {
		if callbackOps[index] != want[index] {
			t.Fatalf("callback ops = %v, want %v", callbackOps, want)
		}
	}
}

func TestEventsReturnsDeepCopy(t *testing.T) {
	oti, object, _ := finalizationFixture(t)
	recorder, _, err := newRecordedDecoder(t, oti, object, raptorgo.DefaultLimits(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	copy := recorder.Events()
	*copy[0].Blocks = 99
	copy[0].Op = "corrupt"
	again := recorder.Events()
	if *again[0].Blocks != 2 || again[0].Op != "init" {
		t.Fatalf("recorder storage was mutated: %#v", again[0])
	}
}
