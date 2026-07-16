package codec

import (
	"errors"
	"fmt"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

var (
	ErrEncoderWorkspace = errors.New("codec: source-block encoder workspace limit exceeded")
	ErrEncoderSymbolID  = errors.New("codec: invalid encoder symbol ID")
)

// SourceBlockEncoder is an immutable raw RFC 6330 source-block encoder. It
// owns both source and intermediate symbols, so caller mutation after
// construction cannot change future output. Concurrent calls to its read-only
// methods, including Symbol, are safe.
type SourceBlockEncoder struct {
	parameters   rfc6330.Parameters
	source       *gf256.Symbols
	intermediate *gf256.Symbols
}

// SourceBlockEncoderWorkspaceBackingBytes returns the constructor's peak
// logical backing bytes: an owned source clone plus intermediate generation.
func SourceBlockEncoderWorkspaceBackingBytes(k, symbolSize uint32) (uint64, error) {
	intermediateBytes, err := HybridIntermediateWorkspaceBackingBytes(k, symbolSize)
	if err != nil {
		return 0, fmt.Errorf("%w: intermediate generation: %w", ErrEncoderWorkspace, err)
	}
	sourceBytes := uint64(k) * uint64(symbolSize)
	return checkedBackingSum(ErrEncoderWorkspace, sourceBytes, intermediateBytes)
}

// SourceBlockEncoderRetainedBackingBytes returns the immutable source and
// intermediate-symbol slabs retained after construction.
func SourceBlockEncoderRetainedBackingBytes(k, symbolSize uint32) (uint64, error) {
	parameters, err := rfc6330.ParametersForSourceSymbols(k)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrEncoderWorkspace, err)
	}
	if symbolSize == 0 {
		return 0, fmt.Errorf("%w: zero symbol size", ErrEncoderWorkspace)
	}
	return checkedBackingSum(
		ErrEncoderWorkspace,
		uint64(k)*uint64(symbolSize),
		uint64(parameters.L())*uint64(symbolSize),
	)
}

// NewSourceBlockEncoder clones source and generates its intermediate symbols.
func NewSourceBlockEncoder(source *gf256.Symbols, maxWorkspace uint64) (*SourceBlockEncoder, error) {
	if source == nil || source.Count() == 0 || source.Size() == 0 {
		return nil, fmt.Errorf("%w: nil or empty", ErrSourceSymbolShape)
	}
	parameters, err := rfc6330.ParametersForSourceSymbols(source.Count())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceSymbolShape, err)
	}
	sourceBytes := uint64(source.Count()) * uint64(source.Size())
	if sourceBytes > maxWorkspace {
		return nil, fmt.Errorf("%w: source clone needs %d bytes (limit %d)", ErrEncoderWorkspace, sourceBytes, maxWorkspace)
	}
	plan, intermediateBytes, err := prepareHybridSourcePlan(source.Count(), source.Size(), maxWorkspace-sourceBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: intermediate generation: %w", ErrEncoderWorkspace, err)
	}
	required, err := checkedBackingSum(ErrEncoderWorkspace, sourceBytes, intermediateBytes)
	if err != nil {
		return nil, err
	}
	if required > maxWorkspace {
		return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrEncoderWorkspace, required, maxWorkspace)
	}
	owned, err := gf256.NewSymbols(source.Count(), source.Size(), sourceBytes)
	if err != nil {
		return nil, fmt.Errorf("clone source: %w", err)
	}
	for index := uint32(0); index < source.Count(); index++ {
		values, err := source.Symbol(index)
		if err != nil {
			return nil, fmt.Errorf("source %d: %w", index, err)
		}
		if err := owned.SetSymbol(index, values); err != nil {
			return nil, fmt.Errorf("clone source %d: %w", index, err)
		}
	}
	intermediate, err := executeHybridSourcePlan(owned, plan)
	if err != nil {
		return nil, err
	}
	return &SourceBlockEncoder{parameters: parameters, source: owned, intermediate: intermediate}, nil
}

func (encoder *SourceBlockEncoder) SourceSymbols() uint32 { return encoder.source.Count() }
func (encoder *SourceBlockEncoder) SymbolSize() uint32    { return encoder.source.Size() }

// Symbol returns a newly owned source or repair symbol for an explicit ESI.
func (encoder *SourceBlockEncoder) Symbol(esi uint32, maxBytes uint64) ([]byte, error) {
	if encoder == nil || encoder.source == nil || encoder.intermediate == nil {
		return nil, fmt.Errorf("%w: nil encoder", ErrSourceSymbolShape)
	}
	if esi > rfc6330.MaxEncodingSymbolID {
		return nil, fmt.Errorf("%w: ESI=%d", ErrEncoderSymbolID, esi)
	}
	if esi < encoder.source.Count() {
		if uint64(encoder.source.Size()) > maxBytes {
			return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrEncodedSymbolLimit, encoder.source.Size(), maxBytes)
		}
		values, err := encoder.source.Symbol(esi)
		if err != nil {
			return nil, err
		}
		return append([]byte(nil), values...), nil
	}
	isi, err := rfc6330.InternalSymbolID(encoder.source.Count(), esi)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEncoderSymbolID, err)
	}
	return EncodingSymbol(encoder.parameters.KPrime, isi, encoder.intermediate, maxBytes)
}
