// Command extracttables deterministically extracts RFC 6330 Tables 1 and 2
// and the V0-V3 random-number tables from the repository's pinned RFC text.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	randomTableSize      = 256
	randomTableCount     = 4
	systematicTableCount = 477
	degreeTableCount     = 31
)

var systematicRowPattern = regexp.MustCompile(
	`^\s*\|\s*(\d+)\s*\|\s*(\d+)\s*\|\s*(\d+)\s*\|\s*(\d+)\s*\|\s*(\d+)\s*\|\s*$`,
)

var (
	degreeCellPattern = regexp.MustCompile(`\|\s*(\d+)\s*`)
	numberListPattern = regexp.MustCompile(`^\s+([0-9][0-9, ]*)\s*$`)
)

type systematicParameters struct {
	kPrime uint32
	j      uint32
	s      uint32
	h      uint32
	w      uint32
}

func main() {
	rfcPath := flag.String("rfc", "", "path to the pinned RFC 6330 text")
	outputPath := flag.String("output", "", "generated Go output path")
	flag.Parse()
	if *rfcPath == "" || *outputPath == "" || flag.NArg() != 0 {
		fatalf("usage: extracttables -rfc <rfc6330.txt> -output <tables_gen.go>")
	}

	rfc, err := os.ReadFile(*rfcPath)
	if err != nil {
		fatalf("read RFC: %v", err)
	}
	randomTables, err := extractRandomTables(string(rfc))
	if err != nil {
		fatalf("extract random tables: %v", err)
	}
	degreeThresholds, err := extractDegreeThresholds(string(rfc))
	if err != nil {
		fatalf("extract degree table: %v", err)
	}
	systematicTable, err := extractSystematicTable(string(rfc))
	if err != nil {
		fatalf("extract systematic table: %v", err)
	}

	generated, err := render(randomTables, degreeThresholds, systematicTable)
	if err != nil {
		fatalf("render Go: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
		fatalf("create output directory: %v", err)
	}
	if err := os.WriteFile(*outputPath, generated, 0o644); err != nil {
		fatalf("write output: %v", err)
	}
}

func extractRandomTables(rfc string) ([randomTableCount][randomTableSize]uint32, error) {
	var tables [randomTableCount][randomTableSize]uint32
	for table := 0; table < randomTableCount; table++ {
		start := fmt.Sprintf("5.5.%d.  The Table V%d", table+1, table)
		var end string
		if table+1 < randomTableCount {
			end = fmt.Sprintf("5.5.%d.  The Table V%d", table+2, table+1)
		} else {
			end = "5.6.  Systematic Indices and Other Parameters"
		}
		body, err := extractSection(rfc, start, end)
		if err != nil {
			return tables, err
		}
		values, err := parseNumberList(body)
		if err != nil {
			return tables, fmt.Errorf("V%d: %w", table, err)
		}
		if len(values) != randomTableSize {
			return tables, fmt.Errorf("V%d has %d entries, want %d", table, len(values), randomTableSize)
		}
		copy(tables[table][:], values)
	}
	return tables, nil
}

func extractDegreeThresholds(rfc string) ([]uint32, error) {
	body, err := extractSection(rfc, "5.3.5.2.  Degree Generator", "5.3.5.3.  Encoding Symbol Generator")
	if err != nil {
		return nil, err
	}
	thresholds := make([]uint32, degreeTableCount)
	nextIndex := uint32(0)
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		matches := degreeCellPattern.FindAllStringSubmatch(scanner.Text(), -1)
		if len(matches) != 2 && len(matches) != 4 {
			continue
		}
		for pair := 0; pair < len(matches); pair += 2 {
			index, parseErr := parseUint32(matches[pair][1])
			if parseErr != nil {
				return nil, parseErr
			}
			threshold, parseErr := parseUint32(matches[pair+1][1])
			if parseErr != nil {
				return nil, parseErr
			}
			if index != nextIndex || index >= degreeTableCount {
				return nil, fmt.Errorf("degree index %d, want %d", index, nextIndex)
			}
			thresholds[index] = threshold
			nextIndex++
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if nextIndex != degreeTableCount {
		return nil, fmt.Errorf("degree table has %d entries, want %d", nextIndex, degreeTableCount)
	}
	return thresholds, nil
}

func extractSystematicTable(rfc string) ([]systematicParameters, error) {
	body, err := extractSection(
		rfc,
		"5.6.  Systematic Indices and Other Parameters",
		"5.7.  Operating with Octets, Symbols, and Matrices",
	)
	if err != nil {
		return nil, err
	}
	rows := make([]systematicParameters, 0, systematicTableCount)
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		match := systematicRowPattern.FindStringSubmatch(scanner.Text())
		if match == nil {
			continue
		}
		values := [5]uint32{}
		for index := range values {
			values[index], err = parseUint32(match[index+1])
			if err != nil {
				return nil, err
			}
		}
		rows = append(rows, systematicParameters{
			kPrime: values[0],
			j:      values[1],
			s:      values[2],
			h:      values[3],
			w:      values[4],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(rows) != systematicTableCount {
		return nil, fmt.Errorf("systematic table has %d rows, want %d", len(rows), systematicTableCount)
	}
	for index, row := range rows {
		if index != 0 && row.kPrime <= rows[index-1].kPrime {
			return nil, fmt.Errorf("K' is not strictly increasing at row %d", index)
		}
	}
	if rows[0].kPrime != 10 || rows[len(rows)-1].kPrime != 56_403 {
		return nil, fmt.Errorf("unexpected systematic table bounds %d..%d", rows[0].kPrime, rows[len(rows)-1].kPrime)
	}
	return rows, nil
}

func extractSection(text, startMarker, endMarker string) (string, error) {
	// Section headings also occur in the table of contents. The last exact
	// heading is the normative section body.
	start := strings.LastIndex(text, startMarker)
	if start < 0 {
		return "", fmt.Errorf("start marker %q not found", startMarker)
	}
	start += len(startMarker)
	endRelative := strings.Index(text[start:], endMarker)
	if endRelative < 0 {
		return "", fmt.Errorf("end marker %q not found", endMarker)
	}
	return text[start : start+endRelative], nil
}

func parseNumberList(body string) ([]uint32, error) {
	values := make([]uint32, 0, randomTableSize)
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		match := numberListPattern.FindStringSubmatch(scanner.Text())
		if match == nil {
			continue
		}
		for _, raw := range strings.Split(match[1], ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			value, err := parseUint32(raw)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
	}
	return values, scanner.Err()
}

func parseUint32(raw string) (uint32, error) {
	value, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", raw, err)
	}
	return uint32(value), nil
}

func render(
	randomTables [randomTableCount][randomTableSize]uint32,
	degreeThresholds []uint32,
	systematicTable []systematicParameters,
) ([]byte, error) {
	var output bytes.Buffer
	output.WriteString("// Code generated by internal/rfc6330/cmd/extracttables; DO NOT EDIT.\n")
	output.WriteString("//\n")
	output.WriteString("// Derived from IETF RFC 6330 Sections 5.3.5, 5.5, and 5.6.\n")
	output.WriteString("// See RFC6330-CODE-COMPONENT-LICENSE.txt for the Revised BSD terms.\n\n")
	output.WriteString("package rfc6330\n\n")
	output.WriteString("var randomTables = [4][256]uint32{\n")
	for table, values := range randomTables {
		fmt.Fprintf(&output, "\t{ // V%d\n", table)
		writeUint32Values(&output, values[:])
		output.WriteString("\t},\n")
	}
	output.WriteString("}\n\n")
	output.WriteString("var degreeThresholds = [...]uint32{\n")
	writeUint32Values(&output, degreeThresholds)
	output.WriteString("}\n\n")
	output.WriteString("type systematicParameters struct {\n")
	output.WriteString("\tkPrime uint32\n\tj uint32\n\ts uint32\n\th uint32\n\tw uint32\n")
	output.WriteString("}\n\n")
	output.WriteString("var systematicTable = [...]systematicParameters{\n")
	for _, row := range systematicTable {
		fmt.Fprintf(&output, "\t{kPrime: %d, j: %d, s: %d, h: %d, w: %d},\n", row.kPrime, row.j, row.s, row.h, row.w)
	}
	output.WriteString("}\n")
	formatted, err := format.Source(output.Bytes())
	if err != nil {
		return nil, errors.Join(err, fmt.Errorf("unformatted output:\n%s", output.Bytes()))
	}
	return formatted, nil
}

func writeUint32Values(output *bytes.Buffer, values []uint32) {
	for index, value := range values {
		if index%8 == 0 {
			output.WriteString("\t\t")
		}
		fmt.Fprintf(output, "%d,", value)
		if index%8 == 7 || index == len(values)-1 {
			output.WriteByte('\n')
		} else {
			output.WriteByte(' ')
		}
	}
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "extracttables: "+format+"\n", arguments...)
	os.Exit(1)
}
