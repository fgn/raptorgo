// Package codec implements RFC 6330 source-block constraint construction,
// intermediate-symbol generation, raw encoding, and decoding substrate.
package codec

import (
	"errors"
	"fmt"
	"math"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

var (
	ErrConstraintDimensions = errors.New("codec: invalid constraint dimensions")
	ErrConstraintBuild      = errors.New("codec: constraint matrix construction failed")
)

// ConstraintMatrixBackingBytes returns the dense reference matrix backing
// bytes for S+H+len(isis) rows and L columns.
func ConstraintMatrixBackingBytes(kPrime uint32, isiCount uint64) (uint64, error) {
	parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
	if err != nil {
		return 0, err
	}
	baseRows := uint64(parameters.S) + uint64(parameters.H)
	if isiCount > math.MaxUint32-baseRows {
		return 0, fmt.Errorf("%w: %d base rows plus %d encoded rows", ErrConstraintDimensions, baseRows, isiCount)
	}
	rows := baseRows + isiCount
	bytes := rows * uint64(parameters.L())
	if bytes > uint64(^uint(0)>>1) {
		return 0, fmt.Errorf("%w: %dx%d backing exceeds platform limit", ErrConstraintDimensions, rows, parameters.L())
	}
	return bytes, nil
}

// BuildConstraintMatrix builds Figure 5's dense reference matrix. The encoded
// rows appear in the same order as isis. Repeated Enc index visits toggle the
// binary coefficient, matching repeated XOR during payload generation.
func BuildConstraintMatrix(kPrime uint32, isis []uint32, maxBytes uint64) (*gf256.Matrix, error) {
	return buildConstraintMatrix(kPrime, uint64(len(isis)), func(index uint32) uint32 {
		return isis[index]
	}, maxBytes)
}

// BuildSourceConstraintMatrix builds the square encoder matrix whose encoded
// rows are ISIs [0,K'). It avoids allocating a temporary ISI slice.
func BuildSourceConstraintMatrix(kPrime uint32, maxBytes uint64) (*gf256.Matrix, error) {
	return buildConstraintMatrix(kPrime, uint64(kPrime), func(index uint32) uint32 {
		return index
	}, maxBytes)
}

func buildConstraintMatrix(kPrime uint32, isiCount uint64, isiAt func(index uint32) uint32, maxBytes uint64) (*gf256.Matrix, error) {
	parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
	if err != nil {
		return nil, err
	}
	required, err := ConstraintMatrixBackingBytes(kPrime, isiCount)
	if err != nil {
		return nil, err
	}
	rows := uint64(parameters.S) + uint64(parameters.H) + isiCount
	matrix, err := gf256.NewMatrix(uint32(rows), parameters.L(), maxBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: allocate: %w", ErrConstraintBuild, err)
	}

	if err := buildLDPC(matrix, parameters); err != nil {
		return nil, err
	}
	if err := buildHDPC(matrix, parameters); err != nil {
		return nil, err
	}
	encodedRow := parameters.S + parameters.H
	for index := uint32(0); uint64(index) < isiCount; index++ {
		isi := isiAt(index)
		err := rfc6330.VisitEncodingIndices(kPrime, isi, func(column uint32) error {
			return toggle(matrix, encodedRow+index, column)
		})
		if err != nil {
			return nil, fmt.Errorf("%w: G_ENC row %d ISI %d: %w", ErrConstraintBuild, index, isi, err)
		}
	}
	if matrix.Bytes() != required {
		return nil, fmt.Errorf("%w: retained %d bytes, estimated %d", ErrConstraintBuild, matrix.Bytes(), required)
	}
	return matrix, nil
}

func buildLDPC(matrix *gf256.Matrix, parameters rfc6330.Parameters) error {
	for column := uint32(0); column < parameters.B(); column++ {
		a := 1 + column/parameters.S
		row := column % parameters.S
		for count := 0; count < 3; count++ {
			if err := toggle(matrix, row, column); err != nil {
				return fmt.Errorf("%w: G_LDPC,1: %w", ErrConstraintBuild, err)
			}
			row = (row + a) % parameters.S
		}
	}
	for row := uint32(0); row < parameters.S; row++ {
		if err := matrix.Set(row, parameters.B()+row, 1); err != nil {
			return fmt.Errorf("%w: I_S: %w", ErrConstraintBuild, err)
		}
		for _, offset := range []uint32{row % parameters.P(), (row + 1) % parameters.P()} {
			if err := toggle(matrix, row, parameters.W+offset); err != nil {
				return fmt.Errorf("%w: G_LDPC,2: %w", ErrConstraintBuild, err)
			}
		}
	}
	return nil
}

func buildHDPC(matrix *gf256.Matrix, parameters rfc6330.Parameters) error {
	nonHDPCColumns := parameters.KPrime + parameters.S
	for hdpcRow := uint32(0); hdpcRow < parameters.H; hdpcRow++ {
		row := parameters.S + hdpcRow
		value := gf256.AlphaPower(hdpcRow)
		if err := matrix.Set(row, nonHDPCColumns-1, value); err != nil {
			return fmt.Errorf("%w: G_HDPC final column: %w", ErrConstraintBuild, err)
		}
		for column := nonHDPCColumns - 1; column > 0; {
			column--
			value = gf256.Mul(2, value)
			first, err := rfc6330.Rand(column+1, 6, parameters.H)
			if err != nil {
				return fmt.Errorf("%w: MT first row: %w", ErrConstraintBuild, err)
			}
			secondOffset, err := rfc6330.Rand(column+1, 7, parameters.H-1)
			if err != nil {
				return fmt.Errorf("%w: MT second row: %w", ErrConstraintBuild, err)
			}
			second := (first + secondOffset + 1) % parameters.H
			if hdpcRow == first || hdpcRow == second {
				value ^= 1
			}
			if err := matrix.Set(row, column, value); err != nil {
				return fmt.Errorf("%w: G_HDPC: %w", ErrConstraintBuild, err)
			}
		}
		if err := matrix.Set(row, nonHDPCColumns+hdpcRow, 1); err != nil {
			return fmt.Errorf("%w: I_H: %w", ErrConstraintBuild, err)
		}
	}
	return nil
}

func toggle(matrix *gf256.Matrix, row, column uint32) error {
	value, err := matrix.At(row, column)
	if err != nil {
		return err
	}
	return matrix.Set(row, column, value^1)
}
