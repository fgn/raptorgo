package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/fgn/raptorgo/internal/matrix"
)

const (
	rows        = uint32(4)
	columns     = uint32(5)
	maxNonzeros = uint64(6)
)

type record struct {
	op            string
	result        string
	left          int64
	right         int64
	budget        uint64
	status        string
	rowMap        []uint32
	rowInverse    []uint32
	columnMap     []uint32
	columnInverse []uint32
	reserved      uint64
	cursor        uint32
}

func main() {
	scenario := flag.String("scenario", "valid", "trace scenario: valid, missing, forged-swap, or forged-budget")
	output := flag.String("output", "", "output TLA+ module")
	flag.Parse()
	if *output == "" || (*scenario != "valid" && *scenario != "missing" && *scenario != "forged-swap" && *scenario != "forged-budget") {
		flag.Usage()
		os.Exit(2)
	}
	if err := os.WriteFile(*output, generate(*scenario), 0o644); err != nil {
		panic(err)
	}
}

func generate(scenario string) []byte {
	required, err := matrix.SparseScheduleBackingBytes(rows, columns, maxNonzeros)
	if err != nil {
		panic(err)
	}
	records := []record{{op: "init", result: "ok", left: -1, right: -1, status: "unallocated"}}
	if schedule, err := matrix.NewSparseSchedule(rows, columns, maxNonzeros, required-1); !errors.Is(err, matrix.ErrBackingLimitExceeded) || schedule != nil {
		panic(fmt.Sprintf("one-byte-short schedule = %v, %v", schedule, err))
	}
	records = append(records, record{op: "create_limit", result: "limit", left: -1, right: -1, budget: required - 1, status: "unallocated"})
	schedule, err := matrix.NewSparseSchedule(rows, columns, maxNonzeros, required)
	if err != nil {
		panic(err)
	}
	records = append(records, observe("create", "ok", -1, -1, required, 0, schedule))
	cursor := uint32(0)
	for _, cell := range [][2]uint32{{0, 0}, {0, 2}, {1, 1}, {1, 3}, {2, 0}, {2, 2}} {
		if err := schedule.Set(cell[0], cell[1], true); err != nil {
			panic(err)
		}
		cursor++
		records = append(records, observe("set", "ok", int64(cell[0]), int64(cell[1]), 0, cursor, schedule))
	}
	if err := schedule.Set(3, 4, true); !errors.Is(err, matrix.ErrNonzeroLimitExceeded) {
		panic(fmt.Sprintf("capacity rejection = %v", err))
	}
	records = append(records, observe("reject_capacity", "capacity", 3, 4, 0, cursor, schedule))
	if err := schedule.SwapRows(0, 3); err != nil {
		panic(err)
	}
	cursor++
	records = append(records, observe("swap_row", "ok", 0, 3, 0, cursor, schedule))
	if err := schedule.SwapColumns(1, 4); err != nil {
		panic(err)
	}
	cursor++
	records = append(records, observe("swap_column", "ok", 1, 4, 0, cursor, schedule))
	if err := schedule.AddRow(2, 3); err != nil {
		panic(err)
	}
	cursor++
	records = append(records, observe("add_row", "ok", 2, 3, 0, cursor, schedule))
	if err := schedule.SwapRows(1, 1); err != nil {
		panic(err)
	}
	cursor++
	records = append(records, observe("swap_row_same", "ok", 1, 1, 0, cursor, schedule))
	if err := schedule.SwapColumns(2, 2); err != nil {
		panic(err)
	}
	cursor++
	records = append(records, observe("swap_column_same", "ok", 2, 2, 0, cursor, schedule))
	if err := schedule.SwapRows(0, rows); !errors.Is(err, matrix.ErrIndex) {
		panic(fmt.Sprintf("index rejection = %v", err))
	}
	records = append(records, observe("reject_index", "index", 0, int64(rows), 0, cursor, schedule))
	records = append(records, observe("observe", "ok", -1, -1, 0, cursor, schedule))

	module := "SparseScheduleTraceData"
	switch scenario {
	case "missing":
		module = "SparseScheduleTraceMissingData"
		for index, record := range records {
			if record.op == "add_row" {
				records = append(records[:index], records[index+1:]...)
				break
			}
		}
	case "forged-swap":
		module = "SparseScheduleTraceForgedSwapData"
		for index := range records {
			if records[index].op == "swap_row" {
				records[index].rowMap = append([]uint32(nil), records[index-1].rowMap...)
				records[index].rowInverse = append([]uint32(nil), records[index-1].rowInverse...)
				break
			}
		}
	case "forged-budget":
		module = "SparseScheduleTraceForgedBudgetData"
		records[1].op = "create"
		records[1].result = "ok"
		records[1].status = "ready"
		records[1].reserved = required
		records[1].rowMap = identity(rows)
		records[1].rowInverse = identity(rows)
		records[1].columnMap = identity(columns)
		records[1].columnInverse = identity(columns)
	}
	return render(module, records)
}

func observe(op, result string, left, right int64, budget uint64, cursor uint32, schedule *matrix.SparseSchedule) record {
	rowMap := make([]uint32, schedule.Rows())
	rowInverse := make([]uint32, schedule.Rows())
	for index := uint32(0); index < schedule.Rows(); index++ {
		original, err := schedule.OriginalRow(index)
		if err != nil {
			panic(err)
		}
		current, err := schedule.CurrentRow(index)
		if err != nil {
			panic(err)
		}
		rowMap[index] = original + 1
		rowInverse[index] = current + 1
	}
	columnMap := make([]uint32, schedule.Columns())
	columnInverse := make([]uint32, schedule.Columns())
	for index := uint32(0); index < schedule.Columns(); index++ {
		original, err := schedule.OriginalColumn(index)
		if err != nil {
			panic(err)
		}
		current, err := schedule.CurrentColumn(index)
		if err != nil {
			panic(err)
		}
		columnMap[index] = original + 1
		columnInverse[index] = current + 1
	}
	return record{
		op: op, result: result, left: left, right: right, budget: budget,
		status: "ready", rowMap: rowMap, rowInverse: rowInverse,
		columnMap: columnMap, columnInverse: columnInverse,
		reserved: schedule.BackingBytes(), cursor: cursor,
	}
}

func render(module string, records []record) []byte {
	var output bytes.Buffer
	fmt.Fprintf(&output, "---------------- MODULE %s ----------------\n", module)
	output.WriteString("EXTENDS Integers, Sequences\n\n")
	fmt.Fprintf(&output, "ImplementationRows == %d\n", rows)
	fmt.Fprintf(&output, "ImplementationColumns == %d\n", columns)
	fmt.Fprintf(&output, "ImplementationMaxNonzeros == %d\n\n", maxNonzeros)
	output.WriteString("ImplementationTrace == <<\n")
	for index, record := range records {
		comma := ","
		if index == len(records)-1 {
			comma = ""
		}
		fmt.Fprintf(&output, "    [op |-> %q, result |-> %q, left |-> %d, right |-> %d, budget |-> %d,\n", record.op, record.result, record.left, record.right, record.budget)
		fmt.Fprintf(&output, "     status |-> %q, rowMap |-> %s, rowInverse |-> %s,\n", record.status, sequence(record.rowMap), sequence(record.rowInverse))
		fmt.Fprintf(&output, "     columnMap |-> %s, columnInverse |-> %s, reserved |-> %d, cursor |-> %d]%s\n", sequence(record.columnMap), sequence(record.columnInverse), record.reserved, record.cursor, comma)
	}
	output.WriteString(">>\n\n=====================================================================\n")
	return output.Bytes()
}

func sequence(values []uint32) string {
	var output bytes.Buffer
	output.WriteString("<<")
	for index, value := range values {
		if index != 0 {
			output.WriteString(", ")
		}
		fmt.Fprint(&output, value)
	}
	output.WriteString(">>")
	return output.String()
}

func identity(size uint32) []uint32 {
	values := make([]uint32, size)
	for index := range values {
		values[index] = uint32(index) + 1
	}
	return values
}
