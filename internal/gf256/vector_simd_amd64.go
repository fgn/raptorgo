//go:build amd64 && goexperiment.simd

package gf256

import "simd/archsimd"

const simdVectorBytes = 32

func xorForward(dst, src []byte) {
	if !archsimd.X86.AVX2() {
		xorForwardScalar(dst, src)
		return
	}
	offset := 0
	for ; offset+simdVectorBytes <= len(src); offset += simdVectorBytes {
		destination := archsimd.LoadUint8x32Slice(dst[offset:])
		source := archsimd.LoadUint8x32Slice(src[offset:])
		destination.Xor(source).StoreSlice(dst[offset:])
	}
	xorForwardScalar(dst[offset:], src[offset:])
}

func mulAddForward(dst []byte, coefficient byte, src []byte) {
	if !archsimd.X86.AVX2() {
		mulAddForwardScalar(dst, coefficient, src)
		return
	}
	zero := archsimd.BroadcastUint8x32(0)
	reduction := archsimd.BroadcastUint8x32(0x1d)
	offset := 0
	for ; offset+simdVectorBytes <= len(src); offset += simdVectorBytes {
		source := archsimd.LoadUint8x32Slice(src[offset:])
		product := simdPolynomialMul(source, coefficient, zero, reduction)
		destination := archsimd.LoadUint8x32Slice(dst[offset:])
		destination.Xor(product).StoreSlice(dst[offset:])
	}
	mulAddForwardScalar(dst[offset:], coefficient, src[offset:])
}

// simdPolynomialMul multiplies 32 field elements by one scalar coefficient
// using the RFC 6330 polynomial 0x11d. Uint8x32.GaloisFieldMul cannot be used:
// the GFNI instruction implements the AES polynomial 0x11b.
func simdPolynomialMul(
	values archsimd.Uint8x32,
	coefficient byte,
	zero, reduction archsimd.Uint8x32,
) archsimd.Uint8x32 {
	product := zero
	power := values
	for bit := byte(1); bit != 0; bit <<= 1 {
		if coefficient&bit != 0 {
			product = product.Xor(power)
		}
		if bit == 0x80 {
			break
		}
		carry := zero.AsInt8x32().Greater(power.AsInt8x32()).
			ToInt8x32().
			AsUint8x32().
			And(reduction)
		power = power.Add(power).Xor(carry)
	}
	return product
}
