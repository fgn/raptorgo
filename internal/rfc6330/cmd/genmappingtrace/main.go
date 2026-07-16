package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/fgn/raptorgo/internal/rfc6330"
)

type record struct {
	k      uint32
	kPrime uint32
	esi    uint32
	isi    uint32
}

func main() {
	scenario := flag.String("scenario", "valid", "trace scenario: valid, invalid, or missing")
	output := flag.String("output", "", "output TLA+ module")
	flag.Parse()
	if *output == "" || (*scenario != "valid" && *scenario != "invalid" && *scenario != "missing") {
		flag.Usage()
		os.Exit(2)
	}
	if err := os.WriteFile(*output, generate(*scenario), 0o644); err != nil {
		panic(err)
	}
}

func generate(scenario string) []byte {
	kPrimes, records := mappingBoundaries()
	module := "MappingTraceData"
	if scenario == "invalid" {
		module = "MappingTraceInvalidData"
		records[len(records)-1].isi++
	} else if scenario == "missing" {
		module = "MappingTraceMissingData"
		records = records[:len(records)-1]
	}
	var output bytes.Buffer
	fmt.Fprintf(&output, "-------------------- MODULE %s --------------------\n", module)
	output.WriteString("EXTENDS Integers, Sequences\n\n")
	output.WriteString("ImplementationKPrimes == <<")
	for index, kPrime := range kPrimes {
		if index != 0 {
			output.WriteString(", ")
		}
		fmt.Fprint(&output, kPrime)
	}
	output.WriteString(">>\n")
	fmt.Fprintf(&output, "ImplementationMaxESI == %d\n\n", rfc6330.MaxEncodingSymbolID)
	output.WriteString("ImplementationTrace == <<\n")
	for index, record := range records {
		comma := ","
		if index == len(records)-1 {
			comma = ""
		}
		fmt.Fprintf(&output, "    [k |-> %d, kPrime |-> %d, esi |-> %d, isi |-> %d]%s\n",
			record.k, record.kPrime, record.esi, record.isi, comma)
	}
	output.WriteString(">>\n\n=====================================================================\n")
	return output.Bytes()
}

func mappingBoundaries() ([]uint32, []record) {
	var kPrimes []uint32
	var records []record
	start := uint32(1)
	lastKPrime := uint32(0)
	for k := uint32(1); k <= rfc6330.MaxSourceSymbolsPerBlock; k++ {
		parameters, err := rfc6330.ParametersForSourceSymbols(k)
		if err != nil {
			panic(err)
		}
		if parameters.KPrime == lastKPrime {
			continue
		}
		if lastKPrime != 0 {
			records = appendBoundaryRecords(records, start, lastKPrime)
			start = k
		}
		lastKPrime = parameters.KPrime
		kPrimes = append(kPrimes, parameters.KPrime)
	}
	records = appendBoundaryRecords(records, start, lastKPrime)
	return kPrimes, records
}

func appendBoundaryRecords(records []record, start, end uint32) []record {
	ks := []uint32{start}
	if end != start {
		ks = append(ks, end)
	}
	for _, k := range ks {
		parameters, err := rfc6330.ParametersForSourceSymbols(k)
		if err != nil {
			panic(err)
		}
		for _, esi := range []uint32{0, k - 1, k, rfc6330.MaxEncodingSymbolID} {
			isi, err := rfc6330.InternalSymbolID(k, esi)
			if err != nil {
				panic(err)
			}
			records = append(records, record{k: k, kPrime: parameters.KPrime, esi: esi, isi: isi})
		}
	}
	return records
}
