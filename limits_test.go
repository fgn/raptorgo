package raptorgo

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/fgn/raptorgo/internal/rfc6330"
)

func TestLimitsValidation(t *testing.T) {
	if err := DefaultLimits().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := RFCWireLimits().Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := DefaultLimits()
	invalid.MaxWorkingMemory = 0
	if err := invalid.Validate(); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("zero working-memory limit returned %v", err)
	} else if !strings.Contains(err.Error(), "MaxWorkingMemory") {
		t.Fatalf("error does not name MaxWorkingMemory: %v", err)
	}
	invalid = DefaultLimits()
	invalid.MaxDecodeAttempts = 0
	if err := invalid.Validate(); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("zero decode-attempt limit returned %v", err)
	} else if !strings.Contains(err.Error(), "MaxDecodeAttempts") {
		t.Fatalf("error does not name MaxDecodeAttempts: %v", err)
	}
	invalid = DefaultLimits()
	invalid.MaxDenseCompletionSymbols = MaxIntermediateSymbols + 1
	if err := invalid.Validate(); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("wide dense-completion limit returned %v", err)
	} else if !strings.Contains(err.Error(), "MaxDenseCompletionSymbols") {
		t.Fatalf("error does not name MaxDenseCompletionSymbols: %v", err)
	}
}

func TestLimitProfilesAreExact(t *testing.T) {
	t.Parallel()

	profiles := []struct {
		name string
		got  Limits
		want Limits
	}{
		{
			name: "default",
			got:  DefaultLimits(),
			want: Limits{
				MaxTransferLength:         942_574_504_275,
				MaxSymbolSize:             65_535,
				MaxSourceBlocks:           255,
				MaxSubBlocks:              256,
				MaxSymbolAlignment:        255,
				MaxSourceSymbolsPerBlock:  56_403,
				MaxSourceBlockBytes:       67_108_864,
				MaxPacketBytes:            4_194_304,
				MaxSymbolsPerPacket:       4_096,
				MaxRetainedSymbols:        65_536,
				MaxDecodeAttempts:         64,
				MaxRepairSymbolsPerBlock:  256,
				MaxDenseCompletionSymbols: 2_048,
				MaxWorkingMemory:          268_435_456,
			},
		},
		{
			name: "RFC wire",
			got:  RFCWireLimits(),
			want: Limits{
				MaxTransferLength:         942_574_504_275,
				MaxSymbolSize:             65_535,
				MaxSourceBlocks:           255,
				MaxSubBlocks:              65_535,
				MaxSymbolAlignment:        255,
				MaxSourceSymbolsPerBlock:  56_403,
				MaxSourceBlockBytes:       3_696_370_605,
				MaxPacketBytes:            math.MaxUint64,
				MaxSymbolsPerPacket:       16_777_216,
				MaxRetainedSymbols:        16_777_216,
				MaxDecodeAttempts:         math.MaxUint32,
				MaxRepairSymbolsPerBlock:  16_777_216,
				MaxDenseCompletionSymbols: 57_326,
				MaxWorkingMemory:          math.MaxUint64,
			},
		},
	}
	for _, profile := range profiles {
		t.Run(profile.name, func(t *testing.T) {
			if profile.got != profile.want {
				t.Fatalf("profile = %+v, want %+v", profile.got, profile.want)
			}
		})
	}
}

func TestLimitsValidationRejectsEveryUnsafeFieldIndependently(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		field  string
		mutate func(*Limits)
	}{
		{name: "zero transfer length", field: "MaxTransferLength", mutate: func(value *Limits) { value.MaxTransferLength = 0 }},
		{name: "wide transfer length", field: "MaxTransferLength", mutate: func(value *Limits) { value.MaxTransferLength = MaxTransferLength + 1 }},
		{name: "zero symbol size", field: "MaxSymbolSize", mutate: func(value *Limits) { value.MaxSymbolSize = 0 }},
		{name: "zero source blocks", field: "MaxSourceBlocks", mutate: func(value *Limits) { value.MaxSourceBlocks = 0 }},
		{name: "zero sub-blocks", field: "MaxSubBlocks", mutate: func(value *Limits) { value.MaxSubBlocks = 0 }},
		{name: "zero symbol alignment", field: "MaxSymbolAlignment", mutate: func(value *Limits) { value.MaxSymbolAlignment = 0 }},
		{name: "zero source symbols", field: "MaxSourceSymbolsPerBlock", mutate: func(value *Limits) { value.MaxSourceSymbolsPerBlock = 0 }},
		{name: "wide source symbols", field: "MaxSourceSymbolsPerBlock", mutate: func(value *Limits) { value.MaxSourceSymbolsPerBlock = MaxSourceSymbolsPerBlock + 1 }},
		{name: "zero source-block bytes", field: "MaxSourceBlockBytes", mutate: func(value *Limits) { value.MaxSourceBlockBytes = 0 }},
		{name: "short packet", field: "MaxPacketBytes", mutate: func(value *Limits) { value.MaxPacketBytes = PayloadIDEncodedSize }},
		{name: "zero symbols per packet", field: "MaxSymbolsPerPacket", mutate: func(value *Limits) { value.MaxSymbolsPerPacket = 0 }},
		{name: "zero retained symbols", field: "MaxRetainedSymbols", mutate: func(value *Limits) { value.MaxRetainedSymbols = 0 }},
		{name: "zero decode attempts", field: "MaxDecodeAttempts", mutate: func(value *Limits) { value.MaxDecodeAttempts = 0 }},
		{name: "zero repair symbols", field: "MaxRepairSymbolsPerBlock", mutate: func(value *Limits) { value.MaxRepairSymbolsPerBlock = 0 }},
		{name: "zero dense completion", field: "MaxDenseCompletionSymbols", mutate: func(value *Limits) { value.MaxDenseCompletionSymbols = 0 }},
		{name: "wide dense completion", field: "MaxDenseCompletionSymbols", mutate: func(value *Limits) { value.MaxDenseCompletionSymbols = MaxIntermediateSymbols + 1 }},
		{name: "zero working memory", field: "MaxWorkingMemory", mutate: func(value *Limits) { value.MaxWorkingMemory = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			test.mutate(&limits)
			err := limits.Validate()
			if !errors.Is(err, ErrInvalidLimits) || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("Validate() error = %v, want ErrInvalidLimits naming %s", err, test.field)
			}
		})
	}
}

func TestLimitsValidationAcceptsMinimumPacketBoundary(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxPacketBytes = PayloadIDEncodedSize + 1
	if err := limits.Validate(); err != nil {
		t.Fatalf("Validate() error = %v at minimum packet boundary", err)
	}
}

func TestExportedRFCConstantsMatchInternal(t *testing.T) {
	if uint32(MaxSourceSymbolsPerBlock) != rfc6330.MaxSourceSymbolsPerBlock {
		t.Fatalf(
			"MaxSourceSymbolsPerBlock %d differs from internal %d",
			MaxSourceSymbolsPerBlock, rfc6330.MaxSourceSymbolsPerBlock,
		)
	}
	if uint32(MaxEncodingSymbolID) != rfc6330.MaxEncodingSymbolID {
		t.Fatalf(
			"MaxEncodingSymbolID %d differs from internal %d",
			MaxEncodingSymbolID, rfc6330.MaxEncodingSymbolID,
		)
	}
	parameters, err := rfc6330.ParametersForSourceSymbols(rfc6330.MaxSourceSymbolsPerBlock)
	if err != nil {
		t.Fatal(err)
	}
	if MaxIntermediateSymbols != parameters.L() {
		t.Fatalf(
			"MaxIntermediateSymbols %d differs from L at K'_max %d",
			MaxIntermediateSymbols, parameters.L(),
		)
	}
}
