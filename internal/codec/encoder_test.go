package codec

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/fgn/raptorgo/internal/rfc6330"
)

func TestSourceBlockEncoderSystematicAndRepairSymbols(t *testing.T) {
	t.Parallel()

	for _, k := range []uint32{1, 10, 11} {
		source := patternedSymbols(t, k, 7)
		original := snapshotSymbols(t, source)
		required, err := SourceBlockEncoderWorkspaceBackingBytes(k, 7)
		if err != nil {
			t.Fatal(err)
		}
		if encoder, err := NewSourceBlockEncoder(source, required-1); !errors.Is(err, ErrEncoderWorkspace) || encoder != nil {
			t.Fatalf("K=%d under-budget encoder = %v, %v", k, encoder, err)
		}
		encoder, err := NewSourceBlockEncoder(source, required)
		if err != nil {
			t.Fatalf("K=%d: %v", k, err)
		}
		if encoder.SourceSymbols() != k || encoder.SymbolSize() != 7 {
			t.Fatalf("K=%d metadata = K=%d T=%d", k, encoder.SourceSymbols(), encoder.SymbolSize())
		}
		for esi := uint32(0); esi < k; esi++ {
			got, err := encoder.Symbol(esi, 7)
			if err != nil {
				t.Fatal(err)
			}
			want, err := source.Symbol(esi)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("K=%d source ESI %d = %x, want %x", k, esi, got, want)
			}
		}
		parameters, err := rfc6330.ParametersForSourceSymbols(k)
		if err != nil {
			t.Fatal(err)
		}
		repairIDs := []uint32{k, k + 1, rfc6330.MaxEncodingSymbolID}
		repairBefore := make(map[uint32][]byte, len(repairIDs))
		for _, esi := range repairIDs {
			got, err := encoder.Symbol(esi, 7)
			if err != nil {
				t.Fatal(err)
			}
			isi, err := rfc6330.InternalSymbolID(k, esi)
			if err != nil {
				t.Fatal(err)
			}
			want, err := EncodingSymbol(parameters.KPrime, isi, encoder.intermediate, 7)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("K=%d repair ESI %d/ISI %d = %x, want %x", k, esi, isi, got, want)
			}
			repairBefore[esi] = append([]byte(nil), got...)
		}
		// Construction cloned the caller's source slab.
		for index := uint32(0); index < source.Count(); index++ {
			values, err := source.Symbol(index)
			if err != nil {
				t.Fatal(err)
			}
			for offset := range values {
				values[offset] ^= byte(index + uint32(offset) + 1)
			}
		}
		got, err := encoder.Symbol(0, 7)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, original[:7]) {
			t.Fatal("caller mutation changed immutable encoder output")
		}
		for _, esi := range repairIDs {
			got, err := encoder.Symbol(esi, 7)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, repairBefore[esi]) {
				t.Fatalf("K=%d caller mutation changed repair ESI %d", k, esi)
			}
			got[0] ^= 0xff
			again, err := encoder.Symbol(esi, 7)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(again, repairBefore[esi]) {
				t.Fatalf("K=%d returned repair ESI %d aliases encoder", k, esi)
			}
		}
	}
}

func TestSourceBlockEncoderConcurrentSymbols(t *testing.T) {
	t.Parallel()

	const k, size = uint32(13), uint32(31)
	source := patternedSymbols(t, k, size)
	required, err := SourceBlockEncoderWorkspaceBackingBytes(k, size)
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := NewSourceBlockEncoder(source, required)
	if err != nil {
		t.Fatal(err)
	}
	ids := []uint32{0, k - 1, k, k + 1, k + 17, rfc6330.MaxEncodingSymbolID}
	want := make(map[uint32][]byte, len(ids))
	for _, esi := range ids {
		want[esi], err = encoder.Symbol(esi, uint64(size))
		if err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	errorsFound := make(chan string, 32)
	var wait sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for iteration := 0; iteration < 100; iteration++ {
				for _, esi := range ids {
					got, callErr := encoder.Symbol(esi, uint64(size))
					if callErr != nil {
						errorsFound <- callErr.Error()
						return
					}
					if !bytes.Equal(got, want[esi]) {
						errorsFound <- "concurrent symbol mismatch"
						return
					}
				}
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errorsFound)
	for message := range errorsFound {
		t.Fatal(message)
	}
}

func TestSourceBlockEncoderSymbolValidationAndOwnership(t *testing.T) {
	t.Parallel()

	source := patternedSymbols(t, 1, 4)
	required, err := SourceBlockEncoderWorkspaceBackingBytes(1, 4)
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := NewSourceBlockEncoder(source, required)
	if err != nil {
		t.Fatal(err)
	}
	if symbol, err := encoder.Symbol(0, 3); !errors.Is(err, ErrEncodedSymbolLimit) || symbol != nil {
		t.Fatalf("under-budget symbol = %v, %v", symbol, err)
	}
	if symbol, err := encoder.Symbol(rfc6330.MaxEncodingSymbolID+1, 4); !errors.Is(err, ErrEncoderSymbolID) || symbol != nil {
		t.Fatalf("oversize ESI symbol = %v, %v", symbol, err)
	}
	first, err := encoder.Symbol(0, 4)
	if err != nil {
		t.Fatal(err)
	}
	second, err := encoder.Symbol(0, 4)
	if err != nil {
		t.Fatal(err)
	}
	first[0] ^= 0xff
	if bytes.Equal(first, second) {
		t.Fatal("source results alias each other")
	}
	var nilEncoder *SourceBlockEncoder
	if symbol, err := nilEncoder.Symbol(0, 4); !errors.Is(err, ErrSourceSymbolShape) || symbol != nil {
		t.Fatalf("nil encoder symbol = %v, %v", symbol, err)
	}
}
