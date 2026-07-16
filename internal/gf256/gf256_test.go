package gf256

import "testing"

func TestRFCExponentPrefix(t *testing.T) {
	if len(exp) != 510 || len(log) != 256 {
		t.Fatalf("RFC table lengths = exp %d, log %d; want 510 and 256", len(exp), len(log))
	}
	// RFC 6330 Section 5.7.3, first 32 OCT_EXP entries.
	want := [...]byte{
		1, 2, 4, 8, 16, 32, 64, 128, 29, 58, 116, 232, 205, 135, 19, 38,
		76, 152, 45, 90, 180, 117, 234, 201, 143, 3, 6, 12, 24, 48, 96, 192,
	}
	for i, value := range want {
		if exp[i] != value {
			t.Fatalf("OCT_EXP[%d] = %d, want %d", i, exp[i], value)
		}
	}
}

func TestExponentAndLogAreInverses(t *testing.T) {
	seen := [256]bool{}
	for i := 0; i < 255; i++ {
		value := exp[i]
		if value == 0 || seen[value] {
			t.Fatalf("OCT_EXP is not a permutation at index %d (value %d)", i, value)
		}
		seen[value] = true
		if got := log[value]; int(got) != i {
			t.Fatalf("OCT_LOG[OCT_EXP[%d]] = %d", i, got)
		}
		if exp[i+255] != value {
			t.Fatalf("OCT_EXP repeat differs at index %d", i+255)
		}
	}
}

func TestMultiplicationMatchesPolynomialReductionExhaustive(t *testing.T) {
	for ai := 0; ai < 256; ai++ {
		for bi := 0; bi < 256; bi++ {
			a, b := byte(ai), byte(bi)
			if got, want := Mul(a, b), polynomialMul(a, b); got != want {
				t.Fatalf("Mul(%d, %d) = %d, polynomial reduction gives %d", a, b, got, want)
			}
		}
	}
}

// polynomialMul is deliberately independent of the production log/exp tables.
func polynomialMul(a, b byte) byte {
	var product byte
	for b != 0 {
		if b&1 != 0 {
			product ^= a
		}
		high := a&0x80 != 0
		a <<= 1
		if high {
			a ^= 0x1d // Low eight bits of the RFC polynomial 0x11d.
		}
		b >>= 1
	}
	return product
}

func TestFieldIdentitiesExhaustive(t *testing.T) {
	for ai := 0; ai < 256; ai++ {
		a := byte(ai)
		if Add(a, a) != 0 || Add(a, 0) != a {
			t.Fatalf("additive identity failed for %d", a)
		}
		if Mul(a, 0) != 0 || Mul(a, 1) != a {
			t.Fatalf("multiplicative identity failed for %d", a)
		}

		inverse, invertible := Inv(a)
		if a == 0 {
			if invertible {
				t.Fatal("zero unexpectedly has an inverse")
			}
		} else if !invertible || Mul(a, inverse) != 1 {
			t.Fatalf("inverse failed for %d", a)
		}

		for bi := 0; bi < 256; bi++ {
			b := byte(bi)
			if Mul(a, b) != Mul(b, a) {
				t.Fatalf("multiplication is not commutative for %d, %d", a, b)
			}
			if b != 0 {
				quotient, ok := Div(Mul(a, b), b)
				if !ok || quotient != a {
					t.Fatalf("division failed for %d, %d", a, b)
				}
			}
		}
	}
}

func TestAssociativityAndDistributivityExhaustive(t *testing.T) {
	for ai := 0; ai < 256; ai++ {
		a := byte(ai)
		for bi := 0; bi < 256; bi++ {
			b := byte(bi)
			ab := Mul(a, b)
			for ci := 0; ci < 256; ci++ {
				c := byte(ci)
				if Mul(ab, c) != Mul(a, Mul(b, c)) {
					t.Fatalf("multiplication is not associative for %d, %d, %d", a, b, c)
				}
				if Mul(a, Add(b, c)) != Add(Mul(a, b), Mul(a, c)) {
					t.Fatalf("multiplication is not distributive for %d, %d, %d", a, b, c)
				}
			}
		}
	}
}

func TestDivisionByZero(t *testing.T) {
	if _, ok := Div(1, 0); ok {
		t.Fatal("division by zero unexpectedly succeeded")
	}
}

func TestAlphaPowerPeriodicAndIndependent(t *testing.T) {
	t.Parallel()

	want := byte(1)
	for exponent := uint32(0); exponent < 4096; exponent++ {
		if got := AlphaPower(exponent); got != want {
			t.Fatalf("AlphaPower(%d) = %d, want %d", exponent, got, want)
		}
		if AlphaPower(exponent) != AlphaPower(exponent+255) {
			t.Fatalf("AlphaPower is not periodic at %d", exponent)
		}
		want = polynomialMul(want, 2)
	}
}
