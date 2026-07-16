// Package gf256 implements the octet arithmetic defined by RFC 6330 Section 5.7.
package gf256

// exp is OCT_EXP from RFC 6330 Section 5.7.3. It is generated from the
// primitive polynomial x^8 + x^4 + x^3 + x^2 + 1 (0x11d). The second 255
// entries repeat the first 255, matching the RFC's 510-entry table.
var exp [510]byte

// log is OCT_LOG from RFC 6330 Section 5.7.4. log[0] is intentionally unused.
var log [256]byte

func init() {
	x := uint16(1)
	for i := 0; i < 255; i++ {
		exp[i] = byte(x)
		log[byte(x)] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11d
		}
	}
	copy(exp[255:], exp[:255])
}

// Add returns a+b in GF(256). Addition and subtraction are both XOR.
func Add(a, b byte) byte { return a ^ b }

// Mul returns a*b in GF(256).
func Mul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return exp[int(log[a])+int(log[b])]
}

// Inv returns the multiplicative inverse of a. There is no inverse for zero.
func Inv(a byte) (byte, bool) {
	if a == 0 {
		return 0, false
	}
	return exp[255-int(log[a])], true
}

// Div returns a/b. Division by zero is undefined and reports ok=false.
func Div(a, b byte) (quotient byte, ok bool) {
	if b == 0 {
		return 0, false
	}
	if a == 0 {
		return 0, true
	}
	return exp[int(log[a])-int(log[b])+255], true
}

// AlphaPower returns 2^exponent in GF(256), with exponents reduced modulo the
// field's multiplicative order. It is the alpha^^i operation used by RFC 6330.
func AlphaPower(exponent uint32) byte {
	return exp[exponent%255]
}
