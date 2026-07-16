// Package verifytrace records small abstract implementation traces for replay
// against the TLA+ verification models. It is development tooling, not a codec.
package verifytrace

import (
	"errors"
	"fmt"
	"io"
	"sort"
)

type Phase string

const (
	Collecting         Phase = "collecting"
	Decoded            Phase = "decoded"
	Verified           Phase = "verified"
	Delivered          Phase = "delivered"
	Aborted            Phase = "aborted"
	VerificationFailed Phase = "verification_failed"
)

type Event struct {
	Operation string
	ID        int
	Accepted  []int
	Phase     Phase
}

type Decoder struct {
	symbols  map[int]struct{}
	required int
	accepted map[int]struct{}
	phase    Phase
	events   []Event
}

func NewDecoder(symbolIDs []int, required int) (*Decoder, error) {
	if required < 1 || required > len(symbolIDs) {
		return nil, errors.New("verifytrace: invalid required symbol count")
	}
	symbols := make(map[int]struct{}, len(symbolIDs))
	for _, id := range symbolIDs {
		if id < 0 {
			return nil, errors.New("verifytrace: symbol IDs must be non-negative")
		}
		if _, duplicate := symbols[id]; duplicate {
			return nil, errors.New("verifytrace: duplicate symbol ID")
		}
		symbols[id] = struct{}{}
	}
	d := &Decoder{
		symbols:  symbols,
		required: required,
		accepted: make(map[int]struct{}),
		phase:    Collecting,
	}
	d.record("init", -1)
	return d, nil
}

func (d *Decoder) Accept(id int) error {
	if d.phase != Collecting {
		d.record("accept_phase_reject", id)
		return fmt.Errorf("verifytrace: cannot accept in phase %q", d.phase)
	}
	if _, valid := d.symbols[id]; !valid {
		d.record("reject", id)
		return nil
	}
	d.accepted[id] = struct{}{}
	d.record("accept", id)
	return nil
}

func (d *Decoder) Decode() error {
	if d.phase != Collecting {
		d.record("decode_reject", -1)
		return fmt.Errorf("verifytrace: cannot decode in phase %q", d.phase)
	}
	if len(d.accepted) < d.required {
		d.record("decode_reject", -1)
		return errors.New("verifytrace: decode is not ready")
	}
	d.phase = Decoded
	d.record("decode", -1)
	return nil
}

func (d *Decoder) Deliver() error {
	if d.phase != Verified {
		d.record("deliver_reject", -1)
		return fmt.Errorf("verifytrace: cannot deliver in phase %q", d.phase)
	}
	d.phase = Delivered
	d.record("deliver", -1)
	return nil
}

func (d *Decoder) Verify(ok bool) error {
	if d.phase != Decoded {
		d.record("verify_reject", -1)
		return fmt.Errorf("verifytrace: cannot verify in phase %q", d.phase)
	}
	if !ok {
		d.phase = VerificationFailed
		d.record("verify_fail", -1)
		return nil
	}
	d.phase = Verified
	d.record("verify", -1)
	return nil
}

func (d *Decoder) Abort() error {
	if d.phase != Collecting {
		d.record("abort_reject", -1)
		return fmt.Errorf("verifytrace: cannot abort in phase %q", d.phase)
	}
	d.phase = Aborted
	d.record("abort", -1)
	return nil
}

func (d *Decoder) Events() []Event {
	events := make([]Event, len(d.events))
	for i, event := range d.events {
		events[i] = event
		events[i].Accepted = append([]int(nil), event.Accepted...)
	}
	return events
}

// Observe records the projected state after a call boundary. Verification
// scenarios use it after rejected calls so mutations occurring after an error
// event but before method return cannot escape trace replay.
func (d *Decoder) Observe() {
	d.record("observe", -1)
}

func (d *Decoder) record(operation string, id int) {
	accepted := make([]int, 0, len(d.accepted))
	for symbolID := range d.accepted {
		accepted = append(accepted, symbolID)
	}
	sort.Ints(accepted)
	d.events = append(d.events, Event{
		Operation: operation,
		ID:        id,
		Accepted:  accepted,
		Phase:     d.phase,
	})
}

func (d *Decoder) WriteTLA(w io.Writer, module string) error {
	if module == "" {
		return errors.New("verifytrace: empty TLA+ module name")
	}
	symbolIDs := make([]int, 0, len(d.symbols))
	for id := range d.symbols {
		symbolIDs = append(symbolIDs, id)
	}
	sort.Ints(symbolIDs)

	if _, err := fmt.Fprintf(w, "-------------------- MODULE %s --------------------\n", module); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, "EXTENDS Integers, Sequences\n\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "ImplementationSymbolIds == %s\n", tlaSet(symbolIDs)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "ImplementationRequired == %d\n\n", d.required); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "ImplementationTrace == <<"); err != nil {
		return err
	}
	for i, event := range d.events {
		comma := ","
		if i == len(d.events)-1 {
			comma = ""
		}
		if _, err := fmt.Fprintf(w,
			"    [op |-> %q, id |-> %d, accepted |-> %s, phase |-> %q]%s\n",
			event.Operation, event.ID, tlaSet(event.Accepted), event.Phase, comma); err != nil {
			return err
		}
	}
	_, err := fmt.Fprint(w, ">>\n\n=====================================================================\n")
	return err
}

func tlaSet(values []int) string {
	if len(values) == 0 {
		return "{}"
	}
	result := "{"
	for i, value := range values {
		if i != 0 {
			result += ", "
		}
		result += fmt.Sprint(value)
	}
	return result + "}"
}
