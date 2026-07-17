package raptorgo

import "fmt"

// PayloadIDEncodedSize is the encoded size in bytes of the RFC 6330 FEC
// Payload ID that prefixes every encoding packet.
const PayloadIDEncodedSize = 4

// PayloadID is the four-octet FEC Payload ID from RFC 6330 Section 3.2.
type PayloadID struct {
	SourceBlockNumber uint8
	EncodingSymbolID  uint32
}

// NewPayloadID constructs a PayloadID, rejecting encoding symbol IDs that do
// not fit the 24-bit wire field.
func NewPayloadID(sourceBlockNumber uint8, encodingSymbolID uint32) (PayloadID, error) {
	if encodingSymbolID > MaxEncodingSymbolID {
		return PayloadID{}, fmt.Errorf("%w: ESI %d exceeds 24 bits", ErrInvalidPayloadID, encodingSymbolID)
	}
	return PayloadID{SourceBlockNumber: sourceBlockNumber, EncodingSymbolID: encodingSymbolID}, nil
}

// MarshalBinary encodes the payload ID into its four-octet RFC 6330 wire
// form: the source block number followed by the 24-bit big-endian encoding
// symbol ID.
func (id PayloadID) MarshalBinary() ([]byte, error) {
	if id.EncodingSymbolID > MaxEncodingSymbolID {
		return nil, fmt.Errorf("%w: ESI %d exceeds 24 bits", ErrInvalidPayloadID, id.EncodingSymbolID)
	}
	return []byte{
		id.SourceBlockNumber,
		byte(id.EncodingSymbolID >> 16),
		byte(id.EncodingSymbolID >> 8),
		byte(id.EncodingSymbolID),
	}, nil
}

func writePayloadID(encoded []byte, sourceBlockNumber uint8, encodingSymbolID uint32) {
	encoded[0] = sourceBlockNumber
	encoded[1] = byte(encodingSymbolID >> 16)
	encoded[2] = byte(encodingSymbolID >> 8)
	encoded[3] = byte(encodingSymbolID)
}

// ParsePayloadID decodes a four-octet RFC 6330 FEC Payload ID.
func ParsePayloadID(encoded []byte) (PayloadID, error) {
	if len(encoded) != PayloadIDEncodedSize {
		return PayloadID{}, fmt.Errorf("%w: encoded length %d", ErrInvalidPayloadID, len(encoded))
	}
	return PayloadID{
		SourceBlockNumber: encoded[0],
		EncodingSymbolID:  uint32(encoded[1])<<16 | uint32(encoded[2])<<8 | uint32(encoded[3]),
	}, nil
}
