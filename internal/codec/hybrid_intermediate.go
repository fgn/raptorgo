package codec

import (
	"fmt"
	"math"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

// HybridIntermediateWorkspaceBackingBytes returns the production source
// solver's peak logical backing. The exact packed-U width is instance
// dependent, so this routine first constructs the deterministic source plan.
// Caller-owned source symbols are excluded.
func HybridIntermediateWorkspaceBackingBytes(k, symbolSize uint32) (uint64, error) {
	plan, required, err := prepareHybridSourcePlan(k, symbolSize, math.MaxUint64)
	if err != nil {
		return 0, err
	}
	_ = plan
	return required, nil
}

// GenerateIntermediateSymbolsHybrid constructs and solves the RFC 6330
// source system with bounded binary inactivation and dense GF(256) completion.
// It never mutates or aliases source on success or failure.
func GenerateIntermediateSymbolsHybrid(source *gf256.Symbols, maxWorkspace uint64) (*gf256.Symbols, error) {
	if source == nil || source.Count() == 0 || source.Size() == 0 {
		return nil, fmt.Errorf("%w: nil or empty", ErrSourceSymbolShape)
	}
	plan, _, err := prepareHybridSourcePlan(source.Count(), source.Size(), maxWorkspace)
	if err != nil {
		return nil, err
	}
	return executeHybridSourcePlan(source, plan)
}

func prepareHybridSourcePlan(k, symbolSize uint32, maxWorkspace uint64) (*InactivationPlan, uint64, error) {
	parameters, err := rfc6330.ParametersForSourceSymbols(k)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrSourceSymbolShape, err)
	}
	if symbolSize == 0 {
		return nil, 0, fmt.Errorf("%w: zero symbol size", ErrSourceSymbolShape)
	}
	planBytes, err := InactivationPlanBackingBytes(parameters.KPrime, parameters.KPrime)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: plan: %w", ErrIntermediateWorkspace, err)
	}
	if planBytes > maxWorkspace {
		return nil, 0, fmt.Errorf("%w: need at least %d planning bytes (limit %d)", ErrIntermediateWorkspace, planBytes, maxWorkspace)
	}
	plan, err := PlanSourceInactivation(parameters.KPrime, planBytes)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: plan: %w", ErrIntermediateWorkspace, err)
	}
	dBytes := uint64(plan.totalRows) * uint64(symbolSize)
	executionBytes, err := InactivationExecutionBackingBytes(plan, symbolSize)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: execution: %w", ErrIntermediateWorkspace, err)
	}
	required, err := checkedBackingSum(ErrIntermediateWorkspace, planBytes, dBytes, executionBytes)
	if err != nil {
		return nil, 0, err
	}
	if required > maxWorkspace {
		return nil, 0, fmt.Errorf("%w: need %d bytes (limit %d)", ErrIntermediateWorkspace, required, maxWorkspace)
	}
	return plan, required, nil
}

func executeHybridSourcePlan(source *gf256.Symbols, plan *InactivationPlan) (*gf256.Symbols, error) {
	if source == nil || plan == nil || source.Size() == 0 || source.Count() > plan.parameters.KPrime {
		return nil, fmt.Errorf("%w: source/plan shape", ErrSourceSymbolShape)
	}
	dBytes := uint64(plan.totalRows) * uint64(source.Size())
	d, err := gf256.NewSymbols(plan.totalRows, source.Size(), dBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: D: %w", ErrIntermediateWorkspace, err)
	}
	for index := uint32(0); index < source.Count(); index++ {
		values, symbolErr := source.Symbol(index)
		if symbolErr != nil {
			return nil, fmt.Errorf("%w: source %d: %w", ErrSourceSymbolShape, index, symbolErr)
		}
		row := plan.parameters.S + plan.parameters.H + index
		if symbolErr = d.SetSymbol(row, values); symbolErr != nil {
			return nil, fmt.Errorf("%w: D row %d: %w", ErrIntermediateSolve, row, symbolErr)
		}
	}
	executionBytes, err := InactivationExecutionBackingBytes(plan, source.Size())
	if err != nil {
		return nil, fmt.Errorf("%w: execution: %w", ErrIntermediateWorkspace, err)
	}
	result, err := ExecuteInactivationPlan(plan, d, executionBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: hybrid: %w", ErrIntermediateSolve, err)
	}
	return result, nil
}
