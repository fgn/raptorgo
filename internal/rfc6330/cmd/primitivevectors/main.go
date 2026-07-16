package main

import (
	"bufio"
	"fmt"
	"math"
	"os"

	"github.com/fgn/raptorgo/internal/rfc6330"
)

func main() {
	output := bufio.NewWriterSize(os.Stdout, 1<<20)
	writeParametersAndTuples(output)
	writeRandomVectors(output)
	writeDegreeVectors(output)
	if err := output.Flush(); err != nil {
		panic(err)
	}
}

func writeParametersAndTuples(output *bufio.Writer) {
	lastKPrime := uint32(0)
	for k := uint32(1); k <= rfc6330.MaxSourceSymbolsPerBlock; k++ {
		parameters := mustParameters(k)
		firstSource := mustInternalSymbolID(k, 0)
		lastSource := mustInternalSymbolID(k, k-1)
		firstRepair := mustInternalSymbolID(k, k)
		lastRepair := mustInternalSymbolID(k, rfc6330.MaxEncodingSymbolID)
		mustWrite(output, "k %d %d %d %d %d %d\n", k, parameters.KPrime, firstSource, lastSource, firstRepair, lastRepair)
		if parameters.KPrime == lastKPrime {
			continue
		}
		lastKPrime = parameters.KPrime
		mustWrite(output, "p %d %d %d %d %d %d %d %d %d %d\n",
			parameters.KPrime, parameters.J, parameters.S, parameters.H, parameters.W,
			parameters.L(), parameters.P(), parameters.P1(), parameters.B(), parameters.U())
		for _, x := range tupleIDs(parameters.KPrime) {
			tuple, err := rfc6330.Tuple(parameters.KPrime, x)
			if err != nil {
				panic(err)
			}
			mustWrite(output, "t %d %d %d %d %d %d %d %d\n",
				parameters.KPrime, x, tuple.D, tuple.A, tuple.B, tuple.D1, tuple.A1, tuple.B1)
			mustWrite(output, "e %d %d", parameters.KPrime, x)
			if err := rfc6330.VisitEncodingIndices(parameters.KPrime, x, func(index uint32) error {
				mustWrite(output, " %d", index)
				return nil
			}); err != nil {
				panic(err)
			}
			mustWrite(output, "\n")
		}
	}
}

func tupleIDs(kPrime uint32) []uint32 {
	return []uint32{
		0,
		kPrime - 1,
		kPrime,
		rfc6330.MaxEncodingSymbolID,
		math.MaxUint32,
		kPrime * 2_654_435_761,
		kPrime*2_246_822_519 + 3_266_489_917,
	}
}

func writeRandomVectors(output *bufio.Writer) {
	explicit := [][3]uint32{
		{0, 0, math.MaxUint32},
		{1, 1, 1 << 20},
		{0x01234567, 89, 56951},
		{math.MaxUint32, math.MaxUint8, 379},
		{0xdeadbeef, 17, 1},
	}
	for _, vector := range explicit {
		writeRandom(output, vector[0], uint8(vector[1]), vector[2])
	}
	moduli := [...]uint32{1, 2, 3, 11, 251, 256, 379, 56_951, 1 << 20, math.MaxUint32}
	for index := uint32(0); index < 1<<16; index++ {
		y := index*0x9e3779b9 + 0x7f4a7c15
		i := uint8(index*73 + 19)
		writeRandom(output, y, i, moduli[index%uint32(len(moduli))])
	}
}

func writeRandom(output *bufio.Writer, y uint32, i uint8, modulus uint32) {
	value, err := rfc6330.Rand(y, i, modulus)
	if err != nil {
		panic(err)
	}
	mustWrite(output, "r %d %d %d %d\n", y, i, modulus, value)
}

func writeDegreeVectors(output *bufio.Writer) {
	for v := uint32(0); v < 1<<20; v++ {
		mustWrite(output, "d %d %d %d %d %d\n", v,
			mustDegree(v, 17), mustDegree(v, 19), mustDegree(v, 61), mustDegree(v, 56_951))
	}
}

func mustParameters(k uint32) rfc6330.Parameters {
	parameters, err := rfc6330.ParametersForSourceSymbols(k)
	if err != nil {
		panic(err)
	}
	return parameters
}

func mustInternalSymbolID(k, esi uint32) uint32 {
	x, err := rfc6330.InternalSymbolID(k, esi)
	if err != nil {
		panic(err)
	}
	return x
}

func mustDegree(v, w uint32) uint32 {
	degree, err := rfc6330.Degree(v, w)
	if err != nil {
		panic(err)
	}
	return degree
}

func mustWrite(output *bufio.Writer, format string, arguments ...any) {
	if _, err := fmt.Fprintf(output, format, arguments...); err != nil {
		panic(err)
	}
}
