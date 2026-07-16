package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/fgn/raptorgo/internal/codec"
	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

const symbolSize = uint32(1)

type record struct {
	op        string
	result    string
	status    string
	budget    uint64
	reserved  uint64
	phase     uint32
	step      uint32
	i         uint32
	u         uint32
	r         uint32
	l         uint32
	m         uint32
	discarded uint32
}

func main() {
	scenario := flag.String("scenario", "valid", "trace scenario: valid, missing, forged-discard, forged-budget, forged-init-budget, forged-initial-u, forged-phase-label, or forged-u")
	output := flag.String("output", "", "output TLA+ module")
	flag.Parse()
	if *output == "" || (*scenario != "valid" && *scenario != "missing" && *scenario != "forged-discard" && *scenario != "forged-budget" && *scenario != "forged-init-budget" && *scenario != "forged-initial-u" && *scenario != "forged-phase-label" && *scenario != "forged-u") {
		flag.Usage()
		os.Exit(2)
	}
	if err := os.WriteFile(*output, generate(*scenario), 0o644); err != nil {
		panic(err)
	}
}

func generate(scenario string) []byte {
	parameters, err := rfc6330.ParametersForSourceSymbols(1)
	if err != nil {
		panic(err)
	}
	isis := make([]uint32, parameters.KPrime+2)
	for index := range isis {
		isis[index] = uint32(index)
	}
	matrixBytes, capacity, err := codec.SparseConstraintMatrixBackingBytes(parameters.KPrime, isis)
	if err != nil {
		panic(err)
	}
	coefficients, err := codec.BuildSparseConstraintMatrix(parameters.KPrime, isis, matrixBytes)
	if err != nil {
		panic(err)
	}
	dBytes := uint64(coefficients.Rows()) * uint64(symbolSize)
	d, err := gf256.NewSymbols(coefficients.Rows(), symbolSize, dBytes)
	if err != nil {
		panic(err)
	}
	workspace, err := codec.InactivationWorkspaceBackingBytes(coefficients.Rows(), parameters.L(), capacity, symbolSize)
	if err != nil {
		panic(err)
	}
	records := []record{
		{op: "init", result: "ok", status: "unallocated", l: parameters.L(), m: coefficients.Rows()},
	}
	if result, err := codec.SolveInactivation(coefficients, d, parameters.P(), parameters.S, parameters.H, workspace-1); !errors.Is(err, codec.ErrInactivationWorkspace) || result != nil {
		panic(fmt.Sprintf("one-byte-short solve = %v, %v", result, err))
	}
	records = append(records, record{
		op: "solve_limit", result: "limit", status: "unallocated", budget: workspace - 1,
		l: parameters.L(), m: coefficients.Rows(),
	})
	var last codec.InactivationState
	result, err := codec.SolveInactivationObserved(
		coefficients, d, parameters.P(), parameters.S, parameters.H, workspace,
		func(state codec.InactivationState) error {
			last = state
			records = append(records, record{
				op: phaseName(state.Phase), result: "ok", status: "running", budget: workspace, reserved: workspace,
				phase: uint32(state.Phase), step: state.Step, i: state.I, u: state.U, r: state.R,
				l: state.L, m: state.M, discarded: state.Discarded,
			})
			return nil
		},
	)
	if err != nil || result == nil {
		panic(fmt.Sprintf("exact solve = %v, %v", result, err))
	}
	records = append(records, record{
		op: "complete", result: "ok", status: "complete", budget: workspace, reserved: workspace,
		phase: uint32(last.Phase), step: last.Step, i: last.I, u: last.U, r: last.R,
		l: last.L, m: last.M, discarded: last.Discarded,
	})

	module := "InactivationTraceData"
	switch scenario {
	case "missing":
		module = "InactivationTraceMissingData"
		for index, value := range records {
			if value.op == "phase3" {
				records = append(records[:index], records[index+1:]...)
				break
			}
		}
	case "forged-discard":
		module = "InactivationTraceForgedDiscardData"
		for index := range records {
			if records[index].op == "phase2" {
				records[index].discarded--
				break
			}
		}
	case "forged-budget":
		module = "InactivationTraceForgedBudgetData"
		records[1].result = "ok"
		records[1].status = "running"
		records[1].reserved = workspace
	case "forged-init-budget":
		module = "InactivationTraceForgedInitBudgetData"
		records[0].budget = workspace
	case "forged-initial-u":
		module = "InactivationTraceForgedInitialUData"
		for index := range records {
			if records[index].op == "phase1" && records[index].step == 0 {
				records[index].u++
				break
			}
		}
	case "forged-phase-label":
		module = "InactivationTraceForgedPhaseLabelData"
		phaseThree, phaseFive := -1, -1
		for index := range records {
			switch records[index].op {
			case "phase3":
				phaseThree = index
			case "phase5":
				phaseFive = index
			}
		}
		if phaseThree < 0 || phaseFive < 0 {
			panic("missing phase label")
		}
		records[phaseThree].op, records[phaseFive].op = records[phaseFive].op, records[phaseThree].op
	case "forged-u":
		module = "InactivationTraceForgedUData"
		for index := range records {
			if records[index].op == "phase1" && records[index].step == 1 {
				records[index].u++
				break
			}
		}
	}
	return render(module, records, coefficients.Rows(), parameters.L(), parameters.P(), capacity, workspace)
}

func phaseName(phase codec.InactivationPhase) string {
	return fmt.Sprintf("phase%d", phase)
}

func render(module string, records []record, rows, columns, initialU uint32, capacity, workspace uint64) []byte {
	var output bytes.Buffer
	fmt.Fprintf(&output, "---------------- MODULE %s ----------------\n", module)
	output.WriteString("EXTENDS Integers, Sequences\n\n")
	fmt.Fprintf(&output, "ImplementationRows == %d\n", rows)
	fmt.Fprintf(&output, "ImplementationColumns == %d\n", columns)
	fmt.Fprintf(&output, "ImplementationInitialU == %d\n", initialU)
	fmt.Fprintf(&output, "ImplementationMaxNonzeros == %d\n", capacity)
	fmt.Fprintf(&output, "ImplementationSymbolSize == %d\n", symbolSize)
	fmt.Fprintf(&output, "ImplementationWorkspace == %d\n\n", workspace)
	output.WriteString("ImplementationTrace == <<\n")
	for index, value := range records {
		comma := ","
		if index == len(records)-1 {
			comma = ""
		}
		fmt.Fprintf(&output, "    [op |-> %q, result |-> %q, status |-> %q, budget |-> %d, reserved |-> %d,\n", value.op, value.result, value.status, value.budget, value.reserved)
		fmt.Fprintf(&output, "     phase |-> %d, step |-> %d, i |-> %d, u |-> %d, r |-> %d, l |-> %d, m |-> %d, discarded |-> %d]%s\n", value.phase, value.step, value.i, value.u, value.r, value.l, value.m, value.discarded, comma)
	}
	output.WriteString(">>\n\n=====================================================================\n")
	return output.Bytes()
}
