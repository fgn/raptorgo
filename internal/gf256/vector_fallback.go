//go:build !amd64 || !goexperiment.simd

package gf256

func xorForward(dst, src []byte) {
	xorForwardScalar(dst, src)
}

func mulAddForward(dst []byte, coefficient byte, src []byte) {
	mulAddForwardScalar(dst, coefficient, src)
}
