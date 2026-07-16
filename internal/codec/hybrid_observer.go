package codec

import (
	"fmt"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/matrix"
)

// HybridExecutionObservation is an immutable, deep-copied development trace
// record emitted by the production hybrid executor. Byte matrices are flat in
// row-major order. Binary is in current logical row/column order; HDPC remains
// in physical RFC column order; WorkingD is in executor physical row order.
// Empty fields do not apply to the record's Stage.
type HybridExecutionObservation struct {
	Stage string
	Step  uint32

	DenseColumns                          uint32
	RowMap, RowInverse                    []uint32
	ColumnMap, ColumnInverse              []uint32
	Binary, HDPC, WorkingD                []byte
	RHSMap                                []uint32
	ResidualRows, ResidualColumns         uint32
	ResidualCoefficients, ResidualRHS     []byte
	ResidualSolution                      []byte
	SubstitutionRow                       uint32
	SubstitutionColumns                   []uint32
	SubstitutionBefore, SubstitutionAfter []byte
	InputD, Result                        []byte
}

// HybridExecutionObserver consumes owned observations. It is development-only
// internal API; normal production execution passes no observer and performs no
// trace allocation.
type HybridExecutionObserver func(HybridExecutionObservation) error

func observeHybridState(
	observer HybridExecutionObserver,
	stage string,
	step uint32,
	binary *matrix.HybridBinary,
	hdpc *gf256.Matrix,
	workingD *gf256.Symbols,
) error {
	if observer == nil {
		return nil
	}
	rowMap, rowInverse, columnMap, columnInverse, err := snapshotHybridMappings(binary)
	if err != nil {
		return err
	}
	binaryValues, err := snapshotHybridBinary(binary)
	if err != nil {
		return err
	}
	hdpcValues, err := snapshotGFMatrix(hdpc)
	if err != nil {
		return err
	}
	workingValues, err := snapshotGFSymbols(workingD)
	if err != nil {
		return err
	}
	return observer(HybridExecutionObservation{
		Stage: stage, Step: step, DenseColumns: binary.DenseColumns(),
		RowMap: rowMap, RowInverse: rowInverse,
		ColumnMap: columnMap, ColumnInverse: columnInverse,
		Binary: binaryValues, HDPC: hdpcValues, WorkingD: workingValues,
	})
}

func snapshotHybridMappings(binary *matrix.HybridBinary) ([]uint32, []uint32, []uint32, []uint32, error) {
	rowMap := make([]uint32, binary.Rows())
	rowInverse := make([]uint32, binary.Rows())
	for index := uint32(0); index < binary.Rows(); index++ {
		physical, err := binary.PhysicalRow(index)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		logical, err := binary.LogicalRow(index)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		rowMap[index], rowInverse[index] = physical, logical
	}
	columnMap := make([]uint32, binary.Columns())
	columnInverse := make([]uint32, binary.Columns())
	for index := uint32(0); index < binary.Columns(); index++ {
		physical, err := binary.PhysicalColumn(index)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		logical, err := binary.LogicalColumn(index)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		columnMap[index], columnInverse[index] = physical, logical
	}
	return rowMap, rowInverse, columnMap, columnInverse, nil
}

func snapshotHybridBinary(binary *matrix.HybridBinary) ([]byte, error) {
	values := make([]byte, uint64(binary.Rows())*uint64(binary.Columns()))
	for row := uint32(0); row < binary.Rows(); row++ {
		for column := uint32(0); column < binary.Columns(); column++ {
			set, err := binary.Has(row, column)
			if err != nil {
				return nil, err
			}
			if set {
				values[uint64(row)*uint64(binary.Columns())+uint64(column)] = 1
			}
		}
	}
	return values, nil
}

func snapshotGFMatrix(value *gf256.Matrix) ([]byte, error) {
	result := make([]byte, uint64(value.Rows())*uint64(value.Columns()))
	for row := uint32(0); row < value.Rows(); row++ {
		for column := uint32(0); column < value.Columns(); column++ {
			octet, err := value.At(row, column)
			if err != nil {
				return nil, err
			}
			result[uint64(row)*uint64(value.Columns())+uint64(column)] = octet
		}
	}
	return result, nil
}

func snapshotGFSymbols(value *gf256.Symbols) ([]byte, error) {
	result := make([]byte, 0, value.Bytes())
	for row := uint32(0); row < value.Count(); row++ {
		symbol, err := value.Symbol(row)
		if err != nil {
			return nil, err
		}
		result = append(result, symbol...)
	}
	return result, nil
}

func emitHybridObservation(observer HybridExecutionObserver, observation HybridExecutionObservation) error {
	if observer == nil {
		return nil
	}
	if err := observer(observation); err != nil {
		return fmt.Errorf("%w: observer: %w", ErrHybridPlanInvariant, err)
	}
	return nil
}

func emitHybridSubstitutionObservation(
	observer HybridExecutionObserver,
	step, row uint32,
	columns []uint32,
	before, after []byte,
) error {
	if observer == nil {
		return nil
	}
	return emitHybridObservation(observer, HybridExecutionObservation{
		Stage: "substitute", Step: step,
		SubstitutionRow:     row,
		SubstitutionColumns: columns,
		SubstitutionBefore:  before,
		SubstitutionAfter:   append([]byte(nil), after...),
	})
}
