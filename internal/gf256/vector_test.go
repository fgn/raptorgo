package gf256

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

func TestXORAndMulAddKnownVectors(t *testing.T) {
	t.Parallel()

	destination := []byte{0x00, 0x01, 0x80, 0xff}
	if err := XOR(destination, []byte{0xff, 0x01, 0x80, 0x55}); err != nil {
		t.Fatal(err)
	}
	if want := []byte{0xff, 0x00, 0x00, 0xaa}; !bytes.Equal(destination, want) {
		t.Fatalf("XOR result = %x, want %x", destination, want)
	}

	destination = []byte{0x10, 0x20, 0x30, 0x40}
	source := []byte{0x01, 0x02, 0x03, 0x04}
	if err := MulAdd(destination, 0x80, source); err != nil {
		t.Fatal(err)
	}
	want := make([]byte, len(destination))
	for index := range want {
		want[index] = []byte{0x10, 0x20, 0x30, 0x40}[index] ^ polynomialMul(0x80, source[index])
	}
	if !bytes.Equal(destination, want) {
		t.Fatalf("MulAdd result = %x, want %x", destination, want)
	}
}

func TestMulAddMatchesIndependentPolynomialArithmeticExhaustive(t *testing.T) {
	t.Parallel()

	source := make([]byte, 256)
	for index := range source {
		source[index] = byte(index)
	}
	for coefficient := 0; coefficient <= math.MaxUint8; coefficient++ {
		destination := make([]byte, len(source))
		for index := range destination {
			destination[index] = byte(255 - index)
		}
		before := bytes.Clone(destination)
		if err := MulAdd(destination, byte(coefficient), source); err != nil {
			t.Fatal(err)
		}
		for index, got := range destination {
			want := before[index] ^ polynomialMul(byte(coefficient), source[index])
			if got != want {
				t.Fatalf("coefficient=%d source=%d: got %d, want %d", coefficient, source[index], got, want)
			}
		}
	}
}

func TestVectorOperationsExactAliasing(t *testing.T) {
	t.Parallel()

	values := []byte{0, 1, 2, 3, 127, 128, 255}
	if err := XOR(values, values); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(values, make([]byte, len(values))) {
		t.Fatalf("self-XOR = %x", values)
	}

	values = []byte{0, 1, 2, 3, 127, 128, 255}
	before := bytes.Clone(values)
	if err := MulAdd(values, 37, values); err != nil {
		t.Fatal(err)
	}
	for index, got := range values {
		want := before[index] ^ polynomialMul(37, before[index])
		if got != want {
			t.Fatalf("self-MulAdd index %d = %d, want %d", index, got, want)
		}
	}
}

func TestVectorOperationsPartialOverlap(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		destination func([]byte) []byte
		source      func([]byte) []byte
	}{
		{"destination-after-source", func(values []byte) []byte { return values[1:] }, func(values []byte) []byte { return values[:3] }},
		{"destination-before-source", func(values []byte) []byte { return values[:3] }, func(values []byte) []byte { return values[1:] }},
	} {
		for _, operation := range []struct {
			name string
			run  func([]byte, []byte) error
		}{
			{"XOR", XOR},
			{"MulAdd", func(dst, src []byte) error { return MulAdd(dst, 37, src) }},
		} {
			backing := []byte{1, 2, 3, 4}
			destination := test.destination(backing)
			source := test.source(backing)
			destinationBefore := bytes.Clone(destination)
			sourceBefore := bytes.Clone(source)
			if err := operation.run(destination, source); err != nil {
				t.Fatalf("%s/%s: %v", test.name, operation.name, err)
			}
			coefficient := byte(1)
			if operation.name == "MulAdd" {
				coefficient = 37
			}
			for index, got := range destination {
				want := destinationBefore[index] ^ polynomialMul(coefficient, sourceBefore[index])
				if got != want {
					t.Fatalf("%s/%s index %d = %d, want %d", test.name, operation.name, index, got, want)
				}
			}
		}
	}
}

func TestVectorLengthMismatchDoesNotMutate(t *testing.T) {
	t.Parallel()

	for _, operation := range []struct {
		name string
		run  func([]byte, []byte) error
	}{
		{"XOR", XOR},
		{"MulAdd", func(dst, src []byte) error { return MulAdd(dst, 99, src) }},
	} {
		destination := []byte{1, 2, 3}
		before := bytes.Clone(destination)
		if err := operation.run(destination, []byte{4, 5}); !errors.Is(err, ErrLengthMismatch) {
			t.Errorf("%s mismatch error = %v", operation.name, err)
		}
		if !bytes.Equal(destination, before) {
			t.Errorf("%s mutated destination on rejected input", operation.name)
		}
	}
}

func TestScaleAllCoefficientsAndValues(t *testing.T) {
	t.Parallel()

	for coefficient := 0; coefficient <= math.MaxUint8; coefficient++ {
		values := make([]byte, 256)
		for index := range values {
			values[index] = byte(index)
		}
		Scale(values, byte(coefficient))
		for index, got := range values {
			if want := polynomialMul(byte(coefficient), byte(index)); got != want {
				t.Fatalf("Scale coefficient=%d value=%d: got %d, want %d", coefficient, index, got, want)
			}
		}
	}
}

func FuzzVectorOperations(f *testing.F) {
	f.Add([]byte{0, 1, 2, 255}, []byte{255, 2, 1, 0}, uint8(37))
	f.Add([]byte{}, []byte{}, uint8(0))
	f.Fuzz(func(t *testing.T, destination, source []byte, coefficient uint8) {
		before := bytes.Clone(destination)
		sourceBefore := bytes.Clone(source)
		err := MulAdd(destination, coefficient, source)
		if len(destination) != len(source) {
			if !errors.Is(err, ErrLengthMismatch) || !bytes.Equal(destination, before) {
				t.Fatalf("rejected operation error=%v destination=%x before=%x", err, destination, before)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		for index, got := range destination {
			want := before[index] ^ polynomialMul(coefficient, sourceBefore[index])
			if got != want {
				t.Fatalf("index %d = %d, want %d", index, got, want)
			}
		}
	})
}
