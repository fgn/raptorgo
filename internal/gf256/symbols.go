package gf256

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidSymbolDimensions = errors.New("gf256: invalid symbol dimensions")
	ErrSymbolLimitExceeded     = errors.New("gf256: symbol allocation limit exceeded")
	ErrSymbolIndex             = errors.New("gf256: symbol index out of range")
	ErrSymbolSizeMismatch      = errors.New("gf256: symbol size mismatch")
)

// Symbols owns a fixed number of equal-sized symbols in one row-major byte
// allocation. Keeping the storage flat makes its backing cost exact and avoids
// a slice header allocation per symbol. Symbols is not safe for concurrent
// mutation.
type Symbols struct {
	count  uint32
	size   uint32
	values []byte
}

// NewSymbols allocates symbol storage only after its dimensions have been
// checked against maxBytes and the platform's slice-size limit.
func NewSymbols(count, size uint32, maxBytes uint64) (*Symbols, error) {
	if count == 0 || size == 0 {
		return nil, fmt.Errorf("%w: %dx%d", ErrInvalidSymbolDimensions, count, size)
	}
	bytes := uint64(count) * uint64(size)
	maxInt := uint64(^uint(0) >> 1)
	if bytes > maxBytes || bytes > maxInt {
		return nil, fmt.Errorf("%w: %d symbols of size %d require %d bytes (limit %d)", ErrSymbolLimitExceeded, count, size, bytes, maxBytes)
	}
	return &Symbols{
		count:  count,
		size:   size,
		values: make([]byte, int(bytes)),
	}, nil
}

func (symbols *Symbols) Count() uint32 { return symbols.count }
func (symbols *Symbols) Size() uint32  { return symbols.size }
func (symbols *Symbols) Bytes() uint64 { return uint64(len(symbols.values)) }

// Symbol returns a mutable view of one symbol. The view remains owned by
// symbols and is invalidated when symbols becomes unreachable.
func (symbols *Symbols) Symbol(index uint32) ([]byte, error) {
	if index >= symbols.count {
		return nil, fmt.Errorf("%w: %d of %d", ErrSymbolIndex, index, symbols.count)
	}
	start := int(uint64(index) * uint64(symbols.size))
	end := start + int(symbols.size)
	return symbols.values[start:end:end], nil
}

// SetSymbol copies values into one symbol. It performs no partial write when
// the index or size is invalid.
func (symbols *Symbols) SetSymbol(index uint32, values []byte) error {
	if index >= symbols.count {
		return fmt.Errorf("%w: %d of %d", ErrSymbolIndex, index, symbols.count)
	}
	if uint64(len(values)) != uint64(symbols.size) {
		return fmt.Errorf("%w: got %d bytes, want %d", ErrSymbolSizeMismatch, len(values), symbols.size)
	}
	destination, err := symbols.Symbol(index)
	if err != nil {
		return err
	}
	copy(destination, values)
	return nil
}
