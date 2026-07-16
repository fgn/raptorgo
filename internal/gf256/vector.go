package gf256

import (
	"errors"
	"unsafe"
)

// ErrLengthMismatch reports vector operations whose operands differ in size.
var ErrLengthMismatch = errors.New("gf256: vector length mismatch")

// XOR adds src to dst in GF(256). It performs no partial write when lengths
// differ and preserves snapshot semantics for exact or partial overlap.
func XOR(dst, src []byte) error {
	if len(dst) != len(src) {
		return ErrLengthMismatch
	}
	if forwardWriteOverlapsSource(dst, src) {
		for index := len(src) - 1; index >= 0; index-- {
			dst[index] ^= src[index]
		}
	} else {
		for index, value := range src {
			dst[index] ^= value
		}
	}
	return nil
}

// MulAdd adds coefficient*src to dst in GF(256). It performs no partial write
// when lengths differ and preserves snapshot semantics for exact or partial
// overlap.
func MulAdd(dst []byte, coefficient byte, src []byte) error {
	if len(dst) != len(src) {
		return ErrLengthMismatch
	}
	switch coefficient {
	case 0:
		return nil
	case 1:
		return XOR(dst, src)
	default:
		if forwardWriteOverlapsSource(dst, src) {
			for index := len(src) - 1; index >= 0; index-- {
				dst[index] ^= Mul(coefficient, src[index])
			}
		} else {
			for index, value := range src {
				dst[index] ^= Mul(coefficient, value)
			}
		}
		return nil
	}
}

// forwardWriteOverlapsSource detects the only overlap direction in which a
// forward element-wise pass can overwrite a source element not yet read. It
// compares the slice starts without dereferencing them. The uintptr values do
// not escape this call; they are used only to select a traversal direction.
func forwardWriteOverlapsSource(dst, src []byte) bool {
	if len(dst) == 0 || &dst[0] == &src[0] {
		return false
	}
	destinationStart := uintptr(unsafe.Pointer(unsafe.SliceData(dst)))
	sourceStart := uintptr(unsafe.Pointer(unsafe.SliceData(src)))
	return destinationStart > sourceStart && destinationStart-sourceStart < uintptr(len(src))
}

// Scale multiplies every element of values by coefficient in GF(256).
func Scale(values []byte, coefficient byte) {
	switch coefficient {
	case 0:
		clear(values)
	case 1:
		return
	default:
		for index, value := range values {
			values[index] = Mul(coefficient, value)
		}
	}
}
