//go:build amd64 && goexperiment.simd

package gf256

import "simd/archsimd"

const simdVectorBytes = 32

// mulTableLow[c] holds the products c*n for every low-nibble value n, and
// mulTableHigh[c] holds c*(n<<4), each duplicated across both 128-bit lanes so
// PermuteOrZeroGrouped (VPSHUFB) can look up 32 bytes at once. Splitting each
// source byte as v = high<<4 ^ low makes c*v = c*(high<<4) ^ c*low by
// distributivity, so two table lookups and one XOR multiply a full vector.
var mulTableLow, mulTableHigh [256][simdVectorBytes]byte

func init() {
	for coefficient := 0; coefficient < 256; coefficient++ {
		for nibble := 0; nibble < 16; nibble++ {
			low := carrylessProduct(byte(coefficient), byte(nibble))
			high := carrylessProduct(byte(coefficient), byte(nibble<<4))
			mulTableLow[coefficient][nibble] = low
			mulTableLow[coefficient][nibble+16] = low
			mulTableHigh[coefficient][nibble] = high
			mulTableHigh[coefficient][nibble+16] = high
		}
	}
}

// carrylessProduct multiplies in GF(256) with the RFC 6330 polynomial 0x11d by
// shift-and-reduce. It is independent of the package's exp/log tables, so the
// nibble tables neither depend on package init ordering nor inherit a defect
// those tables might share with Mul.
func carrylessProduct(a, b byte) byte {
	var product byte
	for ; b != 0; b >>= 1 {
		if b&1 != 0 {
			product ^= a
		}
		carry := a & 0x80
		a <<= 1
		if carry != 0 {
			a ^= 0x1d
		}
	}
	return product
}

func xorForward(dst, src []byte) {
	if !archsimd.X86.AVX2() {
		xorForwardScalar(dst, src)
		return
	}
	for len(src) >= 4*simdVectorBytes {
		destination := dst[:4*simdVectorBytes]
		source := src[:4*simdVectorBytes]
		source0 := archsimd.LoadUint8x32Slice(source)
		source1 := archsimd.LoadUint8x32Slice(source[simdVectorBytes:])
		source2 := archsimd.LoadUint8x32Slice(source[2*simdVectorBytes:])
		source3 := archsimd.LoadUint8x32Slice(source[3*simdVectorBytes:])
		archsimd.LoadUint8x32Slice(destination).
			Xor(source0).StoreSlice(destination)
		archsimd.LoadUint8x32Slice(destination[simdVectorBytes:]).
			Xor(source1).StoreSlice(destination[simdVectorBytes:])
		archsimd.LoadUint8x32Slice(destination[2*simdVectorBytes:]).
			Xor(source2).StoreSlice(destination[2*simdVectorBytes:])
		archsimd.LoadUint8x32Slice(destination[3*simdVectorBytes:]).
			Xor(source3).StoreSlice(destination[3*simdVectorBytes:])
		dst = dst[4*simdVectorBytes:]
		src = src[4*simdVectorBytes:]
	}
	for len(src) >= simdVectorBytes {
		archsimd.LoadUint8x32Slice(dst).
			Xor(archsimd.LoadUint8x32Slice(src)).
			StoreSlice(dst)
		dst = dst[simdVectorBytes:]
		src = src[simdVectorBytes:]
	}
	xorForwardScalar(dst, src)
}

func mulAddForward(dst []byte, coefficient byte, src []byte) {
	if !archsimd.X86.AVX2() {
		mulAddForwardScalar(dst, coefficient, src)
		return
	}
	lowTable := archsimd.LoadUint8x32Slice(mulTableLow[coefficient][:])
	highTable := archsimd.LoadUint8x32Slice(mulTableHigh[coefficient][:])
	nibbleMask := archsimd.BroadcastUint8x32(0x0f)
	nibbleShift := archsimd.BroadcastUint32x8(4)
	for len(src) >= 2*simdVectorBytes {
		destination := dst[:2*simdVectorBytes]
		source := src[:2*simdVectorBytes]
		product0 := simdNibbleMul(
			archsimd.LoadUint8x32Slice(source), lowTable, highTable, nibbleMask, nibbleShift,
		)
		product1 := simdNibbleMul(
			archsimd.LoadUint8x32Slice(source[simdVectorBytes:]), lowTable, highTable, nibbleMask, nibbleShift,
		)
		archsimd.LoadUint8x32Slice(destination).
			Xor(product0).StoreSlice(destination)
		archsimd.LoadUint8x32Slice(destination[simdVectorBytes:]).
			Xor(product1).StoreSlice(destination[simdVectorBytes:])
		dst = dst[2*simdVectorBytes:]
		src = src[2*simdVectorBytes:]
	}
	for len(src) >= simdVectorBytes {
		product := simdNibbleMul(
			archsimd.LoadUint8x32Slice(src), lowTable, highTable, nibbleMask, nibbleShift,
		)
		archsimd.LoadUint8x32Slice(dst).
			Xor(product).StoreSlice(dst)
		dst = dst[simdVectorBytes:]
		src = src[simdVectorBytes:]
	}
	mulAddForwardScalar(dst, coefficient, src)
}

// simdNibbleMul multiplies 32 field elements by the scalar coefficient encoded
// in the two lane-duplicated nibble product tables. The 32-bit variable shift
// (VPSRLVD) plus per-byte mask implements the byte-wise logical right shift by
// four that AVX2 lacks directly. A vector shift count is used instead of
// Uint16x16.ShiftAllRight's scalar count because the scalar form materializes
// its count through a legacy (non-VEX) SSE register move inside the loop,
// which triggers AVX-to-SSE transition stalls that dominate the kernel.
// Nibble indices never set the VPSHUFB sign bit, so the zeroing behavior of
// PermuteOrZeroGrouped is never triggered.
//
// Uint8x32.GaloisFieldMul is deliberately not used: in Go 1.26 it requires
// AVX-512 GFNI and implements the AES polynomial 0x11b, not RFC 6330's 0x11d.
func simdNibbleMul(
	values, lowTable, highTable, nibbleMask archsimd.Uint8x32,
	nibbleShift archsimd.Uint32x8,
) archsimd.Uint8x32 {
	lowNibbles := values.And(nibbleMask)
	highNibbles := values.AsUint32x8().ShiftRight(nibbleShift).AsUint8x32().And(nibbleMask)
	return lowTable.PermuteOrZeroGrouped(lowNibbles.AsInt8x32()).
		Xor(highTable.PermuteOrZeroGrouped(highNibbles.AsInt8x32()))
}
