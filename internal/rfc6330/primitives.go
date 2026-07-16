package rfc6330

import (
	"errors"
	"fmt"
	"sort"
)

const (
	// MaxSourceSymbolsPerBlock is K'_max from RFC 6330 Section 5.3.1.
	MaxSourceSymbolsPerBlock uint32 = 56_403
	// MaxEncodingSymbolID is the largest 24-bit ESI from RFC 6330 Section 3.2.
	MaxEncodingSymbolID uint32 = 1<<24 - 1
	degreeLimit                = 1 << 20
)

var (
	ErrInvalidSourceBlockSize   = errors.New("rfc6330: invalid source block size")
	ErrInvalidExtendedBlockSize = errors.New("rfc6330: invalid extended source block size")
	ErrInvalidEncodingSymbolID  = errors.New("rfc6330: invalid encoding symbol ID")
	ErrInvalidRandomModulus     = errors.New("rfc6330: random modulus must be positive")
	ErrInvalidDegreeInput       = errors.New("rfc6330: invalid degree-generator input")
	ErrNilEncodingIndexVisitor  = errors.New("rfc6330: nil encoding-index visitor")
	ErrEncodingIndexCycle       = errors.New("rfc6330: PI index rejection did not terminate")
)

// InternalSymbolID translates a transmitted ESI to the ISI used by Tuple and
// Enc. Source symbols retain their IDs; repair symbols skip the K'-K implicit
// padding symbols, as specified by RFC 6330 Section 5.3.1.
func InternalSymbolID(k, esi uint32) (uint32, error) {
	parameters, err := ParametersForSourceSymbols(k)
	if err != nil {
		return 0, err
	}
	if esi > MaxEncodingSymbolID {
		return 0, fmt.Errorf("%w: ESI=%d", ErrInvalidEncodingSymbolID, esi)
	}
	if esi < k {
		return esi, nil
	}
	return esi + parameters.KPrime - k, nil
}

// Parameters is one row of the systematic-index table in RFC 6330
// Section 5.6. KPrime is the extended source block size.
type Parameters struct {
	KPrime uint32
	J      uint32
	S      uint32
	H      uint32
	W      uint32
}

// L returns the number of intermediate symbols.
func (parameters Parameters) L() uint32 {
	return parameters.KPrime + parameters.S + parameters.H
}

// P returns the number of PI symbols.
func (parameters Parameters) P() uint32 {
	return parameters.L() - parameters.W
}

// P1 returns the smallest prime greater than or equal to P.
func (parameters Parameters) P1() uint32 {
	return primeAtLeast(parameters.P())
}

// B returns the number of LT symbols that are not LDPC symbols.
func (parameters Parameters) B() uint32 {
	return parameters.W - parameters.S
}

// U returns the number of PI symbols that are not HDPC symbols.
func (parameters Parameters) U() uint32 {
	return parameters.P() - parameters.H
}

// ParametersForSourceSymbols returns the first Table 2 row whose K' is at
// least k, as required by RFC 6330 Section 5.3.1.
func ParametersForSourceSymbols(k uint32) (Parameters, error) {
	if k == 0 || k > MaxSourceSymbolsPerBlock {
		return Parameters{}, fmt.Errorf("%w: K=%d", ErrInvalidSourceBlockSize, k)
	}
	index := sort.Search(len(systematicTable), func(index int) bool {
		return systematicTable[index].kPrime >= k
	})
	if index == len(systematicTable) {
		return Parameters{}, fmt.Errorf("%w: K=%d", ErrInvalidSourceBlockSize, k)
	}
	return parametersFromRow(systematicTable[index]), nil
}

// ParametersForExtendedSourceBlock returns the Table 2 row for an exact K'.
func ParametersForExtendedSourceBlock(kPrime uint32) (Parameters, error) {
	index := sort.Search(len(systematicTable), func(index int) bool {
		return systematicTable[index].kPrime >= kPrime
	})
	if index == len(systematicTable) || systematicTable[index].kPrime != kPrime {
		return Parameters{}, fmt.Errorf("%w: K'=%d", ErrInvalidExtendedBlockSize, kPrime)
	}
	return parametersFromRow(systematicTable[index]), nil
}

// LargestExtendedSourceBlockAtMost returns the largest Table 2 row whose K'
// does not exceed maximum. It returns zero when even the first row is too
// large. This is the KL(n) lookup used by RFC 6330 Section 4.3.
func LargestExtendedSourceBlockAtMost(maximum uint64) uint32 {
	index := sort.Search(len(systematicTable), func(index int) bool {
		return uint64(systematicTable[index].kPrime) > maximum
	})
	if index == 0 {
		return 0
	}
	return systematicTable[index-1].kPrime
}

func parametersFromRow(row systematicParameters) Parameters {
	return Parameters{
		KPrime: row.kPrime,
		J:      row.j,
		S:      row.s,
		H:      row.h,
		W:      row.w,
	}
}

// Rand implements RFC 6330 Section 5.3.5.1. Its uint8 index type makes the
// RFC requirement i < 256 explicit.
func Rand(y uint32, i uint8, modulus uint32) (uint32, error) {
	if modulus == 0 {
		return 0, ErrInvalidRandomModulus
	}
	return rand(y, i, modulus), nil
}

func rand(y uint32, i uint8, modulus uint32) uint32 {
	index := uint32(i)
	x0 := uint8(y + index)
	x1 := uint8((y >> 8) + index)
	x2 := uint8((y >> 16) + index)
	x3 := uint8((y >> 24) + index)
	return (randomTables[0][x0] ^ randomTables[1][x1] ^
		randomTables[2][x2] ^ randomTables[3][x3]) % modulus
}

// Degree implements Deg[v] from RFC 6330 Section 5.3.5.2 for a given W.
func Degree(v, w uint32) (uint32, error) {
	if v >= degreeLimit || w < 3 {
		return 0, fmt.Errorf("%w: v=%d W=%d", ErrInvalidDegreeInput, v, w)
	}
	index := sort.Search(len(degreeThresholds), func(index int) bool {
		return degreeThresholds[index] > v
	})
	degree := uint32(index)
	if maximum := w - 2; degree > maximum {
		degree = maximum
	}
	return degree, nil
}

// EncodingTuple is the (d, a, b, d1, a1, b1) tuple used by Enc[].
type EncodingTuple struct {
	D  uint32
	A  uint32
	B  uint32
	D1 uint32
	A1 uint32
	B1 uint32
}

// Tuple implements RFC 6330 Section 5.3.5.4. kPrime must be an exact Table 2
// extended source block size, and x is an Internal Symbol ID.
func Tuple(kPrime, x uint32) (EncodingTuple, error) {
	parameters, err := ParametersForExtendedSourceBlock(kPrime)
	if err != nil {
		return EncodingTuple{}, err
	}

	a := uint32(53_591) + parameters.J*997
	if a%2 == 0 {
		a++
	}
	b := uint32(10_267) * (parameters.J + 1)
	y := uint32(uint64(b) + uint64(x)*uint64(a))
	v := rand(y, 0, degreeLimit)
	d, err := Degree(v, parameters.W)
	if err != nil {
		return EncodingTuple{}, err
	}
	p1 := parameters.P1()
	d1 := uint32(2)
	if d < 4 {
		d1 += rand(x, 3, 2)
	}
	return EncodingTuple{
		D:  d,
		A:  1 + rand(y, 1, parameters.W-1),
		B:  rand(y, 2, parameters.W),
		D1: d1,
		A1: 1 + rand(x, 4, p1-1),
		B1: rand(x, 5, p1),
	}, nil
}

// VisitEncodingIndices enumerates every intermediate-symbol access performed
// by Enc[K',X] in RFC 6330 Section 5.3.5.3. Visits are deliberately not
// deduplicated: if an index occurs twice, both the coefficient row and payload
// generation must apply it twice so the contributions cancel in GF(2).
func VisitEncodingIndices(kPrime, x uint32, visit func(index uint32) error) error {
	if visit == nil {
		return ErrNilEncodingIndexVisitor
	}
	parameters, err := ParametersForExtendedSourceBlock(kPrime)
	if err != nil {
		return err
	}
	tuple, err := Tuple(kPrime, x)
	if err != nil {
		return err
	}

	b := tuple.B
	if err := visit(b); err != nil {
		return err
	}
	for count := uint32(1); count < tuple.D; count++ {
		b = (b + tuple.A) % parameters.W
		if err := visit(b); err != nil {
			return err
		}
	}

	b1, err := nextPIIndex(tuple.B1, 0, tuple.A1, parameters.P(), parameters.P1())
	if err != nil {
		return err
	}
	if err := visit(parameters.W + b1); err != nil {
		return err
	}
	for count := uint32(1); count < tuple.D1; count++ {
		b1, err = nextPIIndex(b1, tuple.A1, tuple.A1, parameters.P(), parameters.P1())
		if err != nil {
			return err
		}
		if err := visit(parameters.W + b1); err != nil {
			return err
		}
	}
	return nil
}

// nextPIIndex first advances current by initialAdvance and then applies the
// RFC rejection loop. P1 is prime and 1<=step<P1, so at most P1 candidates
// are needed; the bound makes termination explicit in verification builds.
func nextPIIndex(current, initialAdvance, step, p, p1 uint32) (uint32, error) {
	candidate := (current + initialAdvance) % p1
	for attempts := uint32(0); attempts < p1; attempts++ {
		if candidate < p {
			return candidate, nil
		}
		candidate = (candidate + step) % p1
	}
	return 0, ErrEncodingIndexCycle
}

func primeAtLeast(value uint32) uint32 {
	for candidate := value; ; candidate++ {
		if isPrime(candidate) {
			return candidate
		}
	}
}

func isPrime(value uint32) bool {
	if value < 2 {
		return false
	}
	if value%2 == 0 {
		return value == 2
	}
	for divisor := uint32(3); divisor <= value/divisor; divisor += 2 {
		if value%divisor == 0 {
			return false
		}
	}
	return true
}
