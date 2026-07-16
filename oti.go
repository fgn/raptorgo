package raptorgo

import (
	"encoding/binary"
	"fmt"

	"github.com/fgn/raptorgo/internal/rfc6330"
)

// OTIEncodedSize is the encoded size in bytes of the combined RFC 6330
// Common and Scheme-Specific FEC Object Transmission Information produced by
// OTI.MarshalBinary and consumed by ParseOTI.
const OTIEncodedSize = 12

// OTI is the RFC 6330 FEC Object Transmission Information.
type OTI struct {
	TransferLength  uint64
	SymbolSize      uint16
	SourceBlocks    uint8
	SubBlocks       uint16
	SymbolAlignment uint8
}

// DerivationInput contains the inputs to the RFC 6330 Section 4.3 example
// parameter derivation. MaxPayloadSize is P', DecoderBlockBytes is WS, and
// MinSubSymbolAlignmentUnits is SS. P' must be a multiple of SymbolAlignment.
type DerivationInput struct {
	TransferLength             uint64
	DecoderBlockBytes          uint64
	MaxPayloadSize             uint16
	SymbolAlignment            uint8
	MinSubSymbolAlignmentUnits uint32
}

// DeriveOTI applies the RFC 6330 Section 4.3 example algorithm. Applications
// may instead supply an OTI selected by their content-delivery protocol.
func DeriveOTI(input DerivationInput, limits Limits) (OTI, error) {
	if err := limits.Validate(); err != nil {
		return OTI{}, err
	}
	if input.TransferLength > MaxTransferLength || input.DecoderBlockBytes == 0 ||
		input.MaxPayloadSize == 0 || input.SymbolAlignment == 0 ||
		input.MaxPayloadSize%uint16(input.SymbolAlignment) != 0 ||
		input.MinSubSymbolAlignmentUnits == 0 {
		return OTI{}, fmt.Errorf("%w: %+v", ErrInvalidDerivation, input)
	}
	minimumSubSymbolBytes := uint64(input.MinSubSymbolAlignmentUnits) * uint64(input.SymbolAlignment)
	if minimumSubSymbolBytes == 0 || minimumSubSymbolBytes > uint64(input.MaxPayloadSize) {
		return OTI{}, fmt.Errorf("%w: SS*Al=%d exceeds P'=%d", ErrInvalidDerivation, minimumSubSymbolBytes, input.MaxPayloadSize)
	}
	nMax := uint64(input.MaxPayloadSize) / minimumSubSymbolBytes
	if nMax == 0 || nMax > uint64(maxSubBlocks) {
		return OTI{}, fmt.Errorf("%w: N_max=%d", ErrInvalidDerivation, nMax)
	}
	kl := func(n uint64) uint32 {
		alignmentUnits := uint64(input.MaxPayloadSize) / uint64(input.SymbolAlignment)
		subSymbolBytes := uint64(input.SymbolAlignment) * ceilDiv(alignmentUnits, n)
		return rfc6330.LargestExtendedSourceBlockAtMost(input.DecoderBlockBytes / subSymbolBytes)
	}
	maximumKL := kl(nMax)
	if maximumKL == 0 {
		return OTI{}, fmt.Errorf("%w: WS cannot hold the smallest Table 2 block", ErrInvalidDerivation)
	}
	totalSymbols := ceilDiv(input.TransferLength, uint64(input.MaxPayloadSize))
	sourceBlocks := max(uint64(1), ceilDiv(totalSymbols, uint64(maximumKL)))
	if sourceBlocks > uint64(maxSourceBlocks) {
		return OTI{}, fmt.Errorf("%w: derived Z=%d", ErrInvalidDerivation, sourceBlocks)
	}
	targetBlockSymbols := ceilDiv(totalSymbols, sourceBlocks)
	derivedN := uint64(0)
	for n := uint64(1); n <= nMax; n++ {
		if uint64(kl(n)) >= targetBlockSymbols {
			derivedN = n
			break
		}
	}
	if derivedN == 0 {
		return OTI{}, fmt.Errorf("%w: no eligible N <= %d", ErrInvalidDerivation, nMax)
	}
	oti := OTI{
		TransferLength: input.TransferLength, SymbolSize: input.MaxPayloadSize,
		SourceBlocks: uint8(sourceBlocks), SubBlocks: uint16(derivedN),
		SymbolAlignment: input.SymbolAlignment,
	}
	if err := oti.Validate(limits); err != nil {
		return OTI{}, err
	}
	return oti, nil
}

// Validate checks RFC constraints and application resource limits before any
// object, symbol, or matrix allocation.
func (oti OTI) Validate(limits Limits) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	if oti.TransferLength > MaxTransferLength {
		return fmt.Errorf("%w: transfer length %d exceeds the RFC maximum", ErrInvalidOTI, oti.TransferLength)
	}
	if oti.SymbolSize == 0 {
		return fmt.Errorf("%w: symbol size %d", ErrInvalidOTI, oti.SymbolSize)
	}
	if oti.SourceBlocks == 0 {
		return fmt.Errorf("%w: source blocks %d", ErrInvalidOTI, oti.SourceBlocks)
	}
	if oti.SubBlocks == 0 {
		return fmt.Errorf("%w: sub-blocks %d", ErrInvalidOTI, oti.SubBlocks)
	}
	if oti.SymbolAlignment == 0 || oti.SymbolSize%uint16(oti.SymbolAlignment) != 0 {
		return fmt.Errorf("%w: symbol alignment %d", ErrInvalidOTI, oti.SymbolAlignment)
	}

	totalSymbols := ceilDiv(oti.TransferLength, uint64(oti.SymbolSize))
	maxBlockSymbols := ceilDiv(totalSymbols, uint64(oti.SourceBlocks))
	if maxBlockSymbols > uint64(MaxSourceSymbolsPerBlock) {
		return fmt.Errorf("%w: source block requires %d symbols", ErrInvalidOTI, maxBlockSymbols)
	}

	if oti.TransferLength > limits.MaxTransferLength {
		return fmt.Errorf("%w: transfer length %d", ErrLimitExceeded, oti.TransferLength)
	}
	if oti.SymbolSize > limits.MaxSymbolSize {
		return fmt.Errorf("%w: symbol size %d", ErrLimitExceeded, oti.SymbolSize)
	}
	if oti.SourceBlocks > limits.MaxSourceBlocks {
		return fmt.Errorf("%w: source blocks %d", ErrLimitExceeded, oti.SourceBlocks)
	}
	if oti.SubBlocks > limits.MaxSubBlocks {
		return fmt.Errorf("%w: sub-blocks %d", ErrLimitExceeded, oti.SubBlocks)
	}
	if oti.SymbolAlignment > limits.MaxSymbolAlignment {
		return fmt.Errorf("%w: symbol alignment %d", ErrLimitExceeded, oti.SymbolAlignment)
	}
	if maxBlockSymbols > uint64(limits.MaxSourceSymbolsPerBlock) {
		return fmt.Errorf("%w: source block requires %d symbols", ErrLimitExceeded, maxBlockSymbols)
	}
	blockBytes := maxBlockSymbols * uint64(oti.SymbolSize)
	if blockBytes > limits.MaxSourceBlockBytes {
		return fmt.Errorf("%w: source block requires %d bytes", ErrLimitExceeded, blockBytes)
	}
	return nil
}

// MarshalBinary encodes the OTI into its OTIEncodedSize-byte RFC 6330 wire
// form. It validates against RFCWireLimits, not caller limits, so every
// wire-legal OTI can be serialized. ParseOTI is the limits-aware inverse;
// there is deliberately no UnmarshalBinary, so parsing untrusted OTI bytes
// always takes explicit Limits.
func (oti OTI) MarshalBinary() ([]byte, error) {
	if err := oti.Validate(RFCWireLimits()); err != nil {
		return nil, err
	}
	encoded := make([]byte, OTIEncodedSize)
	encoded[0] = byte(oti.TransferLength >> 32)
	binary.BigEndian.PutUint32(encoded[1:5], uint32(oti.TransferLength))
	// encoded[5] is reserved and remains zero.
	binary.BigEndian.PutUint16(encoded[6:8], oti.SymbolSize)
	encoded[8] = oti.SourceBlocks
	binary.BigEndian.PutUint16(encoded[9:11], oti.SubBlocks)
	encoded[11] = oti.SymbolAlignment
	return encoded, nil
}

// ParseOTI decodes an OTIEncodedSize-byte RFC 6330 encoded OTI and validates
// the result against both RFC constraints and the supplied limits before
// returning it.
func ParseOTI(encoded []byte, limits Limits) (OTI, error) {
	if len(encoded) != OTIEncodedSize {
		return OTI{}, fmt.Errorf("%w: encoded length %d", ErrInvalidOTI, len(encoded))
	}
	oti := OTI{
		TransferLength:  uint64(encoded[0])<<32 | uint64(binary.BigEndian.Uint32(encoded[1:5])),
		SymbolSize:      binary.BigEndian.Uint16(encoded[6:8]),
		SourceBlocks:    encoded[8],
		SubBlocks:       binary.BigEndian.Uint16(encoded[9:11]),
		SymbolAlignment: encoded[11],
	}
	if err := oti.Validate(limits); err != nil {
		return OTI{}, err
	}
	return oti, nil
}

func ceilDiv(value, divisor uint64) uint64 {
	if value == 0 {
		return 0
	}
	return 1 + (value-1)/divisor
}
