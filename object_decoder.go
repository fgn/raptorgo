package raptorgo

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/fgn/raptorgo/internal/codec"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

// ObjectDecoderState makes the authenticated-delivery boundary explicit.
// Once a decoder leaves ObjectReceiving it never accepts another packet;
// AddPacket then fails with ErrObjectTerminal.
type ObjectDecoderState uint8

const (
	// ObjectReceiving is the initial state: the decoder accepts packets and
	// has not yet decoded every nonempty source block.
	ObjectReceiving ObjectDecoderState = iota
	// ObjectDecodeFailed is terminal: FEC decoding or reconstruction failed
	// permanently.
	ObjectDecodeFailed
	// ObjectDecoded is a transient finalization state: every nonempty source
	// block decoded and the reconstructed object awaits verification.
	ObjectDecoded
	// ObjectVerified is a transient finalization state: the verifier accepted
	// the reconstructed object and delivery is about to run.
	ObjectVerified
	// ObjectDelivered is terminal: the deliverer received the verified object
	// exactly once and accepted it.
	ObjectDelivered
	// ObjectVerificationFailed is terminal: the application verifier rejected
	// the reconstructed object.
	ObjectVerificationFailed
	// ObjectDeliveryFailed is terminal: the application deliverer rejected
	// the verified object.
	ObjectDeliveryFailed
)

// String returns a short lower-case name for the state, such as "receiving",
// "delivered", or "verification-failed".
func (state ObjectDecoderState) String() string {
	switch state {
	case ObjectReceiving:
		return "receiving"
	case ObjectDecoded:
		return "decoded"
	case ObjectDecodeFailed:
		return "decode-failed"
	case ObjectVerified:
		return "verified"
	case ObjectDelivered:
		return "delivered"
	case ObjectVerificationFailed:
		return "verification-failed"
	case ObjectDeliveryFailed:
		return "delivery-failed"
	default:
		return "unknown"
	}
}

// VerificationRequest binds reconstructed bytes to their OTI and an
// application-supplied generation/context identifier. Object is read-only.
type VerificationRequest struct {
	// OTI describes the reconstructed object being verified.
	OTI OTI
	// Context is the identifier supplied to NewObjectDecoder, so one shared
	// verifier can distinguish concurrent transfers.
	Context string
	// Length equals OTI.TransferLength and exists for convenience.
	Length uint64
	// Object streams the reconstructed bytes. It is valid only for the
	// duration of the verifier call and must not be retained.
	Object io.Reader
}

// VerifiedObject is passed exactly once to the delivery callback after its
// corresponding VerificationRequest succeeds. Bytes are owned by the callback.
type VerifiedObject struct {
	// OTI describes the delivered object.
	OTI OTI
	// Context is the identifier supplied to NewObjectDecoder.
	Context string
	// Bytes is an owned copy of the verified object, exactly
	// OTI.TransferLength long; the callback may retain and mutate it.
	Bytes []byte
}

type (
	// ObjectVerifier authenticates a reconstructed object before delivery.
	// Returning a non-nil error rejects the object and moves the decoder to
	// ObjectVerificationFailed; the deliverer is then never invoked.
	ObjectVerifier func(VerificationRequest) error
	// ObjectDeliverer receives the reconstructed object exactly once, after
	// the verifier has accepted it. Returning a non-nil error moves the
	// decoder to ObjectDeliveryFailed.
	ObjectDeliverer func(VerifiedObject) error
)

// ObjectDecoder incrementally decodes every nonempty source block and enforces
// decoded -> verified -> delivered ordering. FEC success alone never invokes
// delivery. It is safe for concurrent method calls. Callbacks run without the
// decoder mutex held, so they may inspect or call the decoder; packet additions
// made after finalization starts are rejected as terminal.
type ObjectDecoder struct {
	mu              sync.Mutex
	oti             OTI
	limits          Limits
	context         string
	verify          ObjectVerifier
	deliver         ObjectDeliverer
	blocks          []*codec.SourceBlockDecoder
	nonemptyBlocks  uint32
	completedBlocks uint32
	persistentBytes uint64
	state           ObjectDecoderState
}

type objectBlockDecoderBudget struct {
	k, retained, dense  uint32
	persistent, attempt uint64
}

type objectDecoderBudget struct {
	blocks          []objectBlockDecoderBudget
	nonemptyBlocks  uint32
	persistentBytes uint64
	peakBytes       uint64
}

// ObjectDecoderWorkspaceBackingBytes returns the conservative peak logical
// backing reserved by NewObjectDecoder and all later operations under limits.
// It includes persistent block decoders, the largest bounded decode attempt,
// reconstruction, and one owned delivery copy.
func ObjectDecoderWorkspaceBackingBytes(oti OTI, limits Limits) (uint64, error) {
	budget, err := planObjectDecoderBacking(oti, limits)
	if err != nil {
		return 0, err
	}
	return budget.peakBytes, nil
}

// NewObjectDecoder preflights all persistent block decoders, their largest
// bounded attempt, reconstruction, verification, and one owned delivery copy
// before allocating retained packet storage.
//
// A zero-length object (OTI.TransferLength 0) has no nonempty source blocks
// and therefore needs no packets: verify and deliver run synchronously on the
// empty object before NewObjectDecoder returns. In that fast path the decoder
// is returned non-nil even on failure, in the corresponding terminal state,
// together with an error wrapping ErrObjectVerification or ErrObjectDelivery;
// on success it is returned already in the ObjectDelivered state.
func NewObjectDecoder(
	oti OTI,
	limits Limits,
	context string,
	verify ObjectVerifier,
	deliver ObjectDeliverer,
) (*ObjectDecoder, error) {
	if err := oti.Validate(limits); err != nil {
		return nil, err
	}
	if verify == nil || deliver == nil {
		return nil, fmt.Errorf("%w: verifier and deliverer are required", ErrObjectDecoder)
	}
	budgetPlan, err := planObjectDecoderBacking(oti, limits)
	if err != nil {
		return nil, err
	}
	if budgetPlan.peakBytes > limits.MaxWorkingMemory {
		return nil, fmt.Errorf(
			"%w: object decoder needs %d bytes (limit %d)",
			ErrLimitExceeded, budgetPlan.peakBytes, limits.MaxWorkingMemory,
		)
	}
	blocks := make([]*codec.SourceBlockDecoder, int(oti.SourceBlocks))
	for sourceBlockIndex := 0; sourceBlockIndex < int(oti.SourceBlocks); sourceBlockIndex++ {
		sourceBlock := uint8(sourceBlockIndex)
		budget := budgetPlan.blocks[sourceBlock]
		if budget.k == 0 {
			continue
		}
		working, err := checkedObjectBacking(budget.persistent, budget.attempt)
		if err != nil {
			return nil, err
		}
		block, err := codec.NewSourceBlockDecoder(budget.k, uint32(oti.SymbolSize), codec.SourceBlockDecoderLimits{
			MaxRetainedSymbols:        budget.retained,
			MaxDecodeAttempts:         limits.MaxDecodeAttempts,
			MaxDenseCompletionSymbols: budget.dense,
			MaxWorkingMemory:          working,
		})
		if err != nil {
			return nil, fmt.Errorf("%w: SBN %d: %w", ErrObjectDecoder, sourceBlock, err)
		}
		blocks[sourceBlock] = block
	}
	decoder := &ObjectDecoder{
		oti: oti, limits: limits, context: context, verify: verify, deliver: deliver,
		blocks: blocks, nonemptyBlocks: budgetPlan.nonemptyBlocks, persistentBytes: budgetPlan.persistentBytes,
		state: ObjectReceiving,
	}
	if budgetPlan.nonemptyBlocks == 0 {
		decoder.mu.Lock()
		candidate, err := decoder.beginFinalizationLocked()
		decoder.mu.Unlock()
		if err != nil {
			return decoder, err
		}
		if err := decoder.verifyAndDeliver(candidate); err != nil {
			return decoder, err
		}
	}
	return decoder, nil
}

func planObjectDecoderBacking(oti OTI, limits Limits) (objectDecoderBudget, error) {
	if err := oti.Validate(limits); err != nil {
		return objectDecoderBudget{}, err
	}
	result := objectDecoderBudget{blocks: make([]objectBlockDecoderBudget, int(oti.SourceBlocks))}
	var maximumAttempt, maximumBlockBytes uint64
	for sourceBlockIndex := 0; sourceBlockIndex < int(oti.SourceBlocks); sourceBlockIndex++ {
		sourceBlock := uint8(sourceBlockIndex)
		k, err := SourceBlockSymbols(oti, sourceBlock)
		if err != nil {
			return objectDecoderBudget{}, err
		}
		if k == 0 {
			continue
		}
		parameters, err := rfc6330.ParametersForSourceSymbols(k)
		if err != nil {
			return objectDecoderBudget{}, err
		}
		retained64 := uint64(k) + uint64(limits.MaxRepairSymbolsPerBlock)
		if retained64 > uint64(limits.MaxRetainedSymbols) {
			retained64 = uint64(limits.MaxRetainedSymbols)
		}
		if retained64 < uint64(k) {
			return objectDecoderBudget{}, fmt.Errorf(
				"%w: SBN %d retained cap is below K=%d", ErrLimitExceeded, sourceBlock, k,
			)
		}
		retained := uint32(retained64)
		dense := min(limits.MaxDenseCompletionSymbols, parameters.L())
		if dense < parameters.P() {
			return objectDecoderBudget{}, fmt.Errorf(
				"%w: SBN %d dense cap %d is below P=%d", ErrLimitExceeded, sourceBlock, dense, parameters.P(),
			)
		}
		persistent, err := codec.SourceBlockDecoderBackingBytes(k, uint32(oti.SymbolSize), retained)
		if err != nil {
			return objectDecoderBudget{}, fmt.Errorf("%w: SBN %d persistent: %w", ErrObjectDecoder, sourceBlock, err)
		}
		attempt, err := codec.SourceBlockDecoderWorstAttemptBackingBytes(k, uint32(oti.SymbolSize), retained, dense)
		if err != nil {
			return objectDecoderBudget{}, fmt.Errorf("%w: SBN %d attempt: %w", ErrObjectDecoder, sourceBlock, err)
		}
		result.persistentBytes, err = checkedObjectBacking(result.persistentBytes, persistent)
		if err != nil {
			return objectDecoderBudget{}, err
		}
		maximumAttempt = max(maximumAttempt, attempt)
		maximumBlockBytes = max(maximumBlockBytes, uint64(k)*uint64(oti.SymbolSize))
		result.blocks[sourceBlock] = objectBlockDecoderBudget{
			k: k, retained: retained, dense: dense, persistent: persistent, attempt: attempt,
		}
		result.nonemptyBlocks++
	}
	decodePeak, err := checkedObjectBacking(result.persistentBytes, maximumAttempt, uint64(oti.SymbolSize))
	if err != nil {
		return objectDecoderBudget{}, err
	}
	reconstructPeak, err := checkedObjectBacking(result.persistentBytes, oti.TransferLength, maximumBlockBytes)
	if err != nil {
		return objectDecoderBudget{}, err
	}
	deliveryPeak, err := checkedObjectBacking(result.persistentBytes, oti.TransferLength, oti.TransferLength)
	if err != nil {
		return objectDecoderBudget{}, err
	}
	result.peakBytes = max(decodePeak, reconstructPeak, deliveryPeak)
	return result, nil
}

// State reports the decoder's current lifecycle state. State on a nil decoder
// reports ObjectDeliveryFailed.
func (decoder *ObjectDecoder) State() ObjectDecoderState {
	if decoder == nil {
		return ObjectDeliveryFailed
	}
	decoder.mu.Lock()
	defer decoder.mu.Unlock()
	return decoder.state
}

// CompletedBlocks reports how many nonempty source blocks have fully decoded
// so far. CompletedBlocks on a nil decoder reports 0.
func (decoder *ObjectDecoder) CompletedBlocks() uint32 {
	if decoder == nil {
		return 0
	}
	decoder.mu.Lock()
	defer decoder.mu.Unlock()
	return decoder.completedBlocks
}

// PersistentBackingBytes reports the logical backing bytes reserved by the
// persistent per-block decoders for the lifetime of this decoder, excluding
// transient decode-attempt and reconstruction peaks. PersistentBackingBytes
// on a nil decoder reports 0.
func (decoder *ObjectDecoder) PersistentBackingBytes() uint64 {
	if decoder == nil {
		return 0
	}
	return decoder.persistentBytes
}

// AddPacket validates and copies every grouped symbol. Once all blocks decode,
// it synchronously verifies and invokes delivery exactly once. AddPacket on a
// nil decoder reports ObjectDeliveryFailed with an error wrapping
// ErrObjectDecoder.
func (decoder *ObjectDecoder) AddPacket(encoded []byte) (ObjectDecoderState, error) {
	if decoder == nil {
		return ObjectDeliveryFailed, fmt.Errorf("%w: nil decoder", ErrObjectDecoder)
	}
	decoder.mu.Lock()
	candidate, finalize, err := decoder.addPacketLocked(encoded)
	state := decoder.state
	decoder.mu.Unlock()
	if err != nil {
		return state, err
	}
	if finalize {
		if err := decoder.verifyAndDeliver(candidate); err != nil {
			return decoder.State(), err
		}
		return decoder.State(), nil
	}
	return state, nil
}

func (decoder *ObjectDecoder) addPacketLocked(encoded []byte) ([]byte, bool, error) {
	if decoder.state != ObjectReceiving {
		return nil, false, fmt.Errorf("%w: %s", ErrObjectTerminal, decoder.state)
	}
	packet, err := ParseEncodingPacket(encoded, decoder.oti, decoder.limits)
	if err != nil {
		return nil, false, err
	}
	id := packet.PayloadID()
	block := decoder.blocks[id.SourceBlockNumber]
	if block == nil {
		return nil, false, fmt.Errorf("%w: packet targets empty SBN %d", ErrInvalidPacket, id.SourceBlockNumber)
	}
	canonical := make([]byte, int(decoder.oti.SymbolSize))
	for index := uint32(0); index < packet.SymbolCount(); index++ {
		if block.Complete() {
			break
		}
		if err := packet.CopySymbol(index, canonical); err != nil {
			return nil, false, err
		}
		complete, err := block.AddSymbol(id.EncodingSymbolID+index, canonical)
		if err != nil {
			if !errors.Is(err, codec.ErrDecoderDuplicateConflict) && !errors.Is(err, codec.ErrDecoderSymbol) {
				decoder.state = ObjectDecodeFailed
			}
			return nil, false, fmt.Errorf("%w: SBN %d ESI %d: %w", ErrObjectDecoder, id.SourceBlockNumber, id.EncodingSymbolID+index, err)
		}
		if complete {
			decoder.completedBlocks++
		}
	}
	if decoder.completedBlocks == decoder.nonemptyBlocks {
		candidate, err := decoder.beginFinalizationLocked()
		if err != nil {
			return nil, false, err
		}
		return candidate, true, nil
	}
	return nil, false, nil
}

func (decoder *ObjectDecoder) beginFinalizationLocked() ([]byte, error) {
	candidate, err := decoder.reconstructLocked()
	if err != nil {
		decoder.state = ObjectDecodeFailed
		return nil, err
	}
	decoder.state = ObjectDecoded
	return candidate, nil
}

func (decoder *ObjectDecoder) verifyAndDeliver(candidate []byte) error {
	request := VerificationRequest{
		OTI: decoder.oti, Context: decoder.context, Length: decoder.oti.TransferLength,
		Object: bytes.NewReader(candidate),
	}
	if err := decoder.verify(request); err != nil {
		decoder.mu.Lock()
		decoder.state = ObjectVerificationFailed
		decoder.mu.Unlock()
		return fmt.Errorf("%w: %w", ErrObjectVerification, err)
	}
	decoder.mu.Lock()
	decoder.state = ObjectVerified
	decoder.mu.Unlock()
	delivery := VerifiedObject{
		OTI: decoder.oti, Context: decoder.context, Bytes: append([]byte(nil), candidate...),
	}
	if err := decoder.deliver(delivery); err != nil {
		decoder.mu.Lock()
		decoder.state = ObjectDeliveryFailed
		decoder.mu.Unlock()
		return fmt.Errorf("%w: %w", ErrObjectDelivery, err)
	}
	decoder.mu.Lock()
	decoder.state = ObjectDelivered
	decoder.mu.Unlock()
	return nil
}

func (decoder *ObjectDecoder) reconstructLocked() ([]byte, error) {
	if decoder.completedBlocks != decoder.nonemptyBlocks {
		return nil, fmt.Errorf("%w: reconstruction before all blocks complete", ErrObjectDecoder)
	}
	candidate := make([]byte, int(decoder.oti.TransferLength))
	totalSymbols := ceilDiv(decoder.oti.TransferLength, uint64(decoder.oti.SymbolSize))
	blockPartition, err := rfc6330.Partition(totalSymbols, uint64(decoder.oti.SourceBlocks))
	if err != nil {
		return nil, err
	}
	for sourceBlockIndex := 0; sourceBlockIndex < int(decoder.oti.SourceBlocks); sourceBlockIndex++ {
		sourceBlock := uint8(sourceBlockIndex)
		block := decoder.blocks[sourceBlock]
		if block == nil {
			continue
		}
		k := block.SourceSymbols()
		result, err := block.Result(uint64(k) * uint64(decoder.oti.SymbolSize))
		if err != nil {
			return nil, fmt.Errorf("%w: SBN %d result: %w", ErrObjectDecoder, sourceBlock, err)
		}
		firstSymbol := sourceBlockFirstSymbol(blockPartition, uint64(sourceBlock))
		blockStart := firstSymbol * uint64(decoder.oti.SymbolSize)
		if decoder.oti.SubBlocks == 1 {
			for symbol := uint32(0); symbol < k; symbol++ {
				values, symbolErr := result.Symbol(symbol)
				if symbolErr != nil {
					return nil, symbolErr
				}
				copyCandidateRange(candidate, blockStart+uint64(symbol)*uint64(decoder.oti.SymbolSize), values)
			}
			continue
		}
		subSymbolUnits := uint64(decoder.oti.SymbolSize) / uint64(decoder.oti.SymbolAlignment)
		subBlocks, err := rfc6330.Partition(subSymbolUnits, uint64(decoder.oti.SubBlocks))
		if err != nil {
			return nil, err
		}
		outputOffset, symbolOffset := blockStart, uint64(0)
		for subBlock := uint64(0); subBlock < uint64(decoder.oti.SubBlocks); subBlock++ {
			units, sizeErr := subBlocks.Size(subBlock)
			if sizeErr != nil {
				return nil, sizeErr
			}
			size := units * uint64(decoder.oti.SymbolAlignment)
			for symbol := uint32(0); symbol < k; symbol++ {
				values, symbolErr := result.Symbol(symbol)
				if symbolErr != nil {
					return nil, symbolErr
				}
				copyCandidateRange(candidate, outputOffset, values[int(symbolOffset):int(symbolOffset+size)])
				outputOffset += size
			}
			symbolOffset += size
		}
	}
	return candidate, nil
}

func copyCandidateRange(candidate []byte, start uint64, values []byte) {
	if start >= uint64(len(candidate)) {
		return
	}
	copy(candidate[int(start):], values)
}
