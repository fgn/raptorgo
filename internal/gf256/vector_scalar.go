package gf256

func xorForwardScalar(dst, src []byte) {
	for index, value := range src {
		dst[index] ^= value
	}
}

func xorBackwardScalar(dst, src []byte) {
	for index := len(src) - 1; index >= 0; index-- {
		dst[index] ^= src[index]
	}
}

func mulAddForwardScalar(dst []byte, coefficient byte, src []byte) {
	for index, value := range src {
		dst[index] ^= Mul(coefficient, value)
	}
}

func mulAddBackwardScalar(dst []byte, coefficient byte, src []byte) {
	for index := len(src) - 1; index >= 0; index-- {
		dst[index] ^= Mul(coefficient, src[index])
	}
}
