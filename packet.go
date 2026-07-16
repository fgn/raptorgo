package raptorgo

import (
	"fmt"

	"github.com/fgn/raptorgo/internal/rfc6330"
)

// EncodingPacketView is a validated, zero-copy view of a grouped RFC 6330
// source or repair packet. The caller must not mutate the input byte slice while
// using the view.
type EncodingPacketView struct {
	id          PayloadID
	payload     []byte
	symbolSize  uint16
	symbolCount uint32
	source      bool
}

// PayloadID returns the packet's parsed FEC Payload ID.
func (packet EncodingPacketView) PayloadID() PayloadID { return packet.id }

// Payload returns the symbol payload that follows the FEC Payload ID. The
// returned bytes alias the parsed input and must not be mutated.
func (packet EncodingPacketView) Payload() []byte { return packet.payload }

// SymbolCount returns the number of encoding symbols grouped in the packet.
func (packet EncodingPacketView) SymbolCount() uint32 { return packet.symbolCount }

// IsSource reports whether the packet carries source symbols rather than
// repair symbols.
func (packet EncodingPacketView) IsSource() bool { return packet.source }

// Symbol returns a read-only view of one encoding symbol within the payload.
// A legally shortened final source symbol is returned without its trailing
// zero padding, so the view may be shorter than the symbol size T; use
// CopySymbol to obtain the canonical T-byte form.
func (packet EncodingPacketView) Symbol(index uint32) ([]byte, error) {
	if index >= packet.symbolCount {
		return nil, fmt.Errorf("%w: symbol index %d", ErrInvalidPacket, index)
	}
	start := uint64(index) * uint64(packet.symbolSize)
	end := start + uint64(packet.symbolSize)
	if end > uint64(len(packet.payload)) {
		end = uint64(len(packet.payload))
	}
	return packet.payload[int(start):int(end):int(end)], nil
}

// CopySymbol copies a symbol into a canonical T-byte destination, adding the
// mandatory zero padding when the final source symbol was shortened on wire.
func (packet EncodingPacketView) CopySymbol(index uint32, destination []byte) error {
	if len(destination) != int(packet.symbolSize) {
		return fmt.Errorf("%w: destination has %d bytes, want %d", ErrInvalidPacket, len(destination), packet.symbolSize)
	}
	symbol, err := packet.Symbol(index)
	if err != nil {
		return err
	}
	clear(destination)
	copy(destination, symbol)
	return nil
}

// ParseEncodingPacket validates grouped-symbol semantics from RFC 6330
// Section 4.4.2, including the one permitted shortened final source symbol.
func ParseEncodingPacket(encoded []byte, oti OTI, limits Limits) (EncodingPacketView, error) {
	if err := oti.Validate(limits); err != nil {
		return EncodingPacketView{}, err
	}
	if len(encoded) < PayloadIDEncodedSize+1 {
		return EncodingPacketView{}, fmt.Errorf("%w: packet is too short", ErrInvalidPacket)
	}
	if uint64(len(encoded)) > limits.MaxPacketBytes {
		return EncodingPacketView{}, fmt.Errorf("%w: packet has %d bytes", ErrLimitExceeded, len(encoded))
	}
	id, err := ParsePayloadID(encoded[:PayloadIDEncodedSize])
	if err != nil {
		return EncodingPacketView{}, err
	}
	if id.SourceBlockNumber >= oti.SourceBlocks {
		return EncodingPacketView{}, fmt.Errorf("%w: SBN %d is outside Z=%d", ErrInvalidPacket, id.SourceBlockNumber, oti.SourceBlocks)
	}

	payload := encoded[PayloadIDEncodedSize:len(encoded):len(encoded)]
	symbolSize := uint64(oti.SymbolSize)
	count := ceilDiv(uint64(len(payload)), symbolSize)
	if count == 0 || count > uint64(limits.MaxSymbolsPerPacket) {
		return EncodingPacketView{}, fmt.Errorf("%w: packet contains %d symbols", ErrLimitExceeded, count)
	}
	lastESI := uint64(id.EncodingSymbolID) + count - 1
	if lastESI > uint64(MaxEncodingSymbolID) {
		return EncodingPacketView{}, fmt.Errorf("%w: grouped ESI range exceeds 24 bits", ErrInvalidPacket)
	}

	sourceSymbols, err := SourceBlockSymbols(oti, id.SourceBlockNumber)
	if err != nil {
		return EncodingPacketView{}, err
	}
	if sourceSymbols == 0 {
		return EncodingPacketView{}, fmt.Errorf("%w: source block has no symbols", ErrInvalidPacket)
	}
	isSource := id.EncodingSymbolID < sourceSymbols
	remainder := uint64(len(payload)) % symbolSize
	if isSource {
		if uint64(id.EncodingSymbolID)+count > uint64(sourceSymbols) {
			return EncodingPacketView{}, fmt.Errorf("%w: packet crosses source/repair boundary", ErrInvalidPacket)
		}
		layout, layoutErr := paddingLayoutForSourceBlock(oti, id.SourceBlockNumber)
		if layoutErr != nil {
			return EncodingPacketView{}, layoutErr
		}
		if remainder != 0 {
			padding, paddingErr := layout.trailingPadding(id.EncodingSymbolID + uint32(count) - 1)
			if paddingErr != nil {
				return EncodingPacketView{}, paddingErr
			}
			omitted := symbolSize - remainder
			if padding == 0 || omitted > padding {
				return EncodingPacketView{}, fmt.Errorf("%w: illegal shortened source symbol", ErrInvalidPacket)
			}
		}
		for index := uint64(0); index < count; index++ {
			padding, paddingErr := layout.trailingPadding(id.EncodingSymbolID + uint32(index))
			if paddingErr != nil {
				return EncodingPacketView{}, paddingErr
			}
			transmitted := symbolSize
			if index+1 == count && remainder != 0 {
				transmitted = remainder
			}
			paddingStart := symbolSize - padding
			if transmitted <= paddingStart {
				continue
			}
			start := index*symbolSize + paddingStart
			end := index*symbolSize + transmitted
			for _, value := range payload[int(start):int(end)] {
				if value != 0 {
					return EncodingPacketView{}, fmt.Errorf("%w: source symbol has nonzero FEC padding", ErrInvalidPacket)
				}
			}
		}
	} else if remainder != 0 {
		return EncodingPacketView{}, fmt.Errorf("%w: repair packet contains a partial symbol", ErrInvalidPacket)
	}

	return EncodingPacketView{
		id:          id,
		payload:     payload,
		symbolSize:  oti.SymbolSize,
		symbolCount: uint32(count),
		source:      isSource,
	}, nil
}

func finalNonemptySourceBlock(oti OTI) (uint8, error) {
	totalSymbols := ceilDiv(oti.TransferLength, uint64(oti.SymbolSize))
	if totalSymbols == 0 {
		return 0, fmt.Errorf("%w: object has no source symbols", ErrInvalidPacket)
	}
	partition, err := rfc6330.Partition(totalSymbols, uint64(oti.SourceBlocks))
	if err != nil {
		return 0, err
	}
	if partition.SmallSize == 0 {
		return uint8(partition.LargeCount - 1), nil
	}
	return oti.SourceBlocks - 1, nil
}

// SourceBlockSymbols returns K for a source block using RFC 6330
// Section 4.4.1.2 partitioning.
func SourceBlockSymbols(oti OTI, sourceBlockNumber uint8) (uint32, error) {
	if err := oti.Validate(RFCWireLimits()); err != nil {
		return 0, err
	}
	if sourceBlockNumber >= oti.SourceBlocks {
		return 0, fmt.Errorf("%w: invalid source block", ErrInvalidOTI)
	}
	totalSymbols := ceilDiv(oti.TransferLength, uint64(oti.SymbolSize))
	partition, err := rfc6330.Partition(totalSymbols, uint64(oti.SourceBlocks))
	if err != nil {
		return 0, err
	}
	size, err := partition.Size(uint64(sourceBlockNumber))
	if err != nil {
		return 0, err
	}
	if size > uint64(MaxSourceSymbolsPerBlock) {
		return 0, fmt.Errorf("%w: source block requires %d symbols", ErrInvalidOTI, size)
	}
	return uint32(size), nil
}
