package raptorgo

import (
	"fmt"
	"math"
	"sync"

	"github.com/fgn/raptorgo/internal/codec"
	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

// ObjectEncoder owns a source object and emits canonical grouped RFC 6330
// source or repair packets. It retains at most one repair encoder cache and is
// safe for concurrent Packet calls.
type ObjectEncoder struct {
	mu                sync.Mutex
	oti               OTI
	limits            Limits
	object            []byte
	blockPartition    rfc6330.PartitionResult
	subBlockPartition rfc6330.PartitionResult
	cacheBlock        uint8
	cacheValid        bool
	cacheBytes        uint64
	cache             *codec.SourceBlockEncoder
}

// ObjectEncoderWorkspaceBackingBytes returns the largest logical backing peak
// needed to own the object and construct any one repair encoder cache. Packet
// output backing is checked separately because it depends on each request.
func ObjectEncoderWorkspaceBackingBytes(oti OTI, limits Limits) (uint64, error) {
	if err := oti.Validate(limits); err != nil {
		return 0, err
	}
	peak := oti.TransferLength
	workspaceByK := make(map[uint32]uint64, 2)
	for sourceBlockIndex := 0; sourceBlockIndex < int(oti.SourceBlocks); sourceBlockIndex++ {
		sourceBlock := uint8(sourceBlockIndex)
		k, err := SourceBlockSymbols(oti, sourceBlock)
		if err != nil {
			return 0, err
		}
		if k == 0 {
			continue
		}
		workspace, ok := workspaceByK[k]
		if !ok {
			workspace, err = codec.SourceBlockEncoderWorkspaceBackingBytes(k, uint32(oti.SymbolSize))
			if err != nil {
				return 0, fmt.Errorf("%w: SBN %d preflight: %w", ErrObjectEncoder, sourceBlock, err)
			}
			workspaceByK[k] = workspace
		}
		blockBytes := uint64(k) * uint64(oti.SymbolSize)
		copyPeak, err := checkedObjectBacking(oti.TransferLength, blockBytes, blockBytes)
		if err != nil {
			return 0, err
		}
		encodePeak, err := checkedObjectBacking(oti.TransferLength, blockBytes, workspace)
		if err != nil {
			return 0, err
		}
		peak = max(peak, copyPeak, encodePeak)
	}
	return peak, nil
}

// NewObjectEncoder validates and owns object. Repair-plan construction is
// preflighted for every distinct block size, but is deferred until the first
// repair packet for a block is requested.
func NewObjectEncoder(object []byte, oti OTI, limits Limits) (*ObjectEncoder, error) {
	if err := oti.Validate(limits); err != nil {
		return nil, err
	}
	if uint64(len(object)) != oti.TransferLength {
		return nil, fmt.Errorf("%w: object has %d bytes, OTI declares %d", ErrInvalidObject, len(object), oti.TransferLength)
	}
	required, err := ObjectEncoderWorkspaceBackingBytes(oti, limits)
	if err != nil {
		return nil, err
	}
	if required > limits.MaxWorkingMemory {
		return nil, fmt.Errorf(
			"%w: object encoder needs %d bytes (limit %d)",
			ErrLimitExceeded, required, limits.MaxWorkingMemory,
		)
	}
	totalSymbols := ceilDiv(oti.TransferLength, uint64(oti.SymbolSize))
	blockPartition, err := rfc6330.Partition(totalSymbols, uint64(oti.SourceBlocks))
	if err != nil {
		return nil, err
	}
	subSymbolUnits := uint64(oti.SymbolSize) / uint64(oti.SymbolAlignment)
	subBlockPartition, err := rfc6330.Partition(subSymbolUnits, uint64(oti.SubBlocks))
	if err != nil {
		return nil, err
	}
	return &ObjectEncoder{
		oti: oti, limits: limits, object: append([]byte(nil), object...),
		blockPartition: blockPartition, subBlockPartition: subBlockPartition,
	}, nil
}

// OTI returns the Object Transmission Information supplied at construction.
// OTI on a nil encoder returns the zero OTI.
func (encoder *ObjectEncoder) OTI() OTI {
	if encoder == nil {
		return OTI{}
	}
	return encoder.oti
}

// Packet returns one owned grouped packet beginning at firstESI. A group must
// lie wholly in the source range or wholly in the repair range. When
// shortenFinal is true, trailing FEC padding is omitted from the packet's last
// source symbol when RFC 6330 permits it. Packet on a nil encoder returns an
// error wrapping ErrObjectEncoder.
func (encoder *ObjectEncoder) Packet(
	sourceBlockNumber uint8,
	firstESI, symbolCount uint32,
	shortenFinal bool,
) ([]byte, error) {
	if encoder == nil {
		return nil, fmt.Errorf("%w: nil encoder", ErrObjectEncoder)
	}
	encoder.mu.Lock()
	packet, err := encoder.packetLocked(nil, sourceBlockNumber, firstESI, symbolCount, shortenFinal)
	encoder.mu.Unlock()
	return packet, err
}

// PacketInto writes the same canonical packet as Packet into destination and
// returns its exact-length view. Destination must have room for the four-byte
// payload ID and the requested symbols; passing a full-sized buffer is valid
// when shortenFinal may produce a shorter result. The returned bytes alias
// destination, whose unused tail is left unchanged. A source-packet call does
// not allocate when destination is large enough.
func (encoder *ObjectEncoder) PacketInto(
	destination []byte,
	sourceBlockNumber uint8,
	firstESI, symbolCount uint32,
	shortenFinal bool,
) ([]byte, error) {
	if encoder == nil {
		return nil, fmt.Errorf("%w: nil encoder", ErrObjectEncoder)
	}
	if destination == nil {
		return nil, fmt.Errorf("%w: nil packet destination", ErrInvalidPacket)
	}
	encoder.mu.Lock()
	packet, err := encoder.packetLocked(destination, sourceBlockNumber, firstESI, symbolCount, shortenFinal)
	encoder.mu.Unlock()
	return packet, err
}

func (encoder *ObjectEncoder) packetLocked(
	destination []byte,
	sourceBlockNumber uint8,
	firstESI, symbolCount uint32,
	shortenFinal bool,
) ([]byte, error) {
	if sourceBlockNumber >= encoder.oti.SourceBlocks || symbolCount == 0 ||
		symbolCount > encoder.limits.MaxSymbolsPerPacket || firstESI > MaxEncodingSymbolID ||
		uint64(firstESI)+uint64(symbolCount)-1 > uint64(MaxEncodingSymbolID) {
		return nil, fmt.Errorf("%w: SBN=%d ESI=%d count=%d", ErrInvalidPacket, sourceBlockNumber, firstESI, symbolCount)
	}
	blockSymbols, err := encoder.blockPartition.Size(uint64(sourceBlockNumber))
	if err != nil {
		return nil, err
	}
	k := uint32(blockSymbols)
	if k == 0 {
		return nil, fmt.Errorf("%w: SBN %d is empty", ErrInvalidPacket, sourceBlockNumber)
	}
	lastExclusive := uint64(firstESI) + uint64(symbolCount)
	isSource := firstESI < k
	if isSource && lastExclusive > uint64(k) {
		return nil, fmt.Errorf("%w: packet crosses source/repair boundary", ErrInvalidPacket)
	}

	payloadBytes := uint64(symbolCount) * uint64(encoder.oti.SymbolSize)
	if isSource && shortenFinal {
		layout, layoutErr := paddingLayoutForSourceBlock(encoder.oti, sourceBlockNumber)
		if layoutErr != nil {
			return nil, layoutErr
		}
		padding, paddingErr := layout.trailingPadding(firstESI + symbolCount - 1)
		if paddingErr != nil {
			return nil, paddingErr
		}
		payloadBytes -= padding
	}
	packetBytes, err := checkedObjectBacking(PayloadIDEncodedSize, payloadBytes)
	if err != nil {
		return nil, err
	}
	if packetBytes > encoder.limits.MaxPacketBytes || packetBytes > uint64(maxInt()) {
		return nil, fmt.Errorf("%w: packet requires %d bytes", ErrLimitExceeded, packetBytes)
	}
	if destination != nil && uint64(len(destination)) < packetBytes {
		return nil, fmt.Errorf(
			"%w: packet destination has %d bytes, need %d",
			ErrInvalidPacket, len(destination), packetBytes,
		)
	}

	if isSource {
		if peakErr := checkSourcePacketPeak(
			uint64(len(encoder.object)), encoder.cacheBytes, packetBytes, encoder.limits.MaxWorkingMemory,
		); peakErr != nil {
			return nil, peakErr
		}
		packet := packetBuffer(destination, packetBytes)
		if encoder.oti.SubBlocks == 1 {
			return encoder.buildContiguousSourcePacket(
				packet, sourceBlockNumber, firstESI,
			)
		}
		return encoder.buildSubBlockedSourcePacket(
			packet, sourceBlockNumber, k, firstESI, symbolCount,
		)
	}

	retained, err := codec.SourceBlockEncoderRetainedBackingBytes(k, uint32(encoder.oti.SymbolSize))
	if err != nil {
		return nil, fmt.Errorf("%w: repair backing: %w", ErrObjectEncoder, err)
	}
	peak, err := checkedObjectBacking(uint64(len(encoder.object)), retained, packetBytes, uint64(encoder.oti.SymbolSize))
	if err != nil {
		return nil, fmt.Errorf("repair packet peak: %w", err)
	}
	if peak > encoder.limits.MaxWorkingMemory {
		return nil, fmt.Errorf(
			"%w: repair packet peak %d bytes (limit %d)",
			ErrLimitExceeded, peak, encoder.limits.MaxWorkingMemory,
		)
	}
	blockEncoder, err := encoder.repairEncoder(sourceBlockNumber, k, retained)
	if err != nil {
		return nil, err
	}
	packet := packetBuffer(destination, packetBytes)
	return encoder.buildRepairPacket(packet, blockEncoder, sourceBlockNumber, firstESI, symbolCount)
}

func packetBuffer(destination []byte, packetBytes uint64) []byte {
	if destination == nil {
		return make([]byte, int(packetBytes))
	}
	packet := destination[:int(packetBytes)]
	clear(packet)
	return packet
}

func checkSourcePacketPeak(objectBytes, blockBytes, packetBytes, maximum uint64) error {
	peak, err := checkedObjectBacking(objectBytes, blockBytes, packetBytes)
	if err != nil {
		return fmt.Errorf("source packet peak: %w", err)
	}
	if peak > maximum {
		return fmt.Errorf(
			"%w: source packet peak %d bytes (limit %d)",
			ErrLimitExceeded, peak, maximum,
		)
	}
	return nil
}

func (encoder *ObjectEncoder) buildContiguousSourcePacket(
	packet []byte,
	sourceBlockNumber uint8,
	firstESI uint32,
) ([]byte, error) {
	writePayloadID(packet, sourceBlockNumber, firstESI)

	firstSymbol := sourceBlockFirstSymbol(encoder.blockPartition, uint64(sourceBlockNumber))
	objectOffset := (firstSymbol + uint64(firstESI)) * uint64(encoder.oti.SymbolSize)
	copyObjectRange(packet[PayloadIDEncodedSize:], encoder.object, objectOffset)
	return packet, nil
}

func (encoder *ObjectEncoder) buildSubBlockedSourcePacket(
	packet []byte,
	sourceBlockNumber uint8,
	k, firstESI, symbolCount uint32,
) ([]byte, error) {
	if encoder.oti.SubBlocks < 2 {
		return nil, fmt.Errorf("%w: sub-blocked packet path requires N >= 2", ErrObjectEncoder)
	}
	writePayloadID(packet, sourceBlockNumber, firstESI)
	payload := packet[PayloadIDEncodedSize:]

	firstSymbol := sourceBlockFirstSymbol(encoder.blockPartition, uint64(sourceBlockNumber))
	blockStart := firstSymbol * uint64(encoder.oti.SymbolSize)

	var inputOffset, symbolOffset uint64
	for subBlock := range uint64(encoder.oti.SubBlocks) {
		units := encoder.subBlockPartition.SmallSize
		if subBlock < encoder.subBlockPartition.LargeCount {
			units = encoder.subBlockPartition.LargeSize
		}
		subSymbolBytes := units * uint64(encoder.oti.SymbolAlignment)
		for packetSymbol := range symbolCount {
			rawDestinationStart := uint64(packetSymbol)*uint64(encoder.oti.SymbolSize) + symbolOffset
			destinationStart := min(rawDestinationStart, uint64(len(payload)))
			destinationEnd := min(rawDestinationStart+subSymbolBytes, uint64(len(payload)))
			sourceStart := blockStart + inputOffset + uint64(firstESI+packetSymbol)*subSymbolBytes
			copyObjectRange(payload[int(destinationStart):int(destinationEnd)], encoder.object, sourceStart)
		}
		inputOffset += uint64(k) * subSymbolBytes
		symbolOffset += subSymbolBytes
	}
	return packet, nil
}

func (encoder *ObjectEncoder) buildRepairPacket(
	packet []byte,
	blockEncoder *codec.SourceBlockEncoder,
	sourceBlockNumber uint8,
	firstESI, symbolCount uint32,
) ([]byte, error) {
	writePayloadID(packet, sourceBlockNumber, firstESI)
	offset := PayloadIDEncodedSize
	for index := uint32(0); index < symbolCount; index++ {
		values, symbolErr := blockEncoder.Symbol(firstESI+index, uint64(encoder.oti.SymbolSize))
		if symbolErr != nil {
			return nil, fmt.Errorf("%w: ESI %d: %w", ErrObjectEncoder, firstESI+index, symbolErr)
		}
		offset += copy(packet[offset:], values)
	}
	return packet, nil
}

func (encoder *ObjectEncoder) repairEncoder(
	sourceBlockNumber uint8,
	k uint32,
	retained uint64,
) (*codec.SourceBlockEncoder, error) {
	if encoder.cacheValid && encoder.cacheBlock == sourceBlockNumber {
		return encoder.cache, nil
	}
	encoder.cache, encoder.cacheValid, encoder.cacheBytes = nil, false, 0
	block, err := BuildSourceBlock(encoder.object, encoder.oti, sourceBlockNumber, encoder.limits)
	if err != nil {
		return nil, err
	}
	blockBytes := uint64(k) * uint64(encoder.oti.SymbolSize)
	source, err := gf256.NewSymbols(k, uint32(encoder.oti.SymbolSize), blockBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: source slab: %w", ErrObjectEncoder, err)
	}
	for index := uint32(0); index < k; index++ {
		values, symbolErr := block.Symbol(index)
		if symbolErr != nil {
			return nil, symbolErr
		}
		if symbolErr = source.SetSymbol(index, values); symbolErr != nil {
			return nil, symbolErr
		}
	}
	block = SourceBlock{}
	workspace, err := codec.SourceBlockEncoderWorkspaceBackingBytes(k, uint32(encoder.oti.SymbolSize))
	if err != nil {
		return nil, err
	}
	result, err := codec.NewSourceBlockEncoder(source, workspace)
	if err != nil {
		return nil, fmt.Errorf("%w: SBN %d: %w", ErrObjectEncoder, sourceBlockNumber, err)
	}
	encoder.cacheBlock, encoder.cacheValid, encoder.cacheBytes, encoder.cache = sourceBlockNumber, true, retained, result
	return result, nil
}

func checkedObjectBacking(values ...uint64) (uint64, error) {
	var total uint64
	for _, value := range values {
		if total > math.MaxUint64-value {
			return 0, fmt.Errorf("%w: backing size overflow", ErrLimitExceeded)
		}
		total += value
	}
	if total > uint64(maxInt()) {
		return 0, fmt.Errorf("%w: backing %d exceeds address space", ErrLimitExceeded, total)
	}
	return total, nil
}
