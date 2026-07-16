package codec

import (
	"errors"
	"fmt"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

var (
	ErrSourceSymbolShape       = errors.New("codec: invalid source symbol shape")
	ErrIntermediateWorkspace   = errors.New("codec: intermediate-symbol workspace limit exceeded")
	ErrIntermediateSolve       = errors.New("codec: intermediate-symbol solve failed")
	ErrIntermediateSymbolShape = errors.New("codec: invalid intermediate symbol shape")
	ErrEncodedSymbolLimit      = errors.New("codec: encoded symbol allocation limit exceeded")
)

// IntermediateWorkspaceBackingBytes returns the peak logical backing bytes
// owned while generating C: retained A and D plus Solve's A/D clones and C
// result. It excludes the caller-owned source symbols, Go object headers, and
// allocator rounding.
func IntermediateWorkspaceBackingBytes(k, symbolSize uint32) (uint64, error) {
	parameters, err := rfc6330.ParametersForSourceSymbols(k)
	if err != nil {
		return 0, err
	}
	if symbolSize == 0 {
		return 0, fmt.Errorf("%w: zero symbol size", ErrSourceSymbolShape)
	}
	matrixBytes, err := ConstraintMatrixBackingBytes(parameters.KPrime, uint64(parameters.KPrime))
	if err != nil {
		return 0, fmt.Errorf("%w: constraint matrix: %w", ErrIntermediateWorkspace, err)
	}
	symbolBytes := uint64(parameters.L()) * uint64(symbolSize)
	solveBytes, err := gf256.SolveWorkspaceBytes(parameters.L(), parameters.L(), symbolSize)
	if err != nil {
		return 0, fmt.Errorf("%w: solve workspace: %w", ErrIntermediateWorkspace, err)
	}
	return checkedBackingSum(ErrIntermediateWorkspace, matrixBytes, symbolBytes, solveBytes)
}

// GenerateIntermediateSymbols constructs the RFC source constraint system and
// solves A*C=D. Source symbols are never mutated or aliased by the result.
func GenerateIntermediateSymbols(source *gf256.Symbols, maxWorkspace uint64) (*gf256.Symbols, error) {
	if source == nil || source.Count() == 0 || source.Size() == 0 {
		return nil, fmt.Errorf("%w: nil or empty", ErrSourceSymbolShape)
	}
	parameters, err := rfc6330.ParametersForSourceSymbols(source.Count())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceSymbolShape, err)
	}
	required, err := IntermediateWorkspaceBackingBytes(source.Count(), source.Size())
	if err != nil {
		return nil, err
	}
	if required > maxWorkspace {
		return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrIntermediateWorkspace, required, maxWorkspace)
	}

	matrixBytes, err := ConstraintMatrixBackingBytes(parameters.KPrime, uint64(parameters.KPrime))
	if err != nil {
		return nil, err
	}
	constraints, err := BuildSourceConstraintMatrix(parameters.KPrime, matrixBytes)
	if err != nil {
		return nil, err
	}
	symbolBytes := uint64(parameters.L()) * uint64(source.Size())
	rightHandSide, err := gf256.NewSymbols(parameters.L(), source.Size(), symbolBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: allocate D: %w", ErrIntermediateSolve, err)
	}
	firstSourceRow := parameters.S + parameters.H
	for index := uint32(0); index < source.Count(); index++ {
		sourceSymbol, err := source.Symbol(index)
		if err != nil {
			return nil, fmt.Errorf("%w: source %d: %w", ErrSourceSymbolShape, index, err)
		}
		if err := rightHandSide.SetSymbol(firstSourceRow+index, sourceSymbol); err != nil {
			return nil, fmt.Errorf("%w: D row %d: %w", ErrIntermediateSolve, firstSourceRow+index, err)
		}
	}
	solveBudget := maxWorkspace - matrixBytes - symbolBytes
	intermediate, err := gf256.Solve(constraints, rightHandSide, solveBudget)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrIntermediateSolve, err)
	}
	return intermediate, nil
}

// EncodingSymbol returns Enc[K',C,Tuple[K',ISI]]. The result owns its backing
// and cannot alias intermediate.
func EncodingSymbol(kPrime, isi uint32, intermediate *gf256.Symbols, maxBytes uint64) ([]byte, error) {
	parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
	if err != nil {
		return nil, err
	}
	if intermediate == nil || intermediate.Count() != parameters.L() || intermediate.Size() == 0 {
		return nil, fmt.Errorf("%w: got %v, want %d nonempty symbols", ErrIntermediateSymbolShape, intermediateShape(intermediate), parameters.L())
	}
	if uint64(intermediate.Size()) > maxBytes || uint64(intermediate.Size()) > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrEncodedSymbolLimit, intermediate.Size(), maxBytes)
	}
	result := make([]byte, int(intermediate.Size()))
	if err := encodingSymbolInto(kPrime, isi, intermediate, result); err != nil {
		return nil, err
	}
	return result, nil
}

func encodingSymbolInto(kPrime, isi uint32, intermediate *gf256.Symbols, result []byte) error {
	parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
	if err != nil {
		return err
	}
	if intermediate == nil || intermediate.Count() != parameters.L() || intermediate.Size() == 0 || uint64(len(result)) != uint64(intermediate.Size()) {
		return fmt.Errorf("%w: got %v and output %d, want %d nonempty symbols", ErrIntermediateSymbolShape, intermediateShape(intermediate), len(result), parameters.L())
	}
	clear(result)
	err = rfc6330.VisitEncodingIndices(kPrime, isi, func(index uint32) error {
		symbol, err := intermediate.Symbol(index)
		if err != nil {
			return err
		}
		return gf256.XOR(result, symbol)
	})
	if err != nil {
		return fmt.Errorf("encode ISI %d: %w", isi, err)
	}
	return nil
}

func checkedBackingSum(owner error, values ...uint64) (uint64, error) {
	var total uint64
	for _, value := range values {
		if total > ^uint64(0)-value {
			return 0, fmt.Errorf("%w: backing size overflow", owner)
		}
		total += value
	}
	if total > uint64(^uint(0)>>1) {
		return 0, fmt.Errorf("%w: aggregate %d exceeds platform limit", owner, total)
	}
	return total, nil
}

func intermediateShape(symbols *gf256.Symbols) string {
	if symbols == nil {
		return "nil"
	}
	return fmt.Sprintf("%dx%d", symbols.Count(), symbols.Size())
}
