package rfc6330

import (
	"errors"
	"math"
	"slices"
	"testing"
)

func TestParametersForSourceSymbolsExhaustiveMinimalMapping(t *testing.T) {
	t.Parallel()

	rowIndex := 0
	for k := uint32(1); k <= MaxSourceSymbolsPerBlock; k++ {
		for systematicTable[rowIndex].kPrime < k {
			rowIndex++
		}
		got, err := ParametersForSourceSymbols(k)
		if err != nil {
			t.Fatalf("ParametersForSourceSymbols(%d): %v", k, err)
		}
		want := parametersFromRow(systematicTable[rowIndex])
		if got != want {
			t.Fatalf("ParametersForSourceSymbols(%d) = %+v, want %+v", k, got, want)
		}
		if got.KPrime < k {
			t.Fatalf("K=%d mapped below itself to K'=%d", k, got.KPrime)
		}
		if rowIndex > 0 && systematicTable[rowIndex-1].kPrime >= k {
			t.Fatalf("K=%d did not map to the smallest eligible row", k)
		}
	}
}

func TestParametersRejectInvalidSizes(t *testing.T) {
	t.Parallel()

	for _, k := range []uint32{0, MaxSourceSymbolsPerBlock + 1, math.MaxUint32} {
		if _, err := ParametersForSourceSymbols(k); !errors.Is(err, ErrInvalidSourceBlockSize) {
			t.Errorf("ParametersForSourceSymbols(%d) error = %v", k, err)
		}
	}
	for _, kPrime := range []uint32{0, 1, 11, MaxSourceSymbolsPerBlock - 1, math.MaxUint32} {
		if _, err := ParametersForExtendedSourceBlock(kPrime); !errors.Is(err, ErrInvalidExtendedBlockSize) {
			t.Errorf("ParametersForExtendedSourceBlock(%d) error = %v", kPrime, err)
		}
	}
}

func TestLargestExtendedSourceBlockAtMostBoundaries(t *testing.T) {
	t.Parallel()
	if got := LargestExtendedSourceBlockAtMost(9); got != 0 {
		t.Fatalf("maximum 9 selected K'=%d", got)
	}
	for index, row := range systematicTable {
		if got := LargestExtendedSourceBlockAtMost(uint64(row.kPrime)); got != row.kPrime {
			t.Fatalf("row %d exact maximum selected K'=%d, want %d", index, got, row.kPrime)
		}
		if index+1 < len(systematicTable) {
			next := systematicTable[index+1].kPrime
			if got := LargestExtendedSourceBlockAtMost(uint64(next - 1)); got != row.kPrime {
				t.Fatalf("row %d gap maximum selected K'=%d, want %d", index, got, row.kPrime)
			}
		}
	}
	if got := LargestExtendedSourceBlockAtMost(^uint64(0)); got != MaxSourceSymbolsPerBlock {
		t.Fatalf("unbounded maximum selected K'=%d", got)
	}
}

func TestExactParametersAndDerivedInvariants(t *testing.T) {
	t.Parallel()

	for index, row := range systematicTable {
		parameters, err := ParametersForExtendedSourceBlock(row.kPrime)
		if err != nil {
			t.Fatalf("row %d K'=%d: %v", index, row.kPrime, err)
		}
		if parameters != parametersFromRow(row) {
			t.Fatalf("row %d parameters = %+v, want %+v", index, parameters, parametersFromRow(row))
		}
		if parameters.L() != parameters.KPrime+parameters.S+parameters.H {
			t.Fatalf("row %d has inconsistent L", index)
		}
		if parameters.P() != parameters.L()-parameters.W || parameters.P() < parameters.H {
			t.Fatalf("row %d has inconsistent P", index)
		}
		if parameters.B() != parameters.W-parameters.S {
			t.Fatalf("row %d has inconsistent B", index)
		}
		if parameters.U() != parameters.P()-parameters.H {
			t.Fatalf("row %d has inconsistent U", index)
		}
		p1 := parameters.P1()
		if p1 < parameters.P() || !isPrimeReference(p1) {
			t.Fatalf("row %d P1=%d is not prime or is below P=%d", index, p1, parameters.P())
		}
		for candidate := parameters.P(); candidate < p1; candidate++ {
			if isPrimeReference(candidate) {
				t.Fatalf("row %d P1=%d is not minimal; %d is prime", index, p1, candidate)
			}
		}
	}
}

func TestInternalSymbolIDExhaustiveBoundaries(t *testing.T) {
	t.Parallel()

	for k := uint32(1); k <= MaxSourceSymbolsPerBlock; k++ {
		parameters, err := ParametersForSourceSymbols(k)
		if err != nil {
			t.Fatal(err)
		}
		for _, esi := range []uint32{0, k - 1, k, MaxEncodingSymbolID} {
			got, err := InternalSymbolID(k, esi)
			if err != nil {
				t.Fatalf("InternalSymbolID(%d, %d): %v", k, esi, err)
			}
			want := esi
			if esi >= k {
				want += parameters.KPrime - k
			}
			if got != want {
				t.Fatalf("InternalSymbolID(%d, %d) = %d, want %d", k, esi, got, want)
			}
			if esi < k && got != esi {
				t.Fatalf("source ESI %d changed to ISI %d for K=%d", esi, got, k)
			}
			if esi == k && got != parameters.KPrime {
				t.Fatalf("first repair ESI %d mapped to ISI %d, want K'=%d", esi, got, parameters.KPrime)
			}
		}
	}
}

func TestInternalSymbolIDRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	if _, err := InternalSymbolID(0, 0); !errors.Is(err, ErrInvalidSourceBlockSize) {
		t.Fatalf("zero K error = %v", err)
	}
	if _, err := InternalSymbolID(1, MaxEncodingSymbolID+1); !errors.Is(err, ErrInvalidEncodingSymbolID) {
		t.Fatalf("oversize ESI error = %v", err)
	}
}

func TestRandRustRaptorQKnownAnswers(t *testing.T) {
	t.Parallel()

	// Generated independently with raptorq 2.0.1's Rand implementation.
	// The overflowing y+i case uses a release build, whose wrapping arithmetic
	// matches the RFC's modulo-256 definition.
	tests := []struct {
		y       uint32
		i       uint8
		modulus uint32
		want    uint32
	}{
		{0, 0, math.MaxUint32, 415381529},
		{1, 1, 1 << 20, 722408},
		{0x01234567, 89, 56951, 22514},
		{math.MaxUint32, math.MaxUint8, 379, 273},
		{0xdeadbeef, 17, 1, 0},
	}
	for _, test := range tests {
		got, err := Rand(test.y, test.i, test.modulus)
		if err != nil {
			t.Fatalf("Rand(%d, %d, %d): %v", test.y, test.i, test.modulus, err)
		}
		if got != test.want {
			t.Errorf("Rand(%d, %d, %d) = %d, want %d", test.y, test.i, test.modulus, got, test.want)
		}
	}
	if _, err := Rand(0, 0, 0); !errors.Is(err, ErrInvalidRandomModulus) {
		t.Fatalf("Rand with zero modulus error = %v", err)
	}
}

func TestDegreeThresholdsAndClamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		v    uint32
		w    uint32
		want uint32
	}{
		{0, 100, 1},
		{5242, 100, 1},
		{5243, 100, 2},
		{529530, 100, 2},
		{529531, 100, 3},
		{degreeLimit - 1, 100, 30},
		{degreeLimit - 1, 17, 15},
		{0, 3, 1},
	}
	for _, test := range tests {
		got, err := Degree(test.v, test.w)
		if err != nil {
			t.Fatalf("Degree(%d, %d): %v", test.v, test.w, err)
		}
		if got != test.want {
			t.Errorf("Degree(%d, %d) = %d, want %d", test.v, test.w, got, test.want)
		}
	}
	for _, test := range []struct{ v, w uint32 }{{degreeLimit, 17}, {math.MaxUint32, 17}, {0, 0}, {0, 2}} {
		if _, err := Degree(test.v, test.w); !errors.Is(err, ErrInvalidDegreeInput) {
			t.Errorf("Degree(%d, %d) error = %v", test.v, test.w, err)
		}
	}
}

func TestDegreeExhaustiveDistribution(t *testing.T) {
	t.Parallel()

	previous := uint32(0)
	for v := uint32(0); v < degreeLimit; v++ {
		degree, err := Degree(v, math.MaxUint32)
		if err != nil {
			t.Fatalf("Degree(%d): %v", v, err)
		}
		if degree < 1 || degree > 30 {
			t.Fatalf("Degree(%d) = %d outside [1,30]", v, degree)
		}
		if degree < previous || degree > previous+1 {
			t.Fatalf("Degree jumped from %d to %d at v=%d", previous, degree, v)
		}
		if v < degreeThresholds[degree-1] || v >= degreeThresholds[degree] {
			t.Fatalf("Degree(%d)=%d violates f[d-1] <= v < f[d]", v, degree)
		}
		previous = degree
	}
}

func TestTupleRustRaptorQKnownAnswers(t *testing.T) {
	t.Parallel()

	// Generated independently with raptorq 2.0.1's random tables and RFC tuple
	// arithmetic. These include minimum/maximum K' and uint32 wraparound.
	tests := []struct {
		kPrime uint32
		x      uint32
		want   EncodingTuple
	}{
		{10, 0, EncodingTuple{D: 2, A: 4, B: 9, D1: 2, A1: 5, B1: 1}},
		{10, 9, EncodingTuple{D: 6, A: 3, B: 16, D1: 2, A1: 6, B1: 4}},
		{10, 10, EncodingTuple{D: 2, A: 15, B: 15, D1: 2, A1: 10, B1: 7}},
		{49, (1 << 24) - 1, EncodingTuple{D: 2, A: 58, B: 9, D1: 3, A1: 2, B1: 5}},
		{56403, 0, EncodingTuple{D: 2, A: 50588, B: 9917, D1: 2, A1: 55, B1: 128}},
		{56403, math.MaxUint32, EncodingTuple{D: 2, A: 12964, B: 18044, D1: 3, A1: 57, B1: 86}},
	}
	for _, test := range tests {
		got, err := Tuple(test.kPrime, test.x)
		if err != nil {
			t.Fatalf("Tuple(%d, %d): %v", test.kPrime, test.x, err)
		}
		if got != test.want {
			t.Errorf("Tuple(%d, %d) = %+v, want %+v", test.kPrime, test.x, got, test.want)
		}
	}
	if _, err := Tuple(11, 0); !errors.Is(err, ErrInvalidExtendedBlockSize) {
		t.Fatalf("Tuple with non-table K' error = %v", err)
	}
}

func TestTupleDomainsAcrossSystematicTable(t *testing.T) {
	t.Parallel()

	for _, row := range systematicTable {
		parameters := parametersFromRow(row)
		for _, x := range []uint32{0, row.kPrime - 1, row.kPrime, (1 << 24) - 1, math.MaxUint32} {
			tuple, err := Tuple(row.kPrime, x)
			if err != nil {
				t.Fatalf("Tuple(%d, %d): %v", row.kPrime, x, err)
			}
			if tuple.D < 1 || tuple.D > min(30, parameters.W-2) {
				t.Fatalf("Tuple(%d, %d) D=%d outside its domain", row.kPrime, x, tuple.D)
			}
			if tuple.A < 1 || tuple.A >= parameters.W || tuple.B >= parameters.W {
				t.Fatalf("Tuple(%d, %d) LT fields outside their domains: %+v", row.kPrime, x, tuple)
			}
			if tuple.D1 < 2 || tuple.D1 > 3 || (tuple.D >= 4 && tuple.D1 != 2) {
				t.Fatalf("Tuple(%d, %d) D1 inconsistent with D: %+v", row.kPrime, x, tuple)
			}
			p1 := parameters.P1()
			if tuple.A1 < 1 || tuple.A1 >= p1 || tuple.B1 >= p1 {
				t.Fatalf("Tuple(%d, %d) PI fields outside their domains: %+v", row.kPrime, x, tuple)
			}
		}
	}
}

func TestVisitEncodingIndicesAcrossSystematicTable(t *testing.T) {
	t.Parallel()

	for _, row := range systematicTable {
		parameters := parametersFromRow(row)
		for _, x := range tupleIDsForTest(parameters.KPrime) {
			tuple, err := Tuple(parameters.KPrime, x)
			if err != nil {
				t.Fatal(err)
			}
			var got []uint32
			if err := VisitEncodingIndices(parameters.KPrime, x, func(index uint32) error {
				got = append(got, index)
				return nil
			}); err != nil {
				t.Fatalf("VisitEncodingIndices(%d,%d): %v", parameters.KPrime, x, err)
			}
			want := referenceEncodingIndices(tuple, parameters.W, parameters.P(), parameters.P1())
			if !slices.Equal(got, want) {
				t.Fatalf("VisitEncodingIndices(%d,%d) = %v, want %v", parameters.KPrime, x, got, want)
			}
			if len(got) != int(tuple.D+tuple.D1) {
				t.Fatalf("VisitEncodingIndices(%d,%d) count = %d, want %d", parameters.KPrime, x, len(got), tuple.D+tuple.D1)
			}
			for visit, index := range got {
				if index >= parameters.L() {
					t.Fatalf("VisitEncodingIndices(%d,%d)[%d]=%d >= L=%d", parameters.KPrime, x, visit, index, parameters.L())
				}
				if visit < int(tuple.D) && index >= parameters.W {
					t.Fatalf("LT visit %d is outside W: %d", visit, index)
				}
				if visit >= int(tuple.D) && index < parameters.W {
					t.Fatalf("PI visit %d is below W: %d", visit, index)
				}
			}
		}
	}
}

func TestVisitEncodingIndicesVisitorFailureAndValidation(t *testing.T) {
	t.Parallel()

	if err := VisitEncodingIndices(10, 0, nil); !errors.Is(err, ErrNilEncodingIndexVisitor) {
		t.Fatalf("nil visitor error = %v", err)
	}
	if err := VisitEncodingIndices(11, 0, func(uint32) error { return nil }); !errors.Is(err, ErrInvalidExtendedBlockSize) {
		t.Fatalf("invalid K' error = %v", err)
	}
	sentinel := errors.New("visitor stopped")
	visits := 0
	err := VisitEncodingIndices(10, 0, func(uint32) error {
		visits++
		return sentinel
	})
	if !errors.Is(err, sentinel) || visits != 1 {
		t.Fatalf("visitor failure = %v after %d visits", err, visits)
	}
	if _, err := nextPIIndex(0, 0, 1, 0, 5); !errors.Is(err, ErrEncodingIndexCycle) {
		t.Fatalf("impossible PI domain error = %v", err)
	}
}

func TestVisitEncodingIndicesExercisesPIRejectionSentinels(t *testing.T) {
	t.Parallel()

	for _, kPrime := range []uint32{49, 5225} {
		parameters, err := ParametersForExtendedSourceBlock(kPrime)
		if err != nil {
			t.Fatal(err)
		}
		foundRejectedCandidate := false
		for x := uint32(0); x < 100_000; x++ {
			tuple, err := Tuple(kPrime, x)
			if err != nil {
				t.Fatal(err)
			}
			if tuple.B1 >= parameters.P() || (tuple.B1+tuple.A1)%parameters.P1() >= parameters.P() {
				foundRejectedCandidate = true
				visitNumber := uint32(0)
				if err := VisitEncodingIndices(kPrime, x, func(index uint32) error {
					if visitNumber >= tuple.D && (index < parameters.W || index >= parameters.L()) {
						t.Fatalf("K'=%d X=%d produced invalid PI index %d", kPrime, x, index)
					}
					visitNumber++
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
		if parameters.P1() == parameters.P() {
			if foundRejectedCandidate {
				t.Fatalf("K'=%d has P1=P but found a rejected candidate", kPrime)
			}
		} else if !foundRejectedCandidate {
			t.Fatalf("K'=%d did not exercise PI rejection", kPrime)
		}
	}
}

func FuzzRandRange(f *testing.F) {
	f.Add(uint32(0), uint8(0), uint32(1))
	f.Add(uint32(math.MaxUint32), uint8(math.MaxUint8), uint32(math.MaxUint32))
	f.Fuzz(func(t *testing.T, y uint32, i uint8, modulus uint32) {
		got, err := Rand(y, i, modulus)
		if modulus == 0 {
			if !errors.Is(err, ErrInvalidRandomModulus) {
				t.Fatalf("zero modulus error = %v", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if got >= modulus {
			t.Fatalf("Rand(%d, %d, %d) = %d", y, i, modulus, got)
		}
	})
}

func FuzzTupleDomains(f *testing.F) {
	f.Add(uint32(1), uint32(0))
	f.Add(MaxSourceSymbolsPerBlock, uint32(math.MaxUint32))
	f.Fuzz(func(t *testing.T, seed, x uint32) {
		k := seed%MaxSourceSymbolsPerBlock + 1
		parameters, err := ParametersForSourceSymbols(k)
		if err != nil {
			t.Fatal(err)
		}
		tuple, err := Tuple(parameters.KPrime, x)
		if err != nil {
			t.Fatal(err)
		}
		if tuple.D < 1 || tuple.D > min(30, parameters.W-2) ||
			tuple.A < 1 || tuple.A >= parameters.W || tuple.B >= parameters.W ||
			tuple.D1 < 2 || tuple.D1 > 3 || tuple.A1 < 1 ||
			tuple.A1 >= parameters.P1() || tuple.B1 >= parameters.P1() {
			t.Fatalf("Tuple(%d, %d) outside its domains: %+v", parameters.KPrime, x, tuple)
		}
	})
}

func tupleIDsForTest(kPrime uint32) []uint32 {
	return []uint32{0, kPrime - 1, kPrime, MaxEncodingSymbolID, math.MaxUint32, kPrime * 2_654_435_761}
}

func referenceEncodingIndices(tuple EncodingTuple, w, p, p1 uint32) []uint32 {
	indices := make([]uint32, 0, tuple.D+tuple.D1)
	b := tuple.B
	indices = append(indices, b)
	for count := uint32(1); count < tuple.D; count++ {
		b = (b + tuple.A) % w
		indices = append(indices, b)
	}
	b1 := tuple.B1
	for b1 >= p {
		b1 = (b1 + tuple.A1) % p1
	}
	indices = append(indices, w+b1)
	for count := uint32(1); count < tuple.D1; count++ {
		b1 = (b1 + tuple.A1) % p1
		for b1 >= p {
			b1 = (b1 + tuple.A1) % p1
		}
		indices = append(indices, w+b1)
	}
	return indices
}
