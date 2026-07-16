package codec

import (
	"errors"
	"fmt"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

const MaxEagerSourceSymbols uint32 = 1_024

var ErrEagerScaleLimit = errors.New("codec: eager sparse reference size limit exceeded")

// SparseIntermediateWorkspaceBackingBytes returns the eager sparse reference
// path's peak logical backing: retained source constraint matrix and D plus the
// inactivation solver's private workspace.
func SparseIntermediateWorkspaceBackingBytes(k, symbolSize uint32) (uint64, error) {
	parameters, err := rfc6330.ParametersForSourceSymbols(k)
	if err != nil {
		return 0, err
	}
	if symbolSize == 0 {
		return 0, fmt.Errorf("%w: zero symbol size", ErrSourceSymbolShape)
	}
	if k > MaxEagerSourceSymbols {
		return 0, fmt.Errorf("%w: K=%d exceeds %d", ErrEagerScaleLimit, k, MaxEagerSourceSymbols)
	}
	matrixBytes, capacity, err := SparseSourceConstraintMatrixBackingBytes(parameters.KPrime)
	if err != nil {
		return 0, fmt.Errorf("%w: sparse constraint matrix: %w", ErrIntermediateWorkspace, err)
	}
	dBytes := uint64(parameters.L()) * uint64(symbolSize)
	solverBytes, err := InactivationWorkspaceBackingBytes(parameters.L(), parameters.L(), capacity, symbolSize)
	if err != nil {
		return 0, fmt.Errorf("%w: sparse solver: %w", ErrIntermediateWorkspace, err)
	}
	return checkedBackingSum(ErrIntermediateWorkspace, matrixBytes, dBytes, solverBytes)
}

// GenerateIntermediateSymbolsSparse constructs and solves the source system
// through the eager inactivation reference path. It is size-gated until the
// optimized hybrid binary/HDPC owner replaces it.
func GenerateIntermediateSymbolsSparse(source *gf256.Symbols, maxWorkspace uint64) (*gf256.Symbols, error) {
	if source == nil || source.Count() == 0 || source.Size() == 0 {
		return nil, fmt.Errorf("%w: nil or empty", ErrSourceSymbolShape)
	}
	parameters, err := rfc6330.ParametersForSourceSymbols(source.Count())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceSymbolShape, err)
	}
	required, err := SparseIntermediateWorkspaceBackingBytes(source.Count(), source.Size())
	if err != nil {
		return nil, err
	}
	if required > maxWorkspace {
		return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrIntermediateWorkspace, required, maxWorkspace)
	}
	matrixBytes, _, err := SparseSourceConstraintMatrixBackingBytes(parameters.KPrime)
	if err != nil {
		return nil, err
	}
	constraints, err := BuildSparseSourceConstraintMatrix(parameters.KPrime, matrixBytes)
	if err != nil {
		return nil, err
	}
	dBytes := uint64(parameters.L()) * uint64(source.Size())
	d, err := gf256.NewSymbols(parameters.L(), source.Size(), dBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: allocate sparse D: %w", ErrIntermediateWorkspace, err)
	}
	firstSourceRow := parameters.S + parameters.H
	for index := uint32(0); index < source.Count(); index++ {
		values, symbolErr := source.Symbol(index)
		if symbolErr != nil {
			return nil, fmt.Errorf("%w: source %d: %w", ErrSourceSymbolShape, index, symbolErr)
		}
		if symbolErr = d.SetSymbol(firstSourceRow+index, values); symbolErr != nil {
			return nil, fmt.Errorf("%w: sparse D row %d: %w", ErrIntermediateSolve, firstSourceRow+index, symbolErr)
		}
	}
	solveBudget := maxWorkspace - matrixBytes - dBytes
	result, err := SolveInactivation(constraints, d, parameters.P(), parameters.S, parameters.H, solveBudget)
	if err != nil {
		return nil, fmt.Errorf("%w: sparse: %w", ErrIntermediateSolve, err)
	}
	return result, nil
}
