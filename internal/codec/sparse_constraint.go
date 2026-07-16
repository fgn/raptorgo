package codec

import (
	"fmt"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/matrix"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

// SparseConstraintMatrixBackingBytes returns the retained sparse-octet bytes
// and fixed nonzero capacity for Figure 5 with the supplied encoded rows. The
// capacity includes a fixed rows*P allowance for inactivation fill-in. This is
// a bounded reference-path cap, not a proof that every RFC receive set fits.
func SparseConstraintMatrixBackingBytes(kPrime uint32, isis []uint32) (uint64, uint64, error) {
	return sparseConstraintMatrixBackingBytes(kPrime, uint64(len(isis)), func(index uint32) uint32 {
		return isis[index]
	})
}

// SparseSourceConstraintMatrixBackingBytes estimates the square source
// constraint matrix without materializing an ISI slice.
func SparseSourceConstraintMatrixBackingBytes(kPrime uint32) (uint64, uint64, error) {
	return sparseConstraintMatrixBackingBytes(kPrime, uint64(kPrime), func(index uint32) uint32 {
		return index
	})
}

// SparseConstraintWorstCaseBackingBytes returns a conservative initial-entry
// bound plus the reference path's fixed fill allowance for encodedRowCount
// arbitrary valid ISIs. Every Enc row is charged the RFC maximum of 30 LT plus
// 3 PI visits. It does not prove that all elimination schedules fit the cap.
func SparseConstraintWorstCaseBackingBytes(kPrime, encodedRowCount uint32) (uint64, uint64, error) {
	parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
	if err != nil {
		return 0, 0, err
	}
	baseRows := uint64(parameters.S) + uint64(parameters.H)
	if uint64(encodedRowCount) > uint64(^uint32(0))-baseRows {
		return 0, 0, fmt.Errorf("%w: %d base rows plus %d encoded rows", ErrConstraintDimensions, baseRows, encodedRowCount)
	}
	rows := baseRows + uint64(encodedRowCount)
	nonzeros := uint64(3)*uint64(parameters.B()) + uint64(3)*uint64(parameters.S)
	hdpcUpper := uint64(parameters.H) * (uint64(parameters.KPrime) + uint64(parameters.S) + 1)
	var overflow bool
	for _, addition := range []uint64{hdpcUpper, uint64(encodedRowCount) * 33, rows * uint64(parameters.P())} {
		nonzeros, overflow = addCapacity(nonzeros, addition)
		if overflow {
			return 0, 0, fmt.Errorf("%w: worst-case sparse capacity overflow", ErrConstraintDimensions)
		}
	}
	maximum := rows * uint64(parameters.L())
	if nonzeros > maximum {
		nonzeros = maximum
	}
	bytes, err := matrix.SparseOctetBackingBytes(uint32(rows), parameters.L(), nonzeros)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: worst-case sparse backing: %w", ErrConstraintDimensions, err)
	}
	return bytes, nonzeros, nil
}

// BuildSparseConstraintMatrix builds the Figure 5 matrix in bounded sparse
// GF(256) form. Rows preserve Figure 5 order, including HDPC rows before G_ENC.
func BuildSparseConstraintMatrix(kPrime uint32, isis []uint32, maxBytes uint64) (*matrix.SparseOctet, error) {
	return buildSparseConstraintMatrix(kPrime, uint64(len(isis)), func(index uint32) uint32 {
		return isis[index]
	}, maxBytes)
}

// BuildSparseSourceConstraintMatrix builds the square encoder matrix whose
// encoded rows are ISIs [0,K') without allocating a temporary ISI slice.
func BuildSparseSourceConstraintMatrix(kPrime uint32, maxBytes uint64) (*matrix.SparseOctet, error) {
	return buildSparseConstraintMatrix(kPrime, uint64(kPrime), func(index uint32) uint32 {
		return index
	}, maxBytes)
}

func sparseConstraintMatrixBackingBytes(kPrime uint32, isiCount uint64, isiAt func(uint32) uint32) (uint64, uint64, error) {
	parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
	if err != nil {
		return 0, 0, err
	}
	baseRows := uint64(parameters.S) + uint64(parameters.H)
	if isiCount > uint64(^uint32(0))-baseRows {
		return 0, 0, fmt.Errorf("%w: %d base rows plus %d encoded rows", ErrConstraintDimensions, baseRows, isiCount)
	}
	rows := baseRows + isiCount
	columns := uint64(parameters.L())

	// 3*B in G_LDPC,1; S in I_S; 2*S in G_LDPC,2. HDPC
	// reserves every possible G_HDPC coefficient plus I_H.
	nonzeros := uint64(3)*uint64(parameters.B()) + uint64(3)*uint64(parameters.S)
	hdpcUpper := uint64(parameters.H) * (uint64(parameters.KPrime) + uint64(parameters.S) + 1)
	var overflow bool
	nonzeros, overflow = addCapacity(nonzeros, hdpcUpper)
	if overflow {
		return 0, 0, fmt.Errorf("%w: sparse precode capacity overflow", ErrConstraintDimensions)
	}
	for index := uint32(0); uint64(index) < isiCount; index++ {
		var visits uint64
		if err := rfc6330.VisitEncodingIndices(kPrime, isiAt(index), func(uint32) error {
			visits++
			return nil
		}); err != nil {
			return 0, 0, fmt.Errorf("%w: count G_ENC row %d: %w", ErrConstraintBuild, index, err)
		}
		nonzeros, overflow = addCapacity(nonzeros, visits)
		if overflow {
			return 0, 0, fmt.Errorf("%w: sparse G_ENC capacity overflow", ErrConstraintDimensions)
		}
	}
	fillAllowance := rows * uint64(parameters.P())
	nonzeros, overflow = addCapacity(nonzeros, fillAllowance)
	if overflow {
		return 0, 0, fmt.Errorf("%w: sparse fill capacity overflow", ErrConstraintDimensions)
	}
	maximum := rows * columns
	if nonzeros > maximum {
		nonzeros = maximum
	}
	bytes, err := matrix.SparseOctetBackingBytes(uint32(rows), parameters.L(), nonzeros)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: sparse backing: %w", ErrConstraintDimensions, err)
	}
	return bytes, nonzeros, nil
}

func buildSparseConstraintMatrix(kPrime uint32, isiCount uint64, isiAt func(uint32) uint32, maxBytes uint64) (*matrix.SparseOctet, error) {
	parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
	if err != nil {
		return nil, err
	}
	required, capacity, err := sparseConstraintMatrixBackingBytes(kPrime, isiCount, isiAt)
	if err != nil {
		return nil, err
	}
	if required > maxBytes {
		return nil, fmt.Errorf("%w: sparse matrix needs %d bytes (limit %d)", ErrConstraintBuild, required, maxBytes)
	}
	rows := parameters.S + parameters.H + uint32(isiCount)
	result, err := matrix.NewSparseOctet(rows, parameters.L(), capacity, required)
	if err != nil {
		return nil, fmt.Errorf("%w: allocate sparse matrix: %w", ErrConstraintBuild, err)
	}
	if err := buildSparseLDPC(result, parameters); err != nil {
		return nil, err
	}
	if err := buildSparseHDPC(result, parameters); err != nil {
		return nil, err
	}
	encodedRow := parameters.S + parameters.H
	for index := uint32(0); uint64(index) < isiCount; index++ {
		isi := isiAt(index)
		if err := rfc6330.VisitEncodingIndices(kPrime, isi, func(column uint32) error {
			value, atErr := result.At(encodedRow+index, column)
			if atErr != nil {
				return atErr
			}
			return result.Set(encodedRow+index, column, value^1)
		}); err != nil {
			return nil, fmt.Errorf("%w: sparse G_ENC row %d ISI %d: %w", ErrConstraintBuild, index, isi, err)
		}
	}
	if result.BackingBytes() != required {
		return nil, fmt.Errorf("%w: sparse retained %d bytes, estimated %d", ErrConstraintBuild, result.BackingBytes(), required)
	}
	return result, nil
}

func buildSparseLDPC(result *matrix.SparseOctet, parameters rfc6330.Parameters) error {
	for column := uint32(0); column < parameters.B(); column++ {
		a := 1 + column/parameters.S
		row := column % parameters.S
		for count := 0; count < 3; count++ {
			if err := result.Set(row, column, 1); err != nil {
				return fmt.Errorf("%w: sparse G_LDPC,1: %w", ErrConstraintBuild, err)
			}
			row = (row + a) % parameters.S
		}
	}
	for row := uint32(0); row < parameters.S; row++ {
		if err := result.Set(row, parameters.B()+row, 1); err != nil {
			return fmt.Errorf("%w: sparse I_S: %w", ErrConstraintBuild, err)
		}
		if err := result.Set(row, parameters.W+row%parameters.P(), 1); err != nil {
			return fmt.Errorf("%w: sparse G_LDPC,2: %w", ErrConstraintBuild, err)
		}
		if err := result.Set(row, parameters.W+(row+1)%parameters.P(), 1); err != nil {
			return fmt.Errorf("%w: sparse G_LDPC,2: %w", ErrConstraintBuild, err)
		}
	}
	return nil
}

func buildSparseHDPC(result *matrix.SparseOctet, parameters rfc6330.Parameters) error {
	nonHDPCColumns := parameters.KPrime + parameters.S
	for hdpcRow := uint32(0); hdpcRow < parameters.H; hdpcRow++ {
		row := parameters.S + hdpcRow
		value := gf256.AlphaPower(hdpcRow)
		if err := result.Set(row, nonHDPCColumns-1, value); err != nil {
			return fmt.Errorf("%w: sparse G_HDPC final column: %w", ErrConstraintBuild, err)
		}
		for column := nonHDPCColumns - 1; column > 0; {
			column--
			value = gf256.Mul(2, value)
			first, err := rfc6330.Rand(column+1, 6, parameters.H)
			if err != nil {
				return fmt.Errorf("%w: sparse MT first row: %w", ErrConstraintBuild, err)
			}
			secondOffset, err := rfc6330.Rand(column+1, 7, parameters.H-1)
			if err != nil {
				return fmt.Errorf("%w: sparse MT second row: %w", ErrConstraintBuild, err)
			}
			second := (first + secondOffset + 1) % parameters.H
			if hdpcRow == first || hdpcRow == second {
				value ^= 1
			}
			if err := result.Set(row, column, value); err != nil {
				return fmt.Errorf("%w: sparse G_HDPC: %w", ErrConstraintBuild, err)
			}
		}
		if err := result.Set(row, nonHDPCColumns+hdpcRow, 1); err != nil {
			return fmt.Errorf("%w: sparse I_H: %w", ErrConstraintBuild, err)
		}
	}
	return nil
}

func addCapacity(left, right uint64) (uint64, bool) {
	if left > ^uint64(0)-right {
		return 0, true
	}
	return left + right, false
}
