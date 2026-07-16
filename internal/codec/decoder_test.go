package codec

import (
	"bytes"
	"errors"
	"math"
	"math/rand"
	"testing"

	"github.com/fgn/raptorgo/internal/rfc6330"
)

func TestSourceBlockDecoderRecoversLossReorderPaddingAndRepairs(t *testing.T) {
	t.Parallel()

	const k, symbolSize = uint32(13), uint32(31) // K'=18 exercises five synthetic padding rows.
	source := patternedSymbols(t, k, symbolSize)
	encoderBudget, err := SourceBlockEncoderWorkspaceBackingBytes(k, symbolSize)
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := NewSourceBlockEncoder(source, encoderBudget)
	if err != nil {
		t.Fatal(err)
	}
	limits := exactDecoderLimits(t, k, symbolSize, k+8, 9)
	decoder, err := NewSourceBlockDecoder(k, symbolSize, limits)
	if err != nil {
		t.Fatal(err)
	}

	ids := []uint32{0, 2, 4, 6, 8, 10, 12, k, k + 1, k + 2, k + 3, k + 4, k + 5, k + 6, k + 7}
	random := rand.New(rand.NewSource(991)) //nolint:gosec -- deterministic reorder trace
	random.Shuffle(len(ids), func(left, right int) { ids[left], ids[right] = ids[right], ids[left] })
	for _, esi := range ids {
		payload, err := encoder.Symbol(esi, uint64(symbolSize))
		if err != nil {
			t.Fatal(err)
		}
		complete, err := decoder.AddSymbol(esi, payload)
		if err != nil {
			t.Fatalf("ESI %d: %v", esi, err)
		}
		if complete {
			break
		}
	}
	if !decoder.Complete() {
		t.Fatalf("not complete after %d retained symbols and %d attempts", decoder.RetainedSymbols(), decoder.DecodeAttempts())
	}
	result, err := decoder.Result(uint64(k) * uint64(symbolSize))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshotSymbols(t, result), snapshotSymbols(t, source)) {
		t.Fatalf("decoded=%x want=%x", snapshotSymbols(t, result), snapshotSymbols(t, source))
	}
	resultSymbol, err := result.Symbol(0)
	if err != nil {
		t.Fatal(err)
	}
	resultSymbol[0] ^= 0xff
	again, err := decoder.Result(uint64(k) * uint64(symbolSize))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshotSymbols(t, again), snapshotSymbols(t, source)) {
		t.Fatal("caller mutation changed retained decoded result")
	}
}

func TestSourceBlockDecoderAllSystematicAndDuplicateContracts(t *testing.T) {
	t.Parallel()

	const k, symbolSize = uint32(10), uint32(7)
	source := patternedSymbols(t, k, symbolSize)
	limits := exactDecoderLimits(t, k, symbolSize, k+2, 3)
	decoder, err := NewSourceBlockDecoder(k, symbolSize, limits)
	if err != nil {
		t.Fatal(err)
	}
	first, err := source.Symbol(0)
	if err != nil {
		t.Fatal(err)
	}
	if complete, err := decoder.AddSymbol(0, first); err != nil || complete {
		t.Fatalf("first add complete=%v error=%v", complete, err)
	}
	retained := decoder.RetainedSymbols()
	if complete, err := decoder.AddSymbol(0, append([]byte(nil), first...)); err != nil || complete {
		t.Fatalf("duplicate complete=%v error=%v", complete, err)
	}
	if decoder.RetainedSymbols() != retained {
		t.Fatal("identical duplicate consumed retention")
	}
	conflict := append([]byte(nil), first...)
	conflict[0] ^= 0xff
	if complete, err := decoder.AddSymbol(0, conflict); !errors.Is(err, ErrDecoderDuplicateConflict) || complete {
		t.Fatalf("conflicting duplicate complete=%v error=%v", complete, err)
	}
	if decoder.RetainedSymbols() != retained {
		t.Fatal("conflicting duplicate mutated retention")
	}
	for esi := uint32(1); esi < k; esi++ {
		payload, err := source.Symbol(esi)
		if err != nil {
			t.Fatal(err)
		}
		complete, err := decoder.AddSymbol(esi, payload)
		if err != nil {
			t.Fatal(err)
		}
		if complete != (esi == k-1) {
			t.Fatalf("ESI %d complete=%v", esi, complete)
		}
	}
	if decoder.DecodeAttempts() != 1 {
		t.Fatalf("attempts=%d, want 1", decoder.DecodeAttempts())
	}
	if complete, err := decoder.AddSymbol(0, first); err != nil || !complete {
		t.Fatalf("terminal duplicate complete=%v error=%v", complete, err)
	}
	newPayload := make([]byte, symbolSize)
	if complete, err := decoder.AddSymbol(k, newPayload); !errors.Is(err, ErrDecoderComplete) || !complete {
		t.Fatalf("post-complete new symbol complete=%v error=%v", complete, err)
	}
}

func TestSourceBlockDecoderBudgetAndInputValidation(t *testing.T) {
	t.Parallel()

	const k, symbolSize, retained = uint32(10), uint32(7), uint32(13)
	parameters, err := rfc6330.ParametersForSourceSymbols(k)
	if err != nil {
		t.Fatal(err)
	}
	persistent, err := SourceBlockDecoderBackingBytes(k, symbolSize, retained)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := SourceBlockDecoderWorstAttemptBackingBytes(k, symbolSize, retained, parameters.L())
	if err != nil {
		t.Fatal(err)
	}
	limits := SourceBlockDecoderLimits{
		MaxRetainedSymbols: retained, MaxDecodeAttempts: 4,
		MaxDenseCompletionSymbols: parameters.L(), MaxWorkingMemory: persistent + attempt,
	}
	decoder, err := NewSourceBlockDecoder(k, symbolSize, limits)
	if err != nil {
		t.Fatal(err)
	}
	if decoder.BackingBytes() != persistent {
		t.Fatalf("persistent=%d, want %d", decoder.BackingBytes(), persistent)
	}
	limits.MaxWorkingMemory--
	if result, err := NewSourceBlockDecoder(k, symbolSize, limits); !errors.Is(err, ErrDecoderBacking) || result != nil {
		t.Fatalf("budget-1 decoder=%v error=%v", result, err)
	}
	if _, err := NewSourceBlockDecoder(k, symbolSize, SourceBlockDecoderLimits{}); !errors.Is(err, ErrDecoderLimits) {
		t.Fatalf("zero limits error=%v", err)
	}
	boundaryParameters, err := rfc6330.ParametersForSourceSymbols(MaxEagerSourceSymbols + 1)
	if err != nil {
		t.Fatal(err)
	}
	oversizeLimits := SourceBlockDecoderLimits{
		MaxRetainedSymbols: MaxEagerSourceSymbols + 1, MaxDecodeAttempts: 1,
		MaxDenseCompletionSymbols: boundaryParameters.L(), MaxWorkingMemory: math.MaxUint64,
	}
	if decoder, err := NewSourceBlockDecoder(MaxEagerSourceSymbols+1, 1, oversizeLimits); err != nil || decoder == nil {
		t.Fatalf("production hybrid decoder boundary+1=%v error=%v", decoder, err)
	}
	if _, err := SourceBlockDecoderBackingBytes(k, symbolSize, k-1); !errors.Is(err, ErrDecoderLimits) {
		t.Fatalf("retained<K error=%v", err)
	}
	if complete, err := decoder.AddSymbol(math.MaxUint32, make([]byte, symbolSize)); !errors.Is(err, ErrDecoderSymbol) || complete {
		t.Fatalf("invalid ESI complete=%v error=%v", complete, err)
	}
	if complete, err := decoder.AddSymbol(0, make([]byte, symbolSize-1)); !errors.Is(err, ErrDecoderSymbol) || complete {
		t.Fatalf("short payload complete=%v error=%v", complete, err)
	}
	if result, err := decoder.Result(math.MaxUint64); !errors.Is(err, ErrDecoderNotComplete) || result != nil {
		t.Fatalf("early result=%v error=%v", result, err)
	}
}

func FuzzSourceBlockDecoderSystematicOrder(f *testing.F) {
	f.Add([]byte{9, 8, 7, 6, 5, 4, 3, 2, 1, 0})
	f.Add([]byte{0, 0, 1, 1, 2, 2})
	f.Fuzz(func(t *testing.T, order []byte) {
		const k, symbolSize = uint32(10), uint32(3)
		source := patternedSymbols(t, k, symbolSize)
		decoder, err := NewSourceBlockDecoder(k, symbolSize, exactDecoderLimits(t, k, symbolSize, k, 1))
		if err != nil {
			t.Fatal(err)
		}
		seen := make([]bool, k)
		for _, value := range order {
			esi := uint32(value) % k
			if seen[esi] {
				continue
			}
			seen[esi] = true
			payload, err := source.Symbol(esi)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decoder.AddSymbol(esi, payload); err != nil {
				t.Fatal(err)
			}
		}
		for esi := uint32(0); esi < k; esi++ {
			if seen[esi] {
				continue
			}
			payload, err := source.Symbol(esi)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decoder.AddSymbol(esi, payload); err != nil {
				t.Fatal(err)
			}
		}
		if !decoder.Complete() {
			t.Fatal("all systematic symbols did not complete")
		}
		result, err := decoder.Result(uint64(k) * uint64(symbolSize))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(snapshotSymbols(t, result), snapshotSymbols(t, source)) {
			t.Fatal("systematic reorder changed decoded source")
		}
	})
}

func exactDecoderLimits(t testing.TB, k, symbolSize, retained, attempts uint32) SourceBlockDecoderLimits {
	t.Helper()
	persistent, err := SourceBlockDecoderBackingBytes(k, symbolSize, retained)
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := rfc6330.ParametersForSourceSymbols(k)
	if err != nil {
		t.Fatal(err)
	}
	working, err := SourceBlockDecoderWorstAttemptBackingBytes(k, symbolSize, retained, parameters.L())
	if err != nil {
		t.Fatal(err)
	}
	return SourceBlockDecoderLimits{
		MaxRetainedSymbols:        retained,
		MaxDecodeAttempts:         attempts,
		MaxDenseCompletionSymbols: parameters.L(),
		MaxWorkingMemory:          persistent + working,
	}
}

func TestSourceBlockDecoderParameterPaddingSentinel(t *testing.T) {
	parameters, err := rfc6330.ParametersForSourceSymbols(13)
	if err != nil {
		t.Fatal(err)
	}
	if parameters.KPrime-13 != 5 {
		t.Fatalf("K=13 padding=%d, want 5", parameters.KPrime-13)
	}
}

func TestSourceBlockDecoderRetriesAfterRankDeficiencyAndEnforcesCaps(t *testing.T) {
	t.Parallel()

	const k, symbolSize = uint32(10), uint32(1)
	source := patternedSymbols(t, k, symbolSize)
	encoder, err := NewSourceBlockEncoder(source, mustEncoderBudget(t, k, symbolSize))
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := NewSourceBlockDecoder(k, symbolSize, exactDecoderLimits(t, k, symbolSize, k+1, 2))
	if err != nil {
		t.Fatal(err)
	}
	// The ten consecutive repair ESIs 319..328 form a fixed rank-deficient
	// equation set for K=10; ESI 329 contributes the missing rank.
	for esi := uint32(319); esi <= 328; esi++ {
		payload, symbolErr := encoder.Symbol(esi, 1)
		if symbolErr != nil {
			t.Fatal(symbolErr)
		}
		if complete, symbolErr := decoder.AddSymbol(esi, payload); symbolErr != nil || complete {
			t.Fatalf("ESI %d complete=%v error=%v", esi, complete, symbolErr)
		}
	}
	if decoder.DecodeAttempts() != 1 || decoder.RetainedSymbols() != k {
		t.Fatalf("after deficient set retained=%d attempts=%d", decoder.RetainedSymbols(), decoder.DecodeAttempts())
	}
	payload, err := encoder.Symbol(329, 1)
	if err != nil {
		t.Fatal(err)
	}
	if complete, err := decoder.AddSymbol(329, payload); err != nil || !complete {
		t.Fatalf("retry complete=%v error=%v", complete, err)
	}
	if decoder.DecodeAttempts() != 2 {
		t.Fatalf("attempts=%d, want 2", decoder.DecodeAttempts())
	}
	result, err := decoder.Result(uint64(k) * uint64(symbolSize))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshotSymbols(t, result), snapshotSymbols(t, source)) {
		t.Fatal("rank retry recovered the wrong source")
	}

	newDeficientDecoder := func(retained, attempts uint32) *SourceBlockDecoder {
		limited, constructorErr := NewSourceBlockDecoder(k, symbolSize, exactDecoderLimits(t, k, symbolSize, retained, attempts))
		if constructorErr != nil {
			t.Fatal(constructorErr)
		}
		for esi := uint32(319); esi <= 328; esi++ {
			value, symbolErr := encoder.Symbol(esi, 1)
			if symbolErr != nil {
				t.Fatal(symbolErr)
			}
			if _, symbolErr = limited.AddSymbol(esi, value); symbolErr != nil {
				t.Fatal(symbolErr)
			}
		}
		return limited
	}
	attemptLimited := newDeficientDecoder(k+1, 1)
	if complete, err := attemptLimited.AddSymbol(329, payload); !errors.Is(err, ErrDecoderAttemptLimit) || complete {
		t.Fatalf("attempt cap complete=%v error=%v", complete, err)
	}
	if attemptLimited.RetainedSymbols() != k {
		t.Fatal("attempt-cap rejection retained the new symbol")
	}
	retainedLimited := newDeficientDecoder(k, 2)
	if complete, err := retainedLimited.AddSymbol(329, payload); !errors.Is(err, ErrDecoderRetainedLimit) || complete {
		t.Fatalf("retained cap complete=%v error=%v", complete, err)
	}
	if retainedLimited.RetainedSymbols() != k {
		t.Fatal("retained-cap rejection mutated retention")
	}
}

func TestSourceBlockDecoderOwnsRepairPayloads(t *testing.T) {
	t.Parallel()

	const k, symbolSize = uint32(10), uint32(1)
	source := patternedSymbols(t, k, symbolSize)
	encoder, err := NewSourceBlockEncoder(source, mustEncoderBudget(t, k, symbolSize))
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := NewSourceBlockDecoder(k, symbolSize, exactDecoderLimits(t, k, symbolSize, k+1, 2))
	if err != nil {
		t.Fatal(err)
	}
	for esi := uint32(319); esi <= 329; esi++ {
		payload, err := encoder.Symbol(esi, uint64(symbolSize))
		if err != nil {
			t.Fatal(err)
		}
		complete, err := decoder.AddSymbol(esi, payload)
		payload[0] ^= 0xff
		if err != nil {
			t.Fatalf("ESI %d: %v", esi, err)
		}
		if complete != (esi == 329) {
			t.Fatalf("ESI %d complete=%v", esi, complete)
		}
	}
	result, err := decoder.Result(uint64(k) * uint64(symbolSize))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshotSymbols(t, result), snapshotSymbols(t, source)) {
		t.Fatal("caller mutation after AddSymbol changed decoded output")
	}
}

func TestSourceBlockDecoderTerminalEquationConflict(t *testing.T) {
	t.Parallel()

	const k, symbolSize = uint32(10), uint32(1)
	source := patternedSymbols(t, k, symbolSize)
	encoder, err := NewSourceBlockEncoder(source, mustEncoderBudget(t, k, symbolSize))
	if err != nil {
		t.Fatal(err)
	}
	var conflicted *SourceBlockDecoder
	var retainedESI uint32
	var retainedPayload []byte
	for forged := uint32(319); forged <= 329 && conflicted == nil; forged++ {
		decoder, err := NewSourceBlockDecoder(k, symbolSize, exactDecoderLimits(t, k, symbolSize, k+1, 2))
		if err != nil {
			t.Fatal(err)
		}
		for esi := uint32(319); esi <= 329; esi++ {
			payload, err := encoder.Symbol(esi, uint64(symbolSize))
			if err != nil {
				t.Fatal(err)
			}
			if esi == forged {
				payload[0] ^= 1
			}
			_, addErr := decoder.AddSymbol(esi, payload)
			if errors.Is(addErr, ErrDecoderConflict) {
				conflicted = decoder
				retainedESI = esi
				retainedPayload = append([]byte(nil), payload...)
				break
			}
			if addErr != nil {
				t.Fatalf("forged ESI %d, add ESI %d: %v", forged, esi, addErr)
			}
		}
	}
	if conflicted == nil {
		t.Fatal("no single-row corruption activated the known equation dependency")
	}
	retainedBefore, attemptsBefore := conflicted.RetainedSymbols(), conflicted.DecodeAttempts()
	if complete, err := conflicted.AddSymbol(2_000, []byte{1}); !errors.Is(err, ErrDecoderConflict) || complete {
		t.Fatalf("post-conflict new symbol complete=%v error=%v", complete, err)
	}
	if conflicted.RetainedSymbols() != retainedBefore || conflicted.DecodeAttempts() != attemptsBefore {
		t.Fatal("post-conflict rejection changed decoder state")
	}
	if complete, err := conflicted.AddSymbol(retainedESI, retainedPayload); err != nil || complete {
		t.Fatalf("terminal identical duplicate complete=%v error=%v", complete, err)
	}
	retainedPayload[0] ^= 1
	if complete, err := conflicted.AddSymbol(retainedESI, retainedPayload); !errors.Is(err, ErrDecoderDuplicateConflict) || complete {
		t.Fatalf("terminal conflicting duplicate complete=%v error=%v", complete, err)
	}
}

func TestSourceBlockDecoderWorstAttemptBoundsActualHybridPlans(t *testing.T) {
	t.Parallel()

	random := rand.New(rand.NewSource(0xdec0de))
	for _, k := range []uint32{10, 13, 49} {
		parameters, err := rfc6330.ParametersForSourceSymbols(k)
		if err != nil {
			t.Fatal(err)
		}
		retained := k + 5
		cap := parameters.L()
		worst, err := SourceBlockDecoderWorstAttemptBackingBytes(k, 7, retained, cap)
		if err != nil {
			t.Fatal(err)
		}
		encodedRows := retained + parameters.KPrime - k
		for iteration := 0; iteration < 100; iteration++ {
			isis := make([]uint32, encodedRows)
			for index := range isis {
				isis[index] = uint32(random.Intn(4_000))
			}
			planBytes, err := InactivationPlanBackingBytes(parameters.KPrime, encodedRows)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := PlanReceivedInactivation(parameters.KPrime, isis, planBytes)
			if err != nil {
				t.Fatal(err)
			}
			executionBytes, err := InactivationExecutionBackingBytes(plan, 7)
			if err != nil {
				t.Fatal(err)
			}
			dBytes := uint64(plan.totalRows) * 7
			actual, err := checkedBackingSum(ErrDecoderBacking, uint64(encodedRows)*4, planBytes, dBytes, executionBytes)
			if err != nil {
				t.Fatal(err)
			}
			if actual > worst {
				t.Fatalf("K=%d iteration=%d actual=%d worst=%d", k, iteration, actual, worst)
			}
		}
	}
}

func mustEncoderBudget(t testing.TB, k, size uint32) uint64 {
	t.Helper()
	budget, err := SourceBlockEncoderWorkspaceBackingBytes(k, size)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}
