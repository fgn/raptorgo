package codec

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

func TestGenerateIntermediateSymbolsExactBudgetAndEquations(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		k    uint32
		size uint32
	}{
		{1, 1},
		{1, 7},
		{10, 7},
		{11, 7},
	} {
		source := patternedSymbols(t, test.k, test.size)
		sourceBefore := snapshotSymbols(t, source)
		required, err := IntermediateWorkspaceBackingBytes(test.k, test.size)
		if err != nil {
			t.Fatal(err)
		}
		if intermediate, err := GenerateIntermediateSymbols(source, required-1); !errors.Is(err, ErrIntermediateWorkspace) || intermediate != nil {
			t.Fatalf("K=%d under-budget result = %v, %v", test.k, intermediate, err)
		}
		if !bytes.Equal(snapshotSymbols(t, source), sourceBefore) {
			t.Fatalf("K=%d under-budget generation mutated source", test.k)
		}
		intermediate, err := GenerateIntermediateSymbols(source, required)
		if err != nil {
			t.Fatalf("K=%d: %v", test.k, err)
		}
		parameters, err := rfc6330.ParametersForSourceSymbols(test.k)
		if err != nil {
			t.Fatal(err)
		}
		if intermediate.Count() != parameters.L() || intermediate.Size() != test.size {
			t.Fatalf("K=%d C shape = %dx%d, want %dx%d", test.k, intermediate.Count(), intermediate.Size(), parameters.L(), test.size)
		}
		if !bytes.Equal(snapshotSymbols(t, source), sourceBefore) {
			t.Fatalf("K=%d generation mutated source", test.k)
		}
		assertIntermediateEquations(t, source, parameters, intermediate)
	}
}

func TestGenerateIntermediateSymbolsAllZeroIsAllZero(t *testing.T) {
	t.Parallel()

	source, err := gf256.NewSymbols(10, 17, 170)
	if err != nil {
		t.Fatal(err)
	}
	required, err := IntermediateWorkspaceBackingBytes(10, 17)
	if err != nil {
		t.Fatal(err)
	}
	intermediate, err := GenerateIntermediateSymbols(source, required)
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshotSymbols(t, intermediate); !bytes.Equal(got, make([]byte, len(got))) {
		t.Fatalf("zero source produced nonzero C: %x", got)
	}
	for _, isi := range []uint32{0, 9, 10, math.MaxUint32} {
		encoded, err := EncodingSymbol(10, isi, intermediate, 17)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(encoded, make([]byte, 17)) {
			t.Fatalf("zero C produced nonzero ISI %d: %x", isi, encoded)
		}
	}
}

func TestEncodingSymbolValidationBudgetAndOwnership(t *testing.T) {
	t.Parallel()

	parameters, err := rfc6330.ParametersForExtendedSourceBlock(10)
	if err != nil {
		t.Fatal(err)
	}
	intermediate := patternedSymbols(t, parameters.L(), 4)
	before := snapshotSymbols(t, intermediate)
	if encoded, err := EncodingSymbol(10, 0, intermediate, 3); !errors.Is(err, ErrEncodedSymbolLimit) || encoded != nil {
		t.Fatalf("under-budget Enc = %v, %v", encoded, err)
	}
	wrong := patternedSymbols(t, parameters.L()-1, 4)
	if encoded, err := EncodingSymbol(10, 0, wrong, 4); !errors.Is(err, ErrIntermediateSymbolShape) || encoded != nil {
		t.Fatalf("wrong-shape Enc = %v, %v", encoded, err)
	}
	if encoded, err := EncodingSymbol(11, 0, intermediate, 4); !errors.Is(err, rfc6330.ErrInvalidExtendedBlockSize) || encoded != nil {
		t.Fatalf("invalid-K' Enc = %v, %v", encoded, err)
	}
	encoded, err := EncodingSymbol(10, math.MaxUint32, intermediate, 4)
	if err != nil {
		t.Fatal(err)
	}
	encoded[0] ^= 0xff
	if !bytes.Equal(snapshotSymbols(t, intermediate), before) {
		t.Fatal("encoded result aliases or mutates C")
	}
}

func TestIntermediateWorkspaceFormulaAndValidation(t *testing.T) {
	t.Parallel()

	parameters, err := rfc6330.ParametersForSourceSymbols(1)
	if err != nil {
		t.Fatal(err)
	}
	matrixBytes := uint64(parameters.L()) * uint64(parameters.L())
	symbolBytes := uint64(parameters.L()) * 7
	want := 2*matrixBytes + 3*symbolBytes
	got, err := IntermediateWorkspaceBackingBytes(1, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("workspace = %d, want %d", got, want)
	}
	if _, err := IntermediateWorkspaceBackingBytes(0, 7); !errors.Is(err, rfc6330.ErrInvalidSourceBlockSize) {
		t.Fatalf("zero K error = %v", err)
	}
	if _, err := IntermediateWorkspaceBackingBytes(1, 0); !errors.Is(err, ErrSourceSymbolShape) {
		t.Fatalf("zero size error = %v", err)
	}
	if result, err := GenerateIntermediateSymbols(nil, math.MaxUint64); !errors.Is(err, ErrSourceSymbolShape) || result != nil {
		t.Fatalf("nil source result = %v, %v", result, err)
	}
}

func assertIntermediateEquations(t *testing.T, source *gf256.Symbols, parameters rfc6330.Parameters, intermediate *gf256.Symbols) {
	t.Helper()
	matrixBytes, err := ConstraintMatrixBackingBytes(parameters.KPrime, uint64(parameters.KPrime))
	if err != nil {
		t.Fatal(err)
	}
	constraints, err := BuildSourceConstraintMatrix(parameters.KPrime, matrixBytes)
	if err != nil {
		t.Fatal(err)
	}
	firstSourceRow := parameters.S + parameters.H
	for row := uint32(0); row < parameters.L(); row++ {
		got := make([]byte, source.Size())
		for column := uint32(0); column < parameters.L(); column++ {
			coefficient := matrixValue(t, constraints, row, column)
			symbol, err := intermediate.Symbol(column)
			if err != nil {
				t.Fatal(err)
			}
			for index, value := range symbol {
				got[index] ^= polynomialMultiply(coefficient, value)
			}
		}
		want := make([]byte, source.Size())
		if row >= firstSourceRow && row < firstSourceRow+source.Count() {
			symbol, err := source.Symbol(row - firstSourceRow)
			if err != nil {
				t.Fatal(err)
			}
			copy(want, symbol)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("constraint row %d: A*C=%x, D=%x", row, got, want)
		}
	}
	for isi := uint32(0); isi < parameters.KPrime; isi++ {
		encoded, err := EncodingSymbol(parameters.KPrime, isi, intermediate, uint64(source.Size()))
		if err != nil {
			t.Fatal(err)
		}
		want := make([]byte, source.Size())
		if isi < source.Count() {
			symbol, err := source.Symbol(isi)
			if err != nil {
				t.Fatal(err)
			}
			copy(want, symbol)
		}
		if !bytes.Equal(encoded, want) {
			t.Fatalf("source/padding ISI %d = %x, want %x", isi, encoded, want)
		}
	}
}

func patternedSymbols(t *testing.T, count, size uint32) *gf256.Symbols {
	t.Helper()
	symbols, err := gf256.NewSymbols(count, size, uint64(count)*uint64(size))
	if err != nil {
		t.Fatal(err)
	}
	for symbol := uint32(0); symbol < count; symbol++ {
		values, err := symbols.Symbol(symbol)
		if err != nil {
			t.Fatal(err)
		}
		for index := range values {
			values[index] = byte(uint64(symbol)*131 + uint64(index)*37 + 19)
		}
	}
	return symbols
}

func snapshotSymbols(t *testing.T, symbols *gf256.Symbols) []byte {
	t.Helper()
	values := make([]byte, 0, uint64(symbols.Count())*uint64(symbols.Size()))
	for index := uint32(0); index < symbols.Count(); index++ {
		symbol, err := symbols.Symbol(index)
		if err != nil {
			t.Fatal(err)
		}
		values = append(values, symbol...)
	}
	return values
}
