package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"slices"

	"github.com/fgn/raptorgo/internal/codec"
	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

func main() {
	output := bufio.NewWriter(os.Stdout)
	if len(os.Args) == 2 && os.Args[1] == "release" {
		for _, test := range []struct{ k, symbolSize uint32 }{
			{1_024, 1},
			{1_698, 1},
			{5_225, 1},
			{8_837, 1},
			{50_511, 1},
			{55_844, 1},
			{56_403, 1},
			{1_024, 7},
			{55_844, 7},
			{56_403, 7},
			{1_024, 255},
		} {
			emit(output, test.k, test.symbolSize)
		}
	} else if len(os.Args) == 1 {
		for _, k := range []uint32{1, 10, 11, 12, 13, 47, 48, 49, 50} {
			for _, symbolSize := range []uint32{1, 7, 31, 255} {
				emit(output, k, symbolSize)
			}
		}
	} else {
		panic("usage: rawvectors [release]")
	}
	if err := output.Flush(); err != nil {
		panic(err)
	}
}

func emit(output *bufio.Writer, k, symbolSize uint32) {
	source := sourceSymbols(k, symbolSize)
	workspace, err := codec.SourceBlockEncoderWorkspaceBackingBytes(k, symbolSize)
	if err != nil {
		panic(err)
	}
	encoder, err := codec.NewSourceBlockEncoder(source, workspace)
	if err != nil {
		panic(err)
	}
	for _, esi := range symbolIDs(k) {
		symbol, err := encoder.Symbol(esi, uint64(symbolSize))
		if err != nil {
			panic(err)
		}
		if _, err := fmt.Fprintf(output, "s %d %d %d %s\n", k, symbolSize, esi, hex.EncodeToString(symbol)); err != nil {
			panic(err)
		}
	}
}

func sourceSymbols(k, symbolSize uint32) *gf256.Symbols {
	symbols, err := gf256.NewSymbols(k, symbolSize, uint64(k)*uint64(symbolSize))
	if err != nil {
		panic(err)
	}
	for symbol := uint32(0); symbol < k; symbol++ {
		values, err := symbols.Symbol(symbol)
		if err != nil {
			panic(err)
		}
		for index := range values {
			values[index] = byte(uint64(symbol)*131 + uint64(index)*37 + 19)
		}
	}
	return symbols
}

func symbolIDs(k uint32) []uint32 {
	candidates := []uint32{0, k - 1, k, k + 1, k + 17, rfc6330.MaxEncodingSymbolID}
	result := make([]uint32, 0, len(candidates))
	for _, candidate := range candidates {
		if !slices.Contains(result, candidate) {
			result = append(result, candidate)
		}
	}
	return result
}
