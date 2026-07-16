package codec

import (
	"bytes"
	"testing"

	"github.com/fgn/raptorgo/internal/rfc6330"
)

// TestDecoderMatchesIndependentRankOracleBoundedCorpus exercises the RFC 6330
// Section 5.8 SHOULD requirement directly: every received set that the
// independent polynomial-elimination oracle finds mathematically sufficient
// must decode, while a deficient set must not be reported complete.
func TestDecoderMatchesIndependentRankOracleBoundedCorpus(t *testing.T) {
	generator := splitMix64{state: 0x6330_58c0_dec0_de01}
	for _, k := range []uint32{10, 12, 18, 26, 36, 48, 49, 55, 69, 84, 101} {
		parameters, err := rfc6330.ParametersForExtendedSourceBlock(k)
		if err != nil {
			t.Fatal(err)
		}
		source := patternedSymbols(t, k, 1)
		encoder, err := NewSourceBlockEncoder(source, mustEncoderBudget(t, k, 1))
		if err != nil {
			t.Fatal(err)
		}
		for overhead := uint32(0); overhead <= 2; overhead++ {
			for trial := 0; trial < 4; trial++ {
				esis := distinctRandomESIs(&generator, k+overhead)
				rank := constraintRankReference(t, k, esis)
				sufficient := rank == parameters.L()
				decoder, decoderErr := NewSourceBlockDecoder(
					k, 1, exactDecoderLimits(t, k, 1, k+overhead, overhead+1),
				)
				if decoderErr != nil {
					t.Fatal(decoderErr)
				}
				for _, esi := range esis {
					payload, symbolErr := encoder.Symbol(esi, 1)
					if symbolErr != nil {
						t.Fatal(symbolErr)
					}
					complete, addErr := decoder.AddSymbol(esi, payload)
					if addErr != nil {
						t.Fatalf("K'=%d overhead=%d trial=%d ESI=%d: %v", k, overhead, trial, esi, addErr)
					}
					if complete {
						break
					}
				}
				if decoder.Complete() != sufficient {
					t.Fatalf(
						"K'=%d overhead=%d trial=%d rank=%d/L=%d complete=%v ESIs=%v",
						k, overhead, trial, rank, parameters.L(), decoder.Complete(), esis,
					)
				}
				if sufficient {
					result, resultErr := decoder.Result(uint64(k))
					if resultErr != nil {
						t.Fatal(resultErr)
					}
					if !bytes.Equal(snapshotSymbols(t, result), snapshotSymbols(t, source)) {
						t.Fatalf("K'=%d overhead=%d trial=%d recovered wrong source", k, overhead, trial)
					}
				}
			}
		}
	}
}

type splitMix64 struct{ state uint64 }

func (generator *splitMix64) next() uint64 {
	generator.state += 0x9e37_79b9_7f4a_7c15
	value := generator.state
	value = (value ^ value>>30) * 0xbf58_476d_1ce4_e5b9
	value = (value ^ value>>27) * 0x94d0_49bb_1331_11eb
	return value ^ value>>31
}

func distinctRandomESIs(generator *splitMix64, count uint32) []uint32 {
	result := make([]uint32, 0, count)
	seen := make(map[uint32]struct{}, count)
	for uint32(len(result)) < count {
		esi := uint32(generator.next()) & rfc6330.MaxEncodingSymbolID
		if _, duplicate := seen[esi]; duplicate {
			continue
		}
		seen[esi] = struct{}{}
		result = append(result, esi)
	}
	return result
}

func constraintRankReference(t testing.TB, kPrime uint32, esis []uint32) uint32 {
	t.Helper()
	required, err := ConstraintMatrixBackingBytes(kPrime, uint64(len(esis)))
	if err != nil {
		t.Fatal(err)
	}
	matrix, err := BuildConstraintMatrix(kPrime, esis, required)
	if err != nil {
		t.Fatal(err)
	}
	values := make([]byte, int(matrix.Rows()*matrix.Columns()))
	for row := uint32(0); row < matrix.Rows(); row++ {
		for column := uint32(0); column < matrix.Columns(); column++ {
			value, atErr := matrix.At(row, column)
			if atErr != nil {
				t.Fatal(atErr)
			}
			values[int(row*matrix.Columns()+column)] = value
		}
	}
	return polynomialRank(values, matrix.Rows(), matrix.Columns())
}

func polynomialRank(values []byte, rows, columns uint32) uint32 {
	rank := uint32(0)
	for column := uint32(0); column < columns && rank < rows; column++ {
		pivot := rank
		for pivot < rows && values[int(pivot*columns+column)] == 0 {
			pivot++
		}
		if pivot == rows {
			continue
		}
		for index := column; index < columns; index++ {
			left := int(rank*columns + index)
			right := int(pivot*columns + index)
			values[left], values[right] = values[right], values[left]
		}
		pivotValue := values[int(rank*columns+column)]
		inverse := byte(0)
		for candidate := uint16(1); candidate <= 255; candidate++ {
			if polynomialMultiply(pivotValue, byte(candidate)) == 1 {
				inverse = byte(candidate)
				break
			}
		}
		for index := column; index < columns; index++ {
			position := int(rank*columns + index)
			values[position] = polynomialMultiply(values[position], inverse)
		}
		for row := uint32(0); row < rows; row++ {
			if row == rank {
				continue
			}
			factor := values[int(row*columns+column)]
			if factor == 0 {
				continue
			}
			for index := column; index < columns; index++ {
				position := int(row*columns + index)
				values[position] ^= polynomialMultiply(factor, values[int(rank*columns+index)])
			}
		}
		rank++
	}
	return rank
}
