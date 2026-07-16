package raptorgo

import (
	"errors"
	"testing"
)

func TestOTIRoundTrip(t *testing.T) {
	oti := OTI{
		TransferLength:  0x0102030405,
		SymbolSize:      1_400,
		SourceBlocks:    255,
		SubBlocks:       2,
		SymbolAlignment: 4,
	}
	encoded, err := oti.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{1, 2, 3, 4, 5, 0, 0x05, 0x78, 255, 0, 2, 4}
	if string(encoded) != string(want) {
		t.Fatalf("OTI encoding = %v, want %v", encoded, want)
	}
	got, err := ParseOTI(encoded, RFCWireLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got != oti {
		t.Fatalf("OTI round trip = %+v, want %+v", got, oti)
	}
}

func TestDeriveOTISection43ExamplesAndMinimalN(t *testing.T) {
	tests := []struct {
		name  string
		input DerivationInput
		want  OTI
	}{
		{
			name: "one block one sub-block",
			input: DerivationInput{
				TransferLength: 1_000, DecoderBlockBytes: 1_000, MaxPayloadSize: 100,
				SymbolAlignment: 4, MinSubSymbolAlignmentUnits: 8,
			},
			want: OTI{TransferLength: 1_000, SymbolSize: 100, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 4},
		},
		{
			name: "sub-blocking",
			input: DerivationInput{
				TransferLength: 1_000, DecoderBlockBytes: 500, MaxPayloadSize: 100,
				SymbolAlignment: 4, MinSubSymbolAlignmentUnits: 1,
			},
			want: OTI{TransferLength: 1_000, SymbolSize: 100, SourceBlocks: 1, SubBlocks: 3, SymbolAlignment: 4},
		},
		{
			name: "source and sub-blocking",
			input: DerivationInput{
				TransferLength: 100_000, DecoderBlockBytes: 500, MaxPayloadSize: 100,
				SymbolAlignment: 4, MinSubSymbolAlignmentUnits: 1,
			},
			want: OTI{TransferLength: 100_000, SymbolSize: 100, SourceBlocks: 8, SubBlocks: 25, SymbolAlignment: 4},
		},
		{
			name: "empty object keeps positive partitions",
			input: DerivationInput{
				TransferLength: 0, DecoderBlockBytes: 1_000, MaxPayloadSize: 100,
				SymbolAlignment: 4, MinSubSymbolAlignmentUnits: 8,
			},
			want: OTI{TransferLength: 0, SymbolSize: 100, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 4},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := DeriveOTI(test.input, RFCWireLimits())
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("DeriveOTI()=%+v, want %+v", got, test.want)
			}
		})
	}
}

func TestDeriveOTIRejectsInvalidInputsAndPolicy(t *testing.T) {
	valid := DerivationInput{
		TransferLength: 1_000, DecoderBlockBytes: 500, MaxPayloadSize: 100,
		SymbolAlignment: 4, MinSubSymbolAlignmentUnits: 1,
	}
	invalid := []DerivationInput{
		{},
		{TransferLength: 1, DecoderBlockBytes: 500, MaxPayloadSize: 99, SymbolAlignment: 4, MinSubSymbolAlignmentUnits: 1},
		{TransferLength: 1, DecoderBlockBytes: 500, MaxPayloadSize: 100, SymbolAlignment: 4, MinSubSymbolAlignmentUnits: 26},
		{TransferLength: 1, DecoderBlockBytes: 9, MaxPayloadSize: 1, SymbolAlignment: 1, MinSubSymbolAlignmentUnits: 1},
		{TransferLength: MaxTransferLength + 1, DecoderBlockBytes: 500, MaxPayloadSize: 100, SymbolAlignment: 4, MinSubSymbolAlignmentUnits: 1},
	}
	for _, input := range invalid {
		if _, err := DeriveOTI(input, RFCWireLimits()); !errors.Is(err, ErrInvalidDerivation) {
			t.Errorf("DeriveOTI(%+v) error=%v", input, err)
		}
	}
	limits := DefaultLimits()
	limits.MaxSubBlocks = 2
	if _, err := DeriveOTI(valid, limits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("derived policy error=%v", err)
	}
}

func TestOTIVerifiedErratumBoundary(t *testing.T) {
	oti := OTI{
		TransferLength:  MaxTransferLength,
		SymbolSize:      65_535,
		SourceBlocks:    255,
		SubBlocks:       1,
		SymbolAlignment: 1,
	}
	if _, err := oti.MarshalBinary(); err != nil {
		t.Fatalf("verified maximum rejected: %v", err)
	}
	oti.TransferLength++
	if _, err := oti.MarshalBinary(); !errors.Is(err, ErrInvalidOTI) {
		t.Fatalf("above-maximum transfer returned %v", err)
	}
}

func TestOTIRejectsInvalidFieldsAndLimits(t *testing.T) {
	valid := OTI{TransferLength: 1_024, SymbolSize: 128, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 4}
	tests := []OTI{
		{TransferLength: 1, SymbolSize: 0, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1},
		{TransferLength: 1, SymbolSize: 1, SourceBlocks: 0, SubBlocks: 1, SymbolAlignment: 1},
		{TransferLength: 1, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 0, SymbolAlignment: 1},
		{TransferLength: 1, SymbolSize: 3, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 2},
	}
	for _, oti := range tests {
		if _, err := oti.MarshalBinary(); !errors.Is(err, ErrInvalidOTI) {
			t.Errorf("MarshalBinary(%+v) returned %v", oti, err)
		}
	}

	limits := DefaultLimits()
	limits.MaxSourceBlockBytes = 512
	if err := valid.Validate(limits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("source-block budget returned %v", err)
	}
	tooManySymbols := OTI{
		TransferLength:  uint64(MaxSourceSymbolsPerBlock + 1),
		SymbolSize:      1,
		SourceBlocks:    1,
		SubBlocks:       1,
		SymbolAlignment: 1,
	}
	if err := tooManySymbols.Validate(RFCWireLimits()); !errors.Is(err, ErrInvalidOTI) {
		t.Fatalf("oversized source block returned %v", err)
	}
}

func TestOTIAcceptsRFCWireEdgePartitions(t *testing.T) {
	t.Parallel()

	for _, oti := range []OTI{
		{TransferLength: 0, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1},
		{TransferLength: 1, SymbolSize: 1, SourceBlocks: 2, SubBlocks: 1, SymbolAlignment: 1},
		{TransferLength: 1, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 2, SymbolAlignment: 1},
	} {
		if err := oti.Validate(RFCWireLimits()); err != nil {
			t.Errorf("Validate(%+v) = %v", oti, err)
		}
	}
}

func TestOTIPolicyLimitsUseLimitError(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 128, SymbolSize: 128, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 4}
	tests := []func(*Limits){
		func(limits *Limits) { limits.MaxTransferLength = 64 },
		func(limits *Limits) { limits.MaxSymbolSize = 64 },
		func(limits *Limits) { limits.MaxSourceBlocks = 1; oti.SourceBlocks = 2 },
		func(limits *Limits) { limits.MaxSubBlocks = 1; oti.SubBlocks = 2 },
		func(limits *Limits) { limits.MaxSymbolAlignment = 2 },
	}
	for index, restrict := range tests {
		limits := RFCWireLimits()
		restrict(&limits)
		if err := oti.Validate(limits); !errors.Is(err, ErrLimitExceeded) {
			t.Errorf("policy case %d returned %v", index, err)
		}
		oti.SourceBlocks = 1
		oti.SubBlocks = 1
	}
}

func TestParseOTIRejectsMalformedEncoding(t *testing.T) {
	if _, err := ParseOTI(make([]byte, OTIEncodedSize-1), RFCWireLimits()); !errors.Is(err, ErrInvalidOTI) {
		t.Fatalf("short OTI returned %v", err)
	}
	oti := OTI{TransferLength: 1, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	encoded, err := oti.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	encoded[5] = 1
	if parsed, parseErr := ParseOTI(encoded, RFCWireLimits()); parseErr != nil || parsed != oti {
		t.Fatalf("reserved octet parse = %+v, %v", parsed, parseErr)
	}
}

func FuzzParseOTI(f *testing.F) {
	valid := OTI{TransferLength: 100, SymbolSize: 10, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	encoded, _ := valid.MarshalBinary()
	f.Add(encoded)
	f.Fuzz(func(t *testing.T, data []byte) {
		oti, err := ParseOTI(data, RFCWireLimits())
		if err != nil {
			return
		}
		roundTrip, err := oti.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		canonicalInput := append([]byte(nil), data...)
		canonicalInput[5] = 0
		if string(roundTrip) != string(canonicalInput) {
			t.Fatalf("noncanonical accepted OTI: %v -> %v", data, roundTrip)
		}
	})
}
