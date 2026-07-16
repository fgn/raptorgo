// Command recoveryprofile emits the preregistered bounded empirical profile
// for RFC 6330 Section 5.8. It is release evidence, not a universal proof.
package main

import (
	"bufio"
	"fmt"
	"math"
	"os"

	"github.com/fgn/raptorgo/internal/codec"
	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

const (
	kPrime   = uint32(10)
	poolSize = uint32(1 << 16)
	seed     = uint64(0x6330_5801_5e8c_0001)
)

var trialCounts = [...]uint32{100_000, 100_000, 3_000_000}

func main() {
	multiplication, inverse := polynomialTables()
	pool := coefficientPool()
	random := splitMix64{state: seed ^ 0xa076_1d64_78bd_642f}
	output := bufio.NewWriter(os.Stdout)
	fmt.Fprintf(
		output,
		"profile=rfc6330-section-5.8-v1 kprime=%d seed=%016x pool=%d confidence=one-sided-wilson-95 sampling=distinct-uniform-from-preregistered-24-bit-esi-pool\n",
		kPrime, seed, poolSize,
	)
	limits := [...]float64{1e-2, 1e-4, 1e-6}
	for overhead, trials := range trialCounts {
		failures := uint32(0)
		for trial := uint32(0); trial < trials; trial++ {
			if !sampleFullRank(&random, pool, uint32(overhead), multiplication, inverse) {
				failures++
			}
		}
		rate := float64(failures) / float64(trials)
		upper := wilsonUpper(failures, trials)
		fmt.Fprintf(
			output,
			"overhead=%d trials=%d failures=%d observed=%.9g upper95=%.9g rfc-limit=%.9g upper-within-limit=%t\n",
			overhead, trials, failures, rate, upper, limits[overhead], upper <= limits[overhead],
		)
	}
	if err := output.Flush(); err != nil {
		panic(err)
	}
}

func coefficientPool() []byte {
	parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
	must(err)
	required, err := codec.ConstraintMatrixBackingBytes(kPrime, uint64(kPrime))
	must(err)
	matrix, err := codec.BuildSourceConstraintMatrix(kPrime, required)
	must(err)
	invertBytes, err := gf256.InvertWorkspaceBytes(parameters.L())
	must(err)
	inverse, err := gf256.Invert(matrix, invertBytes)
	must(err)

	pool := make([]byte, int(poolSize*kPrime))
	random := splitMix64{state: seed}
	seen := make(map[uint32]struct{}, poolSize)
	for entry := uint32(0); entry < poolSize; {
		esi := uint32(random.next()) & rfc6330.MaxEncodingSymbolID
		if _, duplicate := seen[esi]; duplicate {
			continue
		}
		seen[esi] = struct{}{}
		row := pool[entry*kPrime : (entry+1)*kPrime]
		must(rfc6330.VisitEncodingIndices(kPrime, esi, func(index uint32) error {
			for source := uint32(0); source < kPrime; source++ {
				coefficient, atErr := inverse.At(index, parameters.S+parameters.H+source)
				if atErr != nil {
					return atErr
				}
				row[source] ^= coefficient
			}
			return nil
		}))
		entry++
	}

	// The derived generator must preserve every systematic source row exactly.
	for source := uint32(0); source < kPrime; source++ {
		row := make([]byte, kPrime)
		must(rfc6330.VisitEncodingIndices(kPrime, source, func(index uint32) error {
			for column := uint32(0); column < kPrime; column++ {
				coefficient, atErr := inverse.At(index, parameters.S+parameters.H+column)
				if atErr != nil {
					return atErr
				}
				row[column] ^= coefficient
			}
			return nil
		}))
		for column, coefficient := range row {
			want := byte(0)
			if uint32(column) == source {
				want = 1
			}
			if coefficient != want {
				panic(fmt.Sprintf("derived systematic row %d column %d=%d", source, column, coefficient))
			}
		}
	}
	return pool
}

func sampleFullRank(
	random *splitMix64,
	pool []byte,
	overhead uint32,
	multiplication *[1 << 16]byte,
	inverse *[256]byte,
) bool {
	var selected [12]uint32
	count := kPrime + overhead
	for index := uint32(0); index < count; {
		candidate := uint32(random.next()) & (poolSize - 1)
		duplicate := false
		for prior := uint32(0); prior < index; prior++ {
			if selected[prior] == candidate {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		selected[index] = candidate
		index++
	}

	var basis [100]byte
	rank := uint32(0)
	for index := uint32(0); index < count && rank < kPrime; index++ {
		var row [10]byte
		copy(row[:], pool[selected[index]*kPrime:(selected[index]+1)*kPrime])
		for column := uint32(0); column < kPrime; column++ {
			coefficient := row[column]
			if coefficient == 0 {
				continue
			}
			pivot := basis[column*kPrime : (column+1)*kPrime]
			if pivot[column] == 0 {
				scale := inverse[coefficient]
				for target := column; target < kPrime; target++ {
					row[target] = multiplication[uint16(scale)<<8|uint16(row[target])]
				}
				copy(pivot, row[:])
				rank++
				break
			}
			for target := column; target < kPrime; target++ {
				row[target] ^= multiplication[uint16(coefficient)<<8|uint16(pivot[target])]
			}
		}
	}
	return rank == kPrime
}

func polynomialTables() (*[1 << 16]byte, *[256]byte) {
	multiplication := new([1 << 16]byte)
	for left := uint16(0); left <= 255; left++ {
		for right := uint16(0); right <= 255; right++ {
			multiplication[left<<8|right] = polynomialMultiply(byte(left), byte(right))
		}
	}
	inverse := new([256]byte)
	for value := uint16(1); value <= 255; value++ {
		for candidate := uint16(1); candidate <= 255; candidate++ {
			if multiplication[value<<8|candidate] == 1 {
				inverse[value] = byte(candidate)
				break
			}
		}
	}
	return multiplication, inverse
}

func polynomialMultiply(left, right byte) byte {
	var product byte
	for right != 0 {
		if right&1 != 0 {
			product ^= left
		}
		high := left&0x80 != 0
		left <<= 1
		if high {
			left ^= 0x1d
		}
		right >>= 1
	}
	return product
}

func wilsonUpper(failures, trials uint32) float64 {
	const z = 1.644_853_626_951_472_2
	n := float64(trials)
	p := float64(failures) / n
	z2 := z * z
	denominator := 1 + z2/n
	center := (p + z2/(2*n)) / denominator
	margin := z * math.Sqrt(p*(1-p)/n+z2/(4*n*n)) / denominator
	return center + margin
}

type splitMix64 struct{ state uint64 }

func (generator *splitMix64) next() uint64 {
	generator.state += 0x9e37_79b9_7f4a_7c15
	value := generator.state
	value = (value ^ value>>30) * 0xbf58_476d_1ce4_e5b9
	value = (value ^ value>>27) * 0x94d0_49bb_1331_11eb
	return value ^ value>>31
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
