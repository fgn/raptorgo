package codec

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

func TestHybridIntermediateExactBudgetAndEagerBoundaryPlusOne(t *testing.T) {
	const k, symbolSize = MaxEagerSourceSymbols + 1, uint32(1)
	source := patternedSymbols(t, k, symbolSize)
	sourceBefore := snapshotSymbols(t, source)
	required, err := HybridIntermediateWorkspaceBackingBytes(k, symbolSize)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := GenerateIntermediateSymbolsHybrid(source, required-1); !errors.Is(err, ErrIntermediateWorkspace) || result != nil {
		t.Fatalf("budget-1 result=%v error=%v", result, err)
	}
	if !bytes.Equal(snapshotSymbols(t, source), sourceBefore) {
		t.Fatal("budget-1 generation mutated source")
	}
	intermediate, err := GenerateIntermediateSymbolsHybrid(source, required)
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := rfc6330.ParametersForSourceSymbols(k)
	if err != nil {
		t.Fatal(err)
	}
	if intermediate.Count() != parameters.L() || intermediate.Size() != symbolSize {
		t.Fatalf("intermediate shape=%dx%d want=%dx%d", intermediate.Count(), intermediate.Size(), parameters.L(), symbolSize)
	}
	for _, isi := range []uint32{0, k / 2, k - 1} {
		got, err := EncodingSymbol(parameters.KPrime, isi, intermediate, uint64(symbolSize))
		if err != nil {
			t.Fatal(err)
		}
		want, err := source.Symbol(isi)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("source ISI %d=%x want=%x", isi, got, want)
		}
	}
	for _, isi := range []uint32{k, parameters.KPrime - 1} {
		if isi < k {
			continue
		}
		got, err := EncodingSymbol(parameters.KPrime, isi, intermediate, uint64(symbolSize))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, make([]byte, symbolSize)) {
			t.Fatalf("padding ISI %d=%x", isi, got)
		}
	}
	if !bytes.Equal(snapshotSymbols(t, source), sourceBefore) {
		t.Fatal("hybrid generation mutated source")
	}
}

func TestHybridIntermediateRFCMaximumRelease(t *testing.T) {
	if os.Getenv("RAPTORGO_RELEASE_TESTS") != "1" {
		t.Skip("set RAPTORGO_RELEASE_TESTS=1 for the RFC-maximum solver gate")
	}
	const k, symbolSize = rfc6330.MaxSourceSymbolsPerBlock, uint32(1)
	source, err := gf256.NewSymbols(k, symbolSize, uint64(k))
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	required, err := HybridIntermediateWorkspaceBackingBytes(k, symbolSize)
	if err != nil {
		t.Fatal(err)
	}
	if required > 64<<20 {
		t.Fatalf("maximum source solve backing=%d exceeds 64 MiB release budget", required)
	}
	intermediate, err := GenerateIntermediateSymbolsHybrid(source, required)
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := rfc6330.ParametersForSourceSymbols(k)
	if err != nil {
		t.Fatal(err)
	}
	if intermediate.Count() != parameters.L() || intermediate.Size() != symbolSize {
		t.Fatalf("intermediate shape=%dx%d", intermediate.Count(), intermediate.Size())
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Fatalf("maximum source solve took %s", elapsed)
	}
	t.Logf("K=%d K'=%d L=%d backing=%d elapsed=%s", k, parameters.KPrime, parameters.L(), required, time.Since(started))
}

func TestSourceBlockDecoderRFCMaximumLossRepairRelease(t *testing.T) {
	if os.Getenv("RAPTORGO_RELEASE_TESTS") != "1" {
		t.Skip("set RAPTORGO_RELEASE_TESTS=1 for the RFC-maximum loss/repair decoder gate")
	}
	const (
		k               = rfc6330.MaxSourceSymbolsPerBlock
		symbolSize      = uint32(1)
		denseCompletion = uint32(1_024)
	)
	persistent, err := SourceBlockDecoderBackingBytes(k, symbolSize, k)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := SourceBlockDecoderWorstAttemptBackingBytes(k, symbolSize, k, denseCompletion)
	if err != nil {
		t.Fatal(err)
	}
	working, err := checkedBackingSum(ErrDecoderBacking, persistent, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if working > 64<<20 {
		t.Fatalf("maximum decoder bound=%d exceeds 64 MiB release budget", working)
	}
	started := time.Now()
	source := patternedSymbols(t, k, symbolSize)
	encoder, err := NewSourceBlockEncoder(source, mustEncoderBudget(t, k, symbolSize))
	if err != nil {
		t.Fatal(err)
	}
	repair, err := encoder.Symbol(k, uint64(symbolSize))
	if err != nil {
		t.Fatal(err)
	}
	// This exact byte is independently reproduced by the pinned Rust 2.0.1
	// release corpus before this test runs through task test:release.
	if len(repair) != 1 || repair[0] != 0xb1 {
		t.Fatalf("K=%d repair ESI %d=%x, want Go/Rust corpus value b1", k, k, repair)
	}
	decoder, err := NewSourceBlockDecoder(k, symbolSize, SourceBlockDecoderLimits{
		MaxRetainedSymbols: k, MaxDecodeAttempts: 1,
		MaxDenseCompletionSymbols: denseCompletion, MaxWorkingMemory: working,
	})
	if err != nil {
		t.Fatal(err)
	}
	lost := k / 2
	for esi := uint32(0); esi < k; esi++ {
		if esi == lost {
			continue
		}
		payload, symbolErr := source.Symbol(esi)
		if symbolErr != nil {
			t.Fatal(symbolErr)
		}
		complete, err := decoder.AddSymbol(esi, payload)
		if err != nil {
			t.Fatalf("ESI %d: %v", esi, err)
		}
		if complete {
			t.Fatalf("ESI %d completed with source ESI %d still missing", esi, lost)
		}
	}
	complete, err := decoder.AddSymbol(k, repair)
	if err != nil {
		t.Fatalf("repair ESI %d: %v", k, err)
	}
	if !complete {
		t.Fatalf("K=%d did not recover missing nonzero source ESI %d", k, lost)
	}
	result, err := decoder.Result(uint64(k))
	if err != nil {
		t.Fatal(err)
	}
	for esi := uint32(0); esi < k; esi++ {
		got, symbolErr := result.Symbol(esi)
		if symbolErr != nil {
			t.Fatal(symbolErr)
		}
		want := byte(uint64(esi)*131 + 19)
		if len(got) != 1 || got[0] != want {
			t.Fatalf("ESI %d decoded=%x want=%02x", esi, got, want)
		}
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Fatalf("maximum decoder took %s", elapsed)
	}
	t.Logf("K=%d lost-ESI=%d repair-ESI=%d decoder-bound=%d elapsed=%s", k, lost, k, working, time.Since(started))
}

func TestSourceBlockDecoderBoundaryPlusOneLossRepairRelease(t *testing.T) {
	if os.Getenv("RAPTORGO_RELEASE_TESTS") != "1" {
		t.Skip("set RAPTORGO_RELEASE_TESTS=1 for the hybrid-boundary repair gate")
	}
	const k, symbolSize = MaxEagerSourceSymbols + 1, uint32(7)
	source := patternedSymbols(t, k, symbolSize)
	encoder, err := NewSourceBlockEncoder(source, mustEncoderBudget(t, k, symbolSize))
	if err != nil {
		t.Fatal(err)
	}
	const repairCount = uint32(24)
	decoder, err := NewSourceBlockDecoder(
		k, symbolSize, exactDecoderLimits(t, k, symbolSize, k+repairCount, repairCount+1),
	)
	if err != nil {
		t.Fatal(err)
	}
	lost := map[uint32]bool{1: true, 17: true, 113: true, 257: true, 509: true, 769: true, 1_023: true}
	for esi := uint32(0); esi < k; esi++ {
		if lost[esi] {
			continue
		}
		payload, symbolErr := encoder.Symbol(esi, uint64(symbolSize))
		if symbolErr != nil {
			t.Fatal(symbolErr)
		}
		if complete, addErr := decoder.AddSymbol(esi, payload); addErr != nil || complete {
			t.Fatalf("source ESI %d complete=%v error=%v", esi, complete, addErr)
		}
	}
	for esi := k; esi < k+repairCount && !decoder.Complete(); esi++ {
		payload, symbolErr := encoder.Symbol(esi, uint64(symbolSize))
		if symbolErr != nil {
			t.Fatal(symbolErr)
		}
		if _, addErr := decoder.AddSymbol(esi, payload); addErr != nil {
			t.Fatalf("repair ESI %d: %v", esi, addErr)
		}
	}
	if !decoder.Complete() {
		t.Fatalf("K=%d did not recover seven losses with %d repairs", k, repairCount)
	}
	result, err := decoder.Result(uint64(k) * uint64(symbolSize))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshotSymbols(t, result), snapshotSymbols(t, source)) {
		t.Fatal("hybrid-boundary repair recovery changed source bytes")
	}
}
