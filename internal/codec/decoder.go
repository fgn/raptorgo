package codec

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/matrix"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

var (
	ErrDecoderLimits            = errors.New("codec: invalid source-block decoder limits")
	ErrDecoderBacking           = errors.New("codec: source-block decoder backing limit exceeded")
	ErrDecoderSymbol            = errors.New("codec: invalid received encoding symbol")
	ErrDecoderDuplicateConflict = errors.New("codec: conflicting duplicate encoding symbol")
	ErrDecoderRetainedLimit     = errors.New("codec: retained-symbol limit exceeded")
	ErrDecoderAttemptLimit      = errors.New("codec: decode-attempt limit exceeded")
	ErrDecoderComplexity        = errors.New("codec: dense-completion complexity limit exceeded")
	ErrDecoderConflict          = errors.New("codec: received equations are inconsistent")
	ErrDecoderComplete          = errors.New("codec: source-block decoder is complete")
	ErrDecoderNotComplete       = errors.New("codec: source-block decoder is not complete")
)

// SourceBlockDecoderLimits bounds persistent retention and repeated solving.
// MaxWorkingMemory includes persistent decoder backing plus the worst allowed
// decode attempt. All fields must be nonzero and MaxRetainedSymbols >= K.
type SourceBlockDecoderLimits struct {
	MaxRetainedSymbols        uint32
	MaxDecodeAttempts         uint32
	MaxDenseCompletionSymbols uint32
	MaxWorkingMemory          uint64
}

// SourceBlockDecoder incrementally retains canonical T-byte symbols for one
// RFC source block. It is not safe for concurrent use. Successful FEC recovery
// is deliberately unauthenticated; callers must enforce any trust boundary.
type SourceBlockDecoder struct {
	parameters       rfc6330.Parameters
	k                uint32
	symbolSize       uint32
	limits           SourceBlockDecoderLimits
	persistentBytes  uint64
	esis             []uint32
	payloads         *gf256.Symbols
	decoded          *gf256.Symbols
	retained         uint32
	attempts         uint32
	complete         bool
	terminalConflict bool
}

// SourceBlockDecoderBackingBytes returns persistent logical backing: retained
// ESI words and payload slab plus a preallocated K-symbol decoded result.
func SourceBlockDecoderBackingBytes(k, symbolSize, maxRetained uint32) (uint64, error) {
	if k == 0 || symbolSize == 0 || maxRetained < k {
		return 0, fmt.Errorf("%w: K=%d T=%d retained=%d", ErrDecoderLimits, k, symbolSize, maxRetained)
	}
	if _, err := rfc6330.ParametersForSourceSymbols(k); err != nil {
		return 0, fmt.Errorf("%w: %w", ErrDecoderLimits, err)
	}
	return checkedBackingSum(
		ErrDecoderBacking,
		uint64(maxRetained)*4,
		uint64(maxRetained)*uint64(symbolSize),
		uint64(k)*uint64(symbolSize),
	)
}

// SourceBlockDecoderWorstAttemptBackingBytes returns a conservative logical
// backing bound for any attempt at the retained-symbol cap whose dense Schur
// completion is at most maxDenseCompletion symbols wide. Wider attempts stop
// after bounded planning and retain their equations for a later retry.
func SourceBlockDecoderWorstAttemptBackingBytes(
	k, symbolSize, maxRetained, maxDenseCompletion uint32,
) (uint64, error) {
	parameters, err := rfc6330.ParametersForSourceSymbols(k)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrDecoderLimits, err)
	}
	if symbolSize == 0 || maxRetained < k || maxDenseCompletion < parameters.P() || maxDenseCompletion > parameters.L() {
		return 0, fmt.Errorf(
			"%w: K=%d T=%d retained=%d dense=%d",
			ErrDecoderLimits, k, symbolSize, maxRetained, maxDenseCompletion,
		)
	}
	padding := parameters.KPrime - k
	if maxRetained > ^uint32(0)-padding {
		return 0, fmt.Errorf("%w: encoded row count overflow", ErrDecoderBacking)
	}
	encodedRows := maxRetained + padding
	planBytes, err := InactivationPlanBackingBytes(parameters.KPrime, encodedRows)
	if err != nil {
		return 0, fmt.Errorf("%w: worst plan: %w", ErrDecoderBacking, err)
	}
	nonHDPCrows := parameters.S + encodedRows
	totalRows := nonHDPCrows + parameters.H
	vCapacity := uint64(3)*uint64(parameters.B()) + uint64(parameters.S) + uint64(encodedRows)*30
	if vCapacity > math.MaxUint32 {
		return 0, fmt.Errorf("%w: V incidence count overflow", ErrDecoderBacking)
	}
	maximumSparseCells := uint64(nonHDPCrows) * uint64(parameters.W)
	if vCapacity > maximumSparseCells {
		vCapacity = maximumSparseCells
	}
	hybridBytes, err := matrix.HybridBinaryBackingBytes(
		nonHDPCrows, parameters.L(), parameters.P(), maxDenseCompletion, vCapacity,
	)
	if err != nil {
		return 0, fmt.Errorf("%w: worst binary replay: %w", ErrDecoderBacking, err)
	}
	overhead := maxRetained - k
	residualRows := maxDenseCompletion + overhead
	coefficientBytes := uint64(residualRows) * uint64(maxDenseCompletion)
	rightHandSideBytes := uint64(residualRows) * uint64(symbolSize)
	solverBytes, err := gf256.SolveWorkspaceBytes(residualRows, maxDenseCompletion, symbolSize)
	if err != nil {
		return 0, fmt.Errorf("%w: worst dense completion: %w", ErrDecoderBacking, err)
	}
	dBytes := uint64(totalRows) * uint64(symbolSize)
	executionBytes, err := checkedBackingSum(
		ErrDecoderBacking,
		hybridBytes,
		uint64(parameters.H)*uint64(parameters.L()),
		dBytes, // executor-owned D clone
		coefficientBytes,
		rightHandSideBytes,
		solverBytes,
		uint64(parameters.L())*uint64(symbolSize),
	)
	if err != nil {
		return 0, err
	}
	return checkedBackingSum(
		ErrDecoderBacking,
		uint64(encodedRows)*4,
		planBytes,
		dBytes, // caller-side D materialization
		executionBytes,
	)
}

// NewSourceBlockDecoder preflights and allocates all persistent backing. It
// also proves the configured total working-memory limit can support an attempt
// at MaxRetainedSymbols before retaining any caller data.
func NewSourceBlockDecoder(k, symbolSize uint32, limits SourceBlockDecoderLimits) (*SourceBlockDecoder, error) {
	if limits.MaxRetainedSymbols == 0 || limits.MaxDecodeAttempts == 0 ||
		limits.MaxDenseCompletionSymbols == 0 || limits.MaxWorkingMemory == 0 {
		return nil, fmt.Errorf("%w: zero field", ErrDecoderLimits)
	}
	parameters, err := rfc6330.ParametersForSourceSymbols(k)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecoderLimits, err)
	}
	persistent, err := SourceBlockDecoderBackingBytes(k, symbolSize, limits.MaxRetainedSymbols)
	if err != nil {
		return nil, err
	}
	worstAttempt, err := SourceBlockDecoderWorstAttemptBackingBytes(
		k, symbolSize, limits.MaxRetainedSymbols, limits.MaxDenseCompletionSymbols,
	)
	if err != nil {
		return nil, err
	}
	total, err := checkedBackingSum(ErrDecoderBacking, persistent, worstAttempt)
	if err != nil {
		return nil, err
	}
	if total > limits.MaxWorkingMemory {
		return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrDecoderBacking, total, limits.MaxWorkingMemory)
	}
	payloadBytes := uint64(limits.MaxRetainedSymbols) * uint64(symbolSize)
	payloads, err := gf256.NewSymbols(limits.MaxRetainedSymbols, symbolSize, payloadBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: payload slab: %w", ErrDecoderBacking, err)
	}
	decodedBytes := uint64(k) * uint64(symbolSize)
	decoded, err := gf256.NewSymbols(k, symbolSize, decodedBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: decoded slab: %w", ErrDecoderBacking, err)
	}
	return &SourceBlockDecoder{
		parameters: parameters, k: k, symbolSize: symbolSize, limits: limits,
		persistentBytes: persistent, esis: make([]uint32, int(limits.MaxRetainedSymbols)),
		payloads: payloads, decoded: decoded,
	}, nil
}

func (decoder *SourceBlockDecoder) SourceSymbols() uint32 { return decoder.k }
func (decoder *SourceBlockDecoder) SymbolSize() uint32    { return decoder.symbolSize }
func (decoder *SourceBlockDecoder) RetainedSymbols() uint32 {
	return decoder.retained
}
func (decoder *SourceBlockDecoder) DecodeAttempts() uint32 { return decoder.attempts }
func (decoder *SourceBlockDecoder) Complete() bool         { return decoder.complete }
func (decoder *SourceBlockDecoder) BackingBytes() uint64   { return decoder.persistentBytes }

// AddSymbol validates and owns one received source or repair symbol. Identical
// duplicates are idempotent; conflicting duplicates are rejected without
// mutation. It returns true exactly when decoding is complete.
func (decoder *SourceBlockDecoder) AddSymbol(esi uint32, payload []byte) (bool, error) {
	if decoder == nil || decoder.payloads == nil || decoder.decoded == nil {
		return false, fmt.Errorf("%w: nil decoder", ErrDecoderSymbol)
	}
	if esi > rfc6330.MaxEncodingSymbolID || uint64(len(payload)) != uint64(decoder.symbolSize) {
		return false, fmt.Errorf("%w: ESI=%d payload=%d want=%d", ErrDecoderSymbol, esi, len(payload), decoder.symbolSize)
	}
	for index := uint32(0); index < decoder.retained; index++ {
		if decoder.esis[index] != esi {
			continue
		}
		retained, err := decoder.payloads.Symbol(index)
		if err != nil {
			return false, err
		}
		if !bytes.Equal(retained, payload) {
			return decoder.complete, fmt.Errorf("%w: ESI=%d", ErrDecoderDuplicateConflict, esi)
		}
		return decoder.complete, nil
	}
	if decoder.terminalConflict {
		return false, ErrDecoderConflict
	}
	if decoder.complete {
		return true, ErrDecoderComplete
	}
	if decoder.retained == decoder.limits.MaxRetainedSymbols {
		return false, fmt.Errorf("%w: %d", ErrDecoderRetainedLimit, decoder.retained)
	}
	if decoder.retained+1 >= decoder.k && decoder.attempts == decoder.limits.MaxDecodeAttempts {
		return false, fmt.Errorf("%w: %d", ErrDecoderAttemptLimit, decoder.attempts)
	}

	index := decoder.retained
	if err := decoder.payloads.SetSymbol(index, payload); err != nil {
		return false, err
	}
	decoder.esis[index] = esi
	decoder.retained++
	if decoder.retained < decoder.k {
		return false, nil
	}
	decoder.attempts++
	intermediate, err := decoder.solveRetained()
	if err != nil {
		if errors.Is(err, ErrInactivationRank) || errors.Is(err, ErrDecoderComplexity) {
			return false, nil
		}
		if errors.Is(err, ErrInactivationConflict) {
			decoder.terminalConflict = true
			return false, fmt.Errorf("%w: %w", ErrDecoderConflict, err)
		}
		return false, err
	}
	for source := uint32(0); source < decoder.k; source++ {
		destination, symbolErr := decoder.decoded.Symbol(source)
		if symbolErr != nil {
			return false, symbolErr
		}
		if symbolErr = encodingSymbolInto(decoder.parameters.KPrime, source, intermediate, destination); symbolErr != nil {
			return false, symbolErr
		}
	}
	decoder.complete = true
	return true, nil
}

// Result returns a caller-owned copy of all K canonical source symbols.
func (decoder *SourceBlockDecoder) Result(maxBytes uint64) (*gf256.Symbols, error) {
	if decoder == nil || !decoder.complete {
		return nil, ErrDecoderNotComplete
	}
	required := uint64(decoder.k) * uint64(decoder.symbolSize)
	if required > maxBytes {
		return nil, fmt.Errorf("%w: result needs %d bytes (limit %d)", ErrDecoderBacking, required, maxBytes)
	}
	result, err := gf256.NewSymbols(decoder.k, decoder.symbolSize, required)
	if err != nil {
		return nil, fmt.Errorf("%w: result: %w", ErrDecoderBacking, err)
	}
	for index := uint32(0); index < decoder.k; index++ {
		values, symbolErr := decoder.decoded.Symbol(index)
		if symbolErr != nil {
			return nil, symbolErr
		}
		if symbolErr = result.SetSymbol(index, values); symbolErr != nil {
			return nil, symbolErr
		}
	}
	return result, nil
}

func (decoder *SourceBlockDecoder) solveRetained() (*gf256.Symbols, error) {
	padding := decoder.parameters.KPrime - decoder.k
	encodedCount := padding + decoder.retained
	planBytes, err := InactivationPlanBackingBytes(decoder.parameters.KPrime, encodedCount)
	if err != nil {
		return nil, fmt.Errorf("%w: attempt plan: %w", ErrDecoderBacking, err)
	}
	attemptPrefix, err := checkedBackingSum(ErrDecoderBacking, uint64(encodedCount)*4, planBytes)
	if err != nil {
		return nil, err
	}
	if decoder.persistentBytes > decoder.limits.MaxWorkingMemory ||
		attemptPrefix > decoder.limits.MaxWorkingMemory-decoder.persistentBytes {
		return nil, fmt.Errorf("%w: attempt planning exceeds limit", ErrDecoderBacking)
	}
	isis := make([]uint32, int(encodedCount))
	for index := uint32(0); index < padding; index++ {
		isis[index] = decoder.k + index
	}
	for index := uint32(0); index < decoder.retained; index++ {
		isi, err := rfc6330.InternalSymbolID(decoder.k, decoder.esis[index])
		if err != nil {
			return nil, fmt.Errorf("%w: ESI %d: %w", ErrDecoderSymbol, decoder.esis[index], err)
		}
		isis[padding+index] = isi
	}
	plan, err := PlanReceivedInactivation(decoder.parameters.KPrime, isis, planBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: received plan: %w", ErrDecoderBacking, err)
	}
	residualColumns := decoder.parameters.L() - plan.finalI
	if residualColumns > decoder.limits.MaxDenseCompletionSymbols {
		return nil, fmt.Errorf(
			"%w: need %d symbols (limit %d)",
			ErrDecoderComplexity, residualColumns, decoder.limits.MaxDenseCompletionSymbols,
		)
	}
	dBytes := uint64(plan.totalRows) * uint64(decoder.symbolSize)
	executionBytes, err := InactivationExecutionBackingBytes(plan, decoder.symbolSize)
	if err != nil {
		return nil, fmt.Errorf("%w: attempt execution: %w", ErrDecoderBacking, err)
	}
	attemptBytes, err := checkedBackingSum(ErrDecoderBacking, attemptPrefix, dBytes, executionBytes)
	if err != nil {
		return nil, err
	}
	if attemptBytes > decoder.limits.MaxWorkingMemory-decoder.persistentBytes {
		return nil, fmt.Errorf("%w: attempt needs %d bytes", ErrDecoderBacking, attemptBytes)
	}
	d, err := gf256.NewSymbols(plan.totalRows, decoder.symbolSize, dBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: D: %w", ErrDecoderBacking, err)
	}
	firstEncodedRow := decoder.parameters.S + decoder.parameters.H
	for index := uint32(0); index < decoder.retained; index++ {
		values, symbolErr := decoder.payloads.Symbol(index)
		if symbolErr != nil {
			return nil, symbolErr
		}
		if symbolErr = d.SetSymbol(firstEncodedRow+padding+index, values); symbolErr != nil {
			return nil, symbolErr
		}
	}
	return ExecuteInactivationPlan(plan, d, executionBytes)
}
