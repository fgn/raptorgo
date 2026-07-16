package codec

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

func TestSparseConstraintMatchesDenseFigureFive(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		kPrime uint32
		isis   []uint32
	}{
		{10, []uint32{0, 1, 9, 10, math.MaxUint32}},
		{12, []uint32{0, 11, 12, 29, math.MaxUint32}},
		{49, []uint32{0, 48, 49, 66, math.MaxUint32}},
	} {
		denseBytes, err := ConstraintMatrixBackingBytes(test.kPrime, uint64(len(test.isis)))
		if err != nil {
			t.Fatal(err)
		}
		dense, err := BuildConstraintMatrix(test.kPrime, test.isis, denseBytes)
		if err != nil {
			t.Fatal(err)
		}
		sparseBytes, capacity, err := SparseConstraintMatrixBackingBytes(test.kPrime, test.isis)
		if err != nil {
			t.Fatal(err)
		}
		if sparse, err := BuildSparseConstraintMatrix(test.kPrime, test.isis, sparseBytes-1); !errors.Is(err, ErrConstraintBuild) || sparse != nil {
			t.Fatalf("K'=%d budget-1 sparse=%v error=%v", test.kPrime, sparse, err)
		}
		sparse, err := BuildSparseConstraintMatrix(test.kPrime, test.isis, sparseBytes)
		if err != nil {
			t.Fatal(err)
		}
		if sparse.MaxNonzeros() != capacity || sparse.BackingBytes() != sparseBytes {
			t.Fatalf("K'=%d sparse capacity/backing=%d/%d, want %d/%d", test.kPrime, sparse.MaxNonzeros(), sparse.BackingBytes(), capacity, sparseBytes)
		}
		for row := uint32(0); row < dense.Rows(); row++ {
			for column := uint32(0); column < dense.Columns(); column++ {
				want, err := dense.At(row, column)
				if err != nil {
					t.Fatal(err)
				}
				got, err := sparse.At(row, column)
				if err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Fatalf("K'=%d (%d,%d) sparse=%d dense=%d", test.kPrime, row, column, got, want)
				}
			}
		}
	}
}

func TestSparseSourceConstraintEstimatorAndMatrix(t *testing.T) {
	t.Parallel()

	for _, kPrime := range []uint32{10, 12, 18, 49} {
		parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
		if err != nil {
			t.Fatal(err)
		}
		bytes, capacity, err := SparseSourceConstraintMatrixBackingBytes(kPrime)
		if err != nil {
			t.Fatal(err)
		}
		if capacity == 0 || capacity > uint64(parameters.L())*uint64(parameters.L()) {
			t.Fatalf("K'=%d capacity=%d", kPrime, capacity)
		}
		result, err := BuildSparseSourceConstraintMatrix(kPrime, bytes)
		if err != nil {
			t.Fatal(err)
		}
		if result.Rows() != parameters.L() || result.Columns() != parameters.L() || result.BackingBytes() != bytes {
			t.Fatalf("K'=%d result=%dx%d/%d", kPrime, result.Rows(), result.Columns(), result.BackingBytes())
		}
	}
}

func TestSparseIntermediateMatchesDenseReference(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		k    uint32
		size uint32
	}{
		{1, 1},
		{1, 7},
		{10, 7},
		{11, 7},
		{13, 31},
		{49, 7},
	} {
		source := patternedSymbols(t, test.k, test.size)
		before := snapshotSymbols(t, source)
		sparseBudget, err := SparseIntermediateWorkspaceBackingBytes(test.k, test.size)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := GenerateIntermediateSymbolsSparse(source, sparseBudget-1); !errors.Is(err, ErrIntermediateWorkspace) || result != nil {
			t.Fatalf("K=%d sparse budget-1 result=%v error=%v", test.k, result, err)
		}
		got, err := GenerateIntermediateSymbolsSparse(source, sparseBudget)
		if err != nil {
			t.Fatalf("K=%d: %v", test.k, err)
		}
		denseBudget, err := IntermediateWorkspaceBackingBytes(test.k, test.size)
		if err != nil {
			t.Fatal(err)
		}
		want, err := GenerateIntermediateSymbols(source, denseBudget)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(snapshotSymbols(t, got), snapshotSymbols(t, want)) {
			t.Fatalf("K=%d sparse C=%x dense C=%x", test.k, snapshotSymbols(t, got), snapshotSymbols(t, want))
		}
		if !bytes.Equal(snapshotSymbols(t, source), before) {
			t.Fatalf("K=%d sparse generation mutated source", test.k)
		}
		parameters, err := rfc6330.ParametersForSourceSymbols(test.k)
		if err != nil {
			t.Fatal(err)
		}
		for _, isi := range []uint32{0, parameters.KPrime - 1, parameters.KPrime, math.MaxUint32} {
			sparseSymbol, err := EncodingSymbol(parameters.KPrime, isi, got, uint64(test.size))
			if err != nil {
				t.Fatal(err)
			}
			denseSymbol, err := EncodingSymbol(parameters.KPrime, isi, want, uint64(test.size))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(sparseSymbol, denseSymbol) {
				t.Fatalf("K=%d ISI=%d sparse=%x dense=%x", test.k, isi, sparseSymbol, denseSymbol)
			}
		}
	}
}

func TestSparseIntermediateAllZeroAndValidation(t *testing.T) {
	t.Parallel()

	source, err := gf256.NewSymbols(10, 5, 50)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := SparseIntermediateWorkspaceBackingBytes(10, 5)
	if err != nil {
		t.Fatal(err)
	}
	result, err := GenerateIntermediateSymbolsSparse(source, budget)
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshotSymbols(t, result); !bytes.Equal(got, make([]byte, len(got))) {
		t.Fatalf("zero source produced %x", got)
	}
	if _, err := SparseIntermediateWorkspaceBackingBytes(0, 1); !errors.Is(err, rfc6330.ErrInvalidSourceBlockSize) {
		t.Fatalf("zero K error=%v", err)
	}
	if _, err := SparseIntermediateWorkspaceBackingBytes(1, 0); !errors.Is(err, ErrSourceSymbolShape) {
		t.Fatalf("zero T error=%v", err)
	}
	if _, err := SparseIntermediateWorkspaceBackingBytes(MaxEagerSourceSymbols+1, 1); !errors.Is(err, ErrEagerScaleLimit) {
		t.Fatalf("oversize eager K error=%v", err)
	}
	if required, err := SourceBlockEncoderWorkspaceBackingBytes(MaxEagerSourceSymbols+1, 1); err != nil || required == 0 {
		t.Fatalf("production hybrid encoder boundary+1 backing=%d error=%v", required, err)
	}
}
