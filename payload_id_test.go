package raptorgo

import (
	"errors"
	"testing"
)

func TestPayloadIDRoundTrip(t *testing.T) {
	id, err := NewPayloadID(0xab, 0xcdef12)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := id.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0xab, 0xcd, 0xef, 0x12}
	if string(encoded) != string(want) {
		t.Fatalf("payload ID encoding = %x, want %x", encoded, want)
	}
	got, err := ParsePayloadID(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got != id {
		t.Fatalf("payload ID round trip = %+v, want %+v", got, id)
	}
}

func TestPayloadIDBoundaries(t *testing.T) {
	if _, err := NewPayloadID(255, MaxEncodingSymbolID); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPayloadID(0, MaxEncodingSymbolID+1); !errors.Is(err, ErrInvalidPayloadID) {
		t.Fatalf("oversized ESI returned %v", err)
	}
	invalid := PayloadID{EncodingSymbolID: MaxEncodingSymbolID + 1}
	if _, err := invalid.MarshalBinary(); !errors.Is(err, ErrInvalidPayloadID) {
		t.Fatalf("oversized direct payload ID marshal returned %v", err)
	}
	if _, err := ParsePayloadID([]byte{0, 1, 2}); !errors.Is(err, ErrInvalidPayloadID) {
		t.Fatalf("short payload ID returned %v", err)
	}
}
