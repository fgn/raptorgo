package gf256

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

func TestSymbolsAllocationAndAccessBounds(t *testing.T) {
	t.Parallel()

	for _, dimensions := range [][2]uint32{{0, 0}, {0, 1}, {1, 0}} {
		if _, err := NewSymbols(dimensions[0], dimensions[1], 100); !errors.Is(err, ErrInvalidSymbolDimensions) {
			t.Errorf("NewSymbols(%d, %d) error = %v", dimensions[0], dimensions[1], err)
		}
	}
	if _, err := NewSymbols(10, 20, 199); !errors.Is(err, ErrSymbolLimitExceeded) {
		t.Fatalf("under-budget symbols error = %v", err)
	}
	symbols, err := NewSymbols(10, 20, 200)
	if err != nil {
		t.Fatal(err)
	}
	if symbols.Count() != 10 || symbols.Size() != 20 || symbols.Bytes() != 200 {
		t.Fatalf("symbol dimensions = %dx%d/%d bytes", symbols.Count(), symbols.Size(), symbols.Bytes())
	}
	if _, err := NewSymbols(math.MaxUint32, math.MaxUint32, math.MaxUint64); !errors.Is(err, ErrSymbolLimitExceeded) {
		t.Fatalf("platform-oversize symbols error = %v", err)
	}
	if _, err := symbols.Symbol(10); !errors.Is(err, ErrSymbolIndex) {
		t.Fatalf("out-of-range Symbol error = %v", err)
	}
}

func TestSetSymbolRejectsWithoutMutation(t *testing.T) {
	t.Parallel()

	symbols, err := NewSymbols(2, 4, 8)
	if err != nil {
		t.Fatal(err)
	}
	copy(symbols.values, []byte{1, 2, 3, 4, 5, 6, 7, 8})
	before := bytes.Clone(symbols.values)
	if err := symbols.SetSymbol(2, []byte{9, 9, 9, 9}); !errors.Is(err, ErrSymbolIndex) {
		t.Fatalf("index error = %v", err)
	}
	if !bytes.Equal(symbols.values, before) {
		t.Fatal("invalid index mutated symbols")
	}
	if err := symbols.SetSymbol(0, []byte{9, 9, 9}); !errors.Is(err, ErrSymbolSizeMismatch) {
		t.Fatalf("size error = %v", err)
	}
	if !bytes.Equal(symbols.values, before) {
		t.Fatal("invalid size mutated symbols")
	}
	if err := symbols.SetSymbol(1, []byte{9, 10, 11, 12}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(symbols.values, []byte{1, 2, 3, 4, 9, 10, 11, 12}) {
		t.Fatalf("symbols = %v", symbols.values)
	}
}

func TestSymbolViewCannotAppendIntoNextSymbol(t *testing.T) {
	t.Parallel()

	symbols, err := NewSymbols(2, 3, 6)
	if err != nil {
		t.Fatal(err)
	}
	symbol, err := symbols.Symbol(0)
	if err != nil {
		t.Fatal(err)
	}
	if cap(symbol) != len(symbol) {
		t.Fatalf("symbol capacity = %d, want %d", cap(symbol), len(symbol))
	}
}
