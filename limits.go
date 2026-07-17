package raptorgo

import (
	"fmt"
	"math"
)

const (
	// MaxTransferLength is the largest RFC 6330 transfer length F in bytes,
	// as corrected by verified RFC Editor erratum 5548.
	MaxTransferLength uint64 = 942_574_504_275
	// MaxSourceSymbolsPerBlock is K'_max, the largest number of source
	// symbols in one source block, from RFC 6330 Section 5.1.2.
	MaxSourceSymbolsPerBlock = 56_403
	// MaxIntermediateSymbols is the largest number of intermediate symbols
	// L that any source block can require, reached at K'_max.
	MaxIntermediateSymbols uint32 = 57_326
	// MaxEncodingSymbolID is the largest encoding symbol ID that fits the
	// 24-bit wire field from RFC 6330 Section 3.2.
	MaxEncodingSymbolID               = 16_777_215
	maxSymbolSize              uint16 = math.MaxUint16
	maxSourceBlocks            uint8  = math.MaxUint8
	maxSubBlocks               uint16 = math.MaxUint16
	maxSymbolAlignment         uint8  = math.MaxUint8
	defaultMaxSourceBlockBytes        = 64 << 20
	defaultMaxPacketBytes             = 4 << 20
	defaultMaxWorkingMemory           = 256 << 20
	defaultMaxSubBlocks        uint16 = 256
	defaultMaxSymbolsPerPacket uint32 = 4_096
	defaultMaxRetainedSymbols  uint32 = 65_536
	defaultMaxDecodeAttempts   uint32 = 64
	defaultMaxRepairSymbols    uint32 = 256
	defaultMaxDenseCompletion  uint32 = 2_048
)

// Limits bounds all sizes controlled by OTI or packet data before allocation.
// Every allocation-sensitive operation validates externally controlled input
// against a Limits value taken as an argument or retained from construction.
// Every field must be nonzero; see Validate.
type Limits struct {
	// MaxTransferLength bounds OTI.TransferLength (F) in bytes; it may not
	// exceed the RFC ceiling MaxTransferLength.
	MaxTransferLength uint64
	// MaxSymbolSize bounds OTI.SymbolSize (T) in bytes.
	MaxSymbolSize uint16
	// MaxSourceBlocks bounds OTI.SourceBlocks (Z).
	MaxSourceBlocks uint8
	// MaxSubBlocks bounds OTI.SubBlocks (N).
	MaxSubBlocks uint16
	// MaxSymbolAlignment bounds OTI.SymbolAlignment (Al) in bytes.
	MaxSymbolAlignment uint8
	// MaxSourceSymbolsPerBlock bounds K, the source symbols in any single
	// source block; it may not exceed K'_max (MaxSourceSymbolsPerBlock).
	MaxSourceSymbolsPerBlock uint32
	// MaxSourceBlockBytes bounds K*T, the size in bytes of any single
	// source block.
	MaxSourceBlockBytes uint64
	// MaxPacketBytes bounds the encoded size in bytes of one encoding
	// packet, including its FEC Payload ID.
	MaxPacketBytes uint64
	// MaxSymbolsPerPacket bounds G, the symbols grouped into one packet.
	MaxSymbolsPerPacket uint32
	// MaxRetainedSymbols bounds the source plus repair symbols one block
	// decoder retains at once. It must be at least K for every block the
	// OTI derives, or NewObjectDecoder fails.
	MaxRetainedSymbols uint32
	// MaxDecodeAttempts bounds bounded decode attempts per source block
	// before the decoder gives up.
	MaxDecodeAttempts uint32
	// MaxRepairSymbolsPerBlock bounds the repair symbols retained per source
	// block beyond its K source symbols.
	MaxRepairSymbolsPerBlock uint32
	// MaxDenseCompletionSymbols bounds the dense-completion stage of the
	// hybrid solver, at most MaxIntermediateSymbols. It must be at least P,
	// the block's PI symbol count, or NewObjectDecoder fails.
	MaxDenseCompletionSymbols uint32
	// MaxWorkingMemory bounds the peak logical backing bytes of any single
	// encoder or decoder operation, computed before allocation.
	MaxWorkingMemory uint64
}

// DefaultLimits returns conservative production defaults. They intentionally
// reject some syntactically valid, extremely large RFC configurations.
// Applications should lower these defaults when their workload permits it.
func DefaultLimits() Limits {
	return Limits{
		MaxTransferLength:         MaxTransferLength,
		MaxSymbolSize:             maxSymbolSize,
		MaxSourceBlocks:           maxSourceBlocks,
		MaxSubBlocks:              defaultMaxSubBlocks,
		MaxSymbolAlignment:        maxSymbolAlignment,
		MaxSourceSymbolsPerBlock:  MaxSourceSymbolsPerBlock,
		MaxSourceBlockBytes:       defaultMaxSourceBlockBytes,
		MaxPacketBytes:            defaultMaxPacketBytes,
		MaxSymbolsPerPacket:       defaultMaxSymbolsPerPacket,
		MaxRetainedSymbols:        defaultMaxRetainedSymbols,
		MaxDecodeAttempts:         defaultMaxDecodeAttempts,
		MaxRepairSymbolsPerBlock:  defaultMaxRepairSymbols,
		MaxDenseCompletionSymbols: defaultMaxDenseCompletion,
		MaxWorkingMemory:          defaultMaxWorkingMemory,
	}
}

// RFCWireLimits admits every wire value allowed by RFC 6330. It is useful for
// serialization and conformance tests, not as an allocation policy.
func RFCWireLimits() Limits {
	return Limits{
		MaxTransferLength:         MaxTransferLength,
		MaxSymbolSize:             maxSymbolSize,
		MaxSourceBlocks:           maxSourceBlocks,
		MaxSubBlocks:              maxSubBlocks,
		MaxSymbolAlignment:        maxSymbolAlignment,
		MaxSourceSymbolsPerBlock:  MaxSourceSymbolsPerBlock,
		MaxSourceBlockBytes:       uint64(maxSymbolSize) * uint64(MaxSourceSymbolsPerBlock),
		MaxPacketBytes:            math.MaxUint64,
		MaxSymbolsPerPacket:       MaxEncodingSymbolID + 1,
		MaxRetainedSymbols:        MaxEncodingSymbolID + 1,
		MaxDecodeAttempts:         math.MaxUint32,
		MaxRepairSymbolsPerBlock:  MaxEncodingSymbolID + 1,
		MaxDenseCompletionSymbols: MaxIntermediateSymbols,
		MaxWorkingMemory:          math.MaxUint64,
	}
}

// Validate checks that every field is nonzero and within the RFC 6330 wire
// domain. It returns an error wrapping ErrInvalidLimits that names the first
// offending field, or nil when all fields are usable.
func (limits Limits) Validate() error {
	var field string
	switch {
	case limits.MaxTransferLength == 0 || limits.MaxTransferLength > MaxTransferLength:
		field = "MaxTransferLength"
	case limits.MaxSymbolSize == 0:
		field = "MaxSymbolSize"
	case limits.MaxSourceBlocks == 0:
		field = "MaxSourceBlocks"
	case limits.MaxSubBlocks == 0:
		field = "MaxSubBlocks"
	case limits.MaxSymbolAlignment == 0:
		field = "MaxSymbolAlignment"
	case limits.MaxSourceSymbolsPerBlock == 0 || limits.MaxSourceSymbolsPerBlock > MaxSourceSymbolsPerBlock:
		field = "MaxSourceSymbolsPerBlock"
	case limits.MaxSourceBlockBytes == 0:
		field = "MaxSourceBlockBytes"
	case limits.MaxPacketBytes < PayloadIDEncodedSize+1:
		field = "MaxPacketBytes"
	case limits.MaxSymbolsPerPacket == 0:
		field = "MaxSymbolsPerPacket"
	case limits.MaxRetainedSymbols == 0:
		field = "MaxRetainedSymbols"
	case limits.MaxDecodeAttempts == 0:
		field = "MaxDecodeAttempts"
	case limits.MaxRepairSymbolsPerBlock == 0:
		field = "MaxRepairSymbolsPerBlock"
	case limits.MaxDenseCompletionSymbols == 0 || limits.MaxDenseCompletionSymbols > MaxIntermediateSymbols:
		field = "MaxDenseCompletionSymbols"
	case limits.MaxWorkingMemory == 0:
		field = "MaxWorkingMemory"
	default:
		return nil
	}
	return fmt.Errorf("%w: %s is zero, out-of-range, or unsafe", ErrInvalidLimits, field)
}
