package codec

import (
	"errors"
	"math"
	"testing"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

func TestConstraintMatrixExactBudgetAndDimensions(t *testing.T) {
	t.Parallel()

	parameters, err := rfc6330.ParametersForExtendedSourceBlock(10)
	if err != nil {
		t.Fatal(err)
	}
	required, err := ConstraintMatrixBackingBytes(10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if required != uint64(parameters.L())*uint64(parameters.L()) {
		t.Fatalf("source matrix bytes = %d, want %d", required, uint64(parameters.L())*uint64(parameters.L()))
	}
	if matrix, err := BuildSourceConstraintMatrix(10, required-1); !errors.Is(err, gf256.ErrMatrixLimitExceeded) || matrix != nil {
		t.Fatalf("under-budget matrix = %v, %v", matrix, err)
	}
	matrix, err := BuildSourceConstraintMatrix(10, required)
	if err != nil {
		t.Fatal(err)
	}
	if matrix.Rows() != parameters.L() || matrix.Columns() != parameters.L() || matrix.Bytes() != required {
		t.Fatalf("matrix = %dx%d/%d bytes", matrix.Rows(), matrix.Columns(), matrix.Bytes())
	}
	if _, err := ConstraintMatrixBackingBytes(11, 1); !errors.Is(err, rfc6330.ErrInvalidExtendedBlockSize) {
		t.Fatalf("invalid K' error = %v", err)
	}
	if _, err := ConstraintMatrixBackingBytes(10, math.MaxUint64); !errors.Is(err, ErrConstraintDimensions) {
		t.Fatalf("row overflow error = %v", err)
	}
}

func TestPrecodeStructureAcrossSentinelParameterRows(t *testing.T) {
	for _, kPrime := range []uint32{10, 12, 48, 49, 1698, 5225, 8837, 50511, 56403} {
		parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
		if err != nil {
			t.Fatal(err)
		}
		required, err := ConstraintMatrixBackingBytes(parameters.KPrime, 0)
		if err != nil {
			t.Fatal(err)
		}
		matrix, err := BuildConstraintMatrix(parameters.KPrime, nil, required)
		if err != nil {
			t.Fatalf("K'=%d: %v", parameters.KPrime, err)
		}
		if matrix.Rows() != parameters.S+parameters.H || matrix.Columns() != parameters.L() {
			t.Fatalf("K'=%d dimensions = %dx%d", parameters.KPrime, matrix.Rows(), matrix.Columns())
		}

		for column := uint32(0); column < parameters.B(); column++ {
			a := 1 + column/parameters.S
			first := column % parameters.S
			second := (first + a) % parameters.S
			third := (second + a) % parameters.S
			if first == second || first == third || second == third {
				t.Fatalf("K'=%d G_LDPC,1 column %d repeats rows %d,%d,%d", parameters.KPrime, column, first, second, third)
			}
			for _, row := range []uint32{first, second, third} {
				if matrixValue(t, matrix, row, column) != 1 {
					t.Fatalf("K'=%d G_LDPC,1[%d,%d] != 1", parameters.KPrime, row, column)
				}
			}
		}
		for row := uint32(0); row < parameters.S; row++ {
			if matrixValue(t, matrix, row, parameters.B()+row) != 1 {
				t.Fatalf("K'=%d I_S[%d,%d] != 1", parameters.KPrime, row, parameters.B()+row)
			}
			first := parameters.W + row%parameters.P()
			second := parameters.W + (row+1)%parameters.P()
			if first == second || matrixValue(t, matrix, row, first) != 1 || matrixValue(t, matrix, row, second) != 1 {
				t.Fatalf("K'=%d G_LDPC,2 row %d is missing (%d,%d)", parameters.KPrime, row, first, second)
			}
		}
		nonHDPCColumns := parameters.KPrime + parameters.S
		for row := uint32(0); row < parameters.H; row++ {
			for column := uint32(0); column < parameters.H; column++ {
				want := byte(0)
				if row == column {
					want = 1
				}
				if got := matrixValue(t, matrix, parameters.S+row, nonHDPCColumns+column); got != want {
					t.Fatalf("K'=%d I_H[%d,%d] = %d, want %d", parameters.KPrime, row, column, got, want)
				}
			}
		}
	}
}

func TestConstraintDimensionsAcrossAllParameterRows(t *testing.T) {
	t.Parallel()

	previous := uint32(0)
	for k := uint32(1); k <= rfc6330.MaxSourceSymbolsPerBlock; k++ {
		parameters, err := rfc6330.ParametersForSourceSymbols(k)
		if err != nil {
			t.Fatal(err)
		}
		if parameters.KPrime == previous {
			continue
		}
		previous = parameters.KPrime
		precodeBytes, err := ConstraintMatrixBackingBytes(parameters.KPrime, 0)
		if err != nil {
			t.Fatalf("K'=%d precode bytes: %v", parameters.KPrime, err)
		}
		if want := uint64(parameters.S+parameters.H) * uint64(parameters.L()); precodeBytes != want {
			t.Fatalf("K'=%d precode bytes = %d, want %d", parameters.KPrime, precodeBytes, want)
		}
		sourceBytes, err := ConstraintMatrixBackingBytes(parameters.KPrime, uint64(parameters.KPrime))
		want := uint64(parameters.L()) * uint64(parameters.L())
		if want > uint64(^uint(0)>>1) {
			if !errors.Is(err, ErrConstraintDimensions) {
				t.Fatalf("K'=%d source bytes above platform limit = %d, error %v", parameters.KPrime, want, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("K'=%d source bytes: %v", parameters.KPrime, err)
		}
		if sourceBytes != want {
			t.Fatalf("K'=%d source bytes = %d, want %d", parameters.KPrime, sourceBytes, want)
		}
	}
}

func TestHDPCRecurrenceMatchesLiteralProduct(t *testing.T) {
	t.Parallel()

	for _, kPrime := range []uint32{10, 49, 1698, 8837} {
		parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
		if err != nil {
			t.Fatal(err)
		}
		required, err := ConstraintMatrixBackingBytes(kPrime, 0)
		if err != nil {
			t.Fatal(err)
		}
		matrix, err := BuildConstraintMatrix(kPrime, nil, required)
		if err != nil {
			t.Fatal(err)
		}
		n := parameters.KPrime + parameters.S
		columns := []uint32{0, 1, n / 2, n - 2, n - 1}
		for row := uint32(0); row < parameters.H; row++ {
			for _, column := range columns {
				got := matrixValue(t, matrix, parameters.S+row, column)
				want := literalHDPCEntry(t, parameters, row, column)
				if got != want {
					t.Fatalf("K'=%d G_HDPC[%d,%d] = %d, literal MT*GAMMA gives %d", kPrime, row, column, got, want)
				}
			}
		}
	}
}

func TestGEncRowsEqualVisitEncodingIndexParity(t *testing.T) {
	t.Parallel()

	for _, kPrime := range []uint32{10, 49, 5225, 56403} {
		parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
		if err != nil {
			t.Fatal(err)
		}
		isis := []uint32{0, kPrime - 1, kPrime, math.MaxUint32}
		required, err := ConstraintMatrixBackingBytes(kPrime, uint64(len(isis)))
		if err != nil {
			t.Fatal(err)
		}
		matrix, err := BuildConstraintMatrix(kPrime, isis, required)
		if err != nil {
			t.Fatal(err)
		}
		for index, isi := range isis {
			parity := make([]byte, parameters.L())
			if err := rfc6330.VisitEncodingIndices(kPrime, isi, func(column uint32) error {
				parity[column] ^= 1
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			row := parameters.S + parameters.H + uint32(index)
			for column, want := range parity {
				if got := matrixValue(t, matrix, row, uint32(column)); got != want {
					t.Fatalf("K'=%d ISI=%d column=%d coefficient=%d, want parity %d", kPrime, isi, column, got, want)
				}
			}
		}
	}
}

func literalHDPCEntry(t *testing.T, parameters rfc6330.Parameters, row, column uint32) byte {
	t.Helper()
	n := parameters.KPrime + parameters.S
	var result byte
	for mtColumn := column; mtColumn < n; mtColumn++ {
		var mt byte
		if mtColumn == n-1 {
			mt = alphaPowerReference(row)
		} else {
			first, err := rfc6330.Rand(mtColumn+1, 6, parameters.H)
			if err != nil {
				t.Fatal(err)
			}
			offset, err := rfc6330.Rand(mtColumn+1, 7, parameters.H-1)
			if err != nil {
				t.Fatal(err)
			}
			second := (first + offset + 1) % parameters.H
			if row == first || row == second {
				mt = 1
			}
		}
		result ^= polynomialMultiply(mt, alphaPowerReference(mtColumn-column))
	}
	return result
}

func alphaPowerReference(exponent uint32) byte {
	result := byte(1)
	for count := uint32(0); count < exponent%255; count++ {
		result = polynomialMultiply(result, 2)
	}
	return result
}

func polynomialMultiply(a, b byte) byte {
	var product byte
	for b != 0 {
		if b&1 != 0 {
			product ^= a
		}
		high := a&0x80 != 0
		a <<= 1
		if high {
			a ^= 0x1d
		}
		b >>= 1
	}
	return product
}

func matrixValue(t *testing.T, matrix *gf256.Matrix, row, column uint32) byte {
	t.Helper()
	value, err := matrix.At(row, column)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
