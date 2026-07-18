// Package dotparse parses the deliberately restricted DOT emitted by the
// pinned TLC ObjectDecoderModel run. It is not a general Graphviz parser.
package dotparse

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Kind identifies one supported TLA+ scalar value kind.
type Kind uint8

const (
	Integer Kind = iota + 1
	String
	IntegerSet
	Boolean
)

// Value is one restricted TLA+ value.
type Value struct {
	Kind   Kind
	Int    int64
	String string
	Set    []int64
	Bool   bool
}

// State maps the flat ObjectDecoderModel variable names to values.
type State map[string]Value

// Edge is one action-labelled TLC graph edge. Action retains parameters, for
// example AddWait(0,1); parallel source/destination edges are not collapsed.
type Edge struct {
	Src, Dst string
	Action   string
}

// Graph is a deterministic representation of one TLC state dump.
type Graph struct {
	States   map[string]State
	StateIDs []string
	Edges    []Edge
	Inits    []string
}

// Parse reads a TLC -dump dot,actionlabels graph.
func Parse(reader io.Reader) (Graph, error) {
	graph := Graph{States: make(map[string]State)}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	lineNumber := 0
	depth := 0
	sawHeader, sawCluster, closed := false, false, false
	layoutIDs := make([]string, 0)
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !sawHeader {
			if line != "strict digraph DiskGraph {" {
				return Graph{}, fmt.Errorf("dotparse: line %d: missing strict DiskGraph header", lineNumber)
			}
			sawHeader, depth = true, 1
			continue
		}
		if closed {
			return Graph{}, fmt.Errorf("dotparse: line %d: content after graph close", lineNumber)
		}
		if line == "subgraph cluster_graph {" {
			if depth != 1 || sawCluster {
				return Graph{}, fmt.Errorf("dotparse: line %d: misplaced cluster", lineNumber)
			}
			sawCluster, depth = true, 2
			continue
		}
		if line == "}" {
			if depth < 1 {
				return Graph{}, fmt.Errorf("dotparse: line %d: unmatched close brace", lineNumber)
			}
			depth--
			closed = depth == 0
			continue
		}
		if depth == 1 {
			switch line {
			case "node [shape=box,style=rounded]", "nodesep=0.35;":
				continue
			}
		}
		if depth == 2 {
			if line == `color="white";` {
				continue
			}
			if strings.HasPrefix(line, "{rank") {
				ids, err := parseRankStatement(line)
				if err != nil {
					return Graph{}, fmt.Errorf("dotparse: line %d: %w", lineNumber, err)
				}
				layoutIDs = append(layoutIDs, ids...)
				continue
			}
		}
		if depth != 2 {
			return Graph{}, fmt.Errorf("dotparse: line %d: statement outside cluster", lineNumber)
		}
		if strings.Contains(line, " -> ") {
			edge, err := parseEdge(line)
			if err != nil {
				return Graph{}, fmt.Errorf("dotparse: line %d: %w", lineNumber, err)
			}
			graph.Edges = append(graph.Edges, edge)
			continue
		}
		id, attributes, ok, err := parseNodeStatement(line)
		if err != nil {
			return Graph{}, fmt.Errorf("dotparse: line %d: %w", lineNumber, err)
		}
		if !ok {
			return Graph{}, fmt.Errorf("dotparse: line %d: unsupported statement %q", lineNumber, line)
		}
		parsedAttributes, err := parseAttributes(attributes)
		if err != nil {
			return Graph{}, fmt.Errorf("dotparse: line %d: %w", lineNumber, err)
		}
		label, ok := parsedAttributes["label"]
		if !ok {
			return Graph{}, fmt.Errorf("dotparse: line %d: missing label attribute", lineNumber)
		}
		if err := validateNodeAttributes(parsedAttributes); err != nil {
			return Graph{}, fmt.Errorf("dotparse: line %d: %w", lineNumber, err)
		}
		state, err := parseState(label)
		if err != nil {
			return Graph{}, fmt.Errorf("dotparse: line %d state %s: %w", lineNumber, id, err)
		}
		if _, duplicate := graph.States[id]; duplicate {
			return Graph{}, fmt.Errorf("dotparse: line %d: duplicate node %s", lineNumber, id)
		}
		graph.States[id] = state
		if parsedAttributes["style"] == "filled" {
			graph.Inits = append(graph.Inits, id)
		}
	}
	if err := scanner.Err(); err != nil {
		return Graph{}, fmt.Errorf("dotparse: scan: %w", err)
	}
	if !sawHeader || !sawCluster || !closed || depth != 0 {
		return Graph{}, errors.New("dotparse: incomplete graph envelope")
	}
	if len(graph.States) == 0 {
		return Graph{}, errors.New("dotparse: graph has no states")
	}
	for _, edge := range graph.Edges {
		if _, ok := graph.States[edge.Src]; !ok {
			return Graph{}, fmt.Errorf("dotparse: edge source %s has no node", edge.Src)
		}
		if _, ok := graph.States[edge.Dst]; !ok {
			return Graph{}, fmt.Errorf("dotparse: edge destination %s has no node", edge.Dst)
		}
	}
	for _, id := range layoutIDs {
		if _, ok := graph.States[id]; !ok {
			return Graph{}, fmt.Errorf("dotparse: rank references unknown node %s", id)
		}
	}
	for id := range graph.States {
		graph.StateIDs = append(graph.StateIDs, id)
	}
	sort.Strings(graph.StateIDs)
	sort.Strings(graph.Inits)
	sort.Slice(graph.Edges, func(left, right int) bool {
		if graph.Edges[left].Src != graph.Edges[right].Src {
			return graph.Edges[left].Src < graph.Edges[right].Src
		}
		if graph.Edges[left].Action != graph.Edges[right].Action {
			return graph.Edges[left].Action < graph.Edges[right].Action
		}
		return graph.Edges[left].Dst < graph.Edges[right].Dst
	})
	return graph, nil
}

func parseEdge(line string) (Edge, error) {
	left, rest, err := consumeID(line)
	if err != nil {
		return Edge{}, err
	}
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, "->") {
		return Edge{}, errors.New("missing edge arrow")
	}
	right, rest, err := consumeID(strings.TrimSpace(strings.TrimPrefix(rest, "->")))
	if err != nil {
		return Edge{}, err
	}
	attributes, err := bracketAttributes(strings.TrimSpace(rest))
	if err != nil {
		return Edge{}, err
	}
	parsedAttributes, err := parseAttributes(attributes)
	if err != nil {
		return Edge{}, err
	}
	label, ok := parsedAttributes["label"]
	if !ok {
		return Edge{}, errors.New("missing label attribute")
	}
	if err := validateActionLabel(label); err != nil {
		return Edge{}, err
	}
	if err := validateEdgeAttributes(parsedAttributes); err != nil {
		return Edge{}, err
	}
	return Edge{Src: left, Dst: right, Action: label}, nil
}

func parseRankStatement(line string) ([]string, error) {
	const prefix = "{rank = same; "
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, ";}") {
		return nil, fmt.Errorf("invalid rank statement %q", line)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(line, prefix), ";}")
	parts := strings.Split(body, ";")
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty rank statement %q", line)
	}
	for _, id := range parts {
		if !validSignedDecimal(id) {
			return nil, fmt.Errorf("invalid rank node id %q", id)
		}
	}
	return parts, nil
}

func parseNodeStatement(line string) (string, string, bool, error) {
	if len(line) == 0 || (line[0] != '-' && line[0] != '"' && !unicode.IsDigit(rune(line[0]))) {
		return "", "", false, nil
	}
	id, rest, err := consumeID(line)
	if err != nil {
		return "", "", false, err
	}
	attributes, err := bracketAttributes(strings.TrimSpace(rest))
	if err != nil {
		return "", "", false, err
	}
	return id, attributes, true, nil
}

func consumeID(input string) (string, string, error) {
	if strings.HasPrefix(input, "\"") {
		end := strings.Index(input[1:], "\"")
		if end < 0 {
			return "", "", errors.New("unterminated quoted node id")
		}
		id := input[1 : end+1]
		if !validSignedDecimal(id) {
			return "", "", fmt.Errorf("invalid node id %q", id)
		}
		return id, input[end+2:], nil
	}
	end := 0
	for end < len(input) && input[end] != ' ' && input[end] != '[' {
		end++
	}
	if end == 0 || !validSignedDecimal(input[:end]) {
		return "", "", fmt.Errorf("invalid node id prefix %q", input)
	}
	return input[:end], input[end:], nil
}

func validSignedDecimal(value string) bool {
	if value == "" {
		return false
	}
	start := 0
	if value[0] == '-' {
		start = 1
	}
	if start == len(value) {
		return false
	}
	for _, char := range value[start:] {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func bracketAttributes(input string) (string, error) {
	if !strings.HasPrefix(input, "[") {
		return "", errors.New("missing attributes")
	}
	end := strings.LastIndex(input, "]")
	if end < 0 {
		return "", errors.New("unterminated attributes")
	}
	trailing := strings.TrimSpace(input[end+1:])
	if trailing != "" && trailing != ";" {
		return "", fmt.Errorf("unexpected trailing text %q", trailing)
	}
	return input[1:end], nil
}

func parseAttributes(input string) (map[string]string, error) {
	result := make(map[string]string)
	for offset := 0; ; {
		for offset < len(input) && (input[offset] == ' ' || input[offset] == '\t') {
			offset++
		}
		if offset == len(input) {
			return result, nil
		}
		start := offset
		for offset < len(input) && ((input[offset] >= 'a' && input[offset] <= 'z') ||
			(input[offset] >= 'A' && input[offset] <= 'Z') || input[offset] == '_') {
			offset++
		}
		if start == offset {
			return nil, fmt.Errorf("invalid attribute near %q", input[offset:])
		}
		name := input[start:offset]
		for offset < len(input) && (input[offset] == ' ' || input[offset] == '\t') {
			offset++
		}
		if offset >= len(input) || input[offset] != '=' {
			return nil, fmt.Errorf("attribute %s has no equals", name)
		}
		offset++
		for offset < len(input) && (input[offset] == ' ' || input[offset] == '\t') {
			offset++
		}
		var value string
		if offset < len(input) && input[offset] == '"' {
			start, escaped := offset, false
			offset++
			for offset < len(input) {
				char := input[offset]
				offset++
				if escaped {
					escaped = false
					continue
				}
				if char == '\\' {
					escaped = true
					continue
				}
				if char == '"' {
					decoded, err := strconv.Unquote(input[start:offset])
					if err != nil {
						return nil, fmt.Errorf("decode attribute %s: %w", name, err)
					}
					value = decoded
					break
				}
			}
			if value == "" && (offset == 0 || input[offset-1] != '"') {
				return nil, fmt.Errorf("unterminated attribute %s", name)
			}
		} else {
			start = offset
			for offset < len(input) && input[offset] != ',' {
				offset++
			}
			value = strings.TrimSpace(input[start:offset])
			if value == "" {
				return nil, fmt.Errorf("attribute %s has empty value", name)
			}
		}
		if _, duplicate := result[name]; duplicate {
			return nil, fmt.Errorf("duplicate attribute %s", name)
		}
		result[name] = value
		for offset < len(input) && (input[offset] == ' ' || input[offset] == '\t') {
			offset++
		}
		if offset < len(input) && input[offset] != ',' {
			return nil, fmt.Errorf("attribute %s has trailing text %q", name, input[offset:])
		}
		if offset < len(input) {
			offset++
			separator := offset
			for offset < len(input) && (input[offset] == ' ' || input[offset] == '\t') {
				offset++
			}
			if offset == len(input) || input[offset] == ',' {
				return nil, fmt.Errorf("invalid attribute separator near %q", input[separator-1:])
			}
		}
	}
}

func validateNodeAttributes(attributes map[string]string) error {
	for name := range attributes {
		switch name {
		case "label", "style", "tooltip":
		default:
			return fmt.Errorf("unsupported node attribute %s", name)
		}
	}
	if style, ok := attributes["style"]; ok && style != "filled" {
		return fmt.Errorf("unsupported node style %q", style)
	}
	if tooltip, ok := attributes["tooltip"]; ok && tooltip != attributes["label"] {
		return errors.New("node tooltip does not match label")
	}
	return nil
}

func validateEdgeAttributes(attributes map[string]string) error {
	for name := range attributes {
		switch name {
		case "label", "color", "fontcolor":
		default:
			return fmt.Errorf("unsupported edge attribute %s", name)
		}
	}
	for _, name := range []string{"color", "fontcolor"} {
		if value, ok := attributes[name]; ok && value != "black" {
			return fmt.Errorf("unsupported edge %s %q", name, value)
		}
	}
	return nil
}

func validateActionLabel(label string) error {
	if label == "" || !unicode.IsUpper(rune(label[0])) {
		return fmt.Errorf("invalid action label %q", label)
	}
	depth := 0
	for _, char := range label {
		switch {
		case unicode.IsLetter(char), unicode.IsDigit(char), char == '_', char == ',', char == '-', char == ' ':
		case char == '(':
			depth++
		case char == ')':
			depth--
			if depth < 0 {
				return fmt.Errorf("invalid action label %q", label)
			}
		default:
			return fmt.Errorf("invalid action label %q", label)
		}
	}
	if depth != 0 {
		return fmt.Errorf("invalid action label %q", label)
	}
	return nil
}

func parseState(label string) (State, error) {
	state := make(State)
	for _, line := range strings.Split(label, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "/\\ ") {
			return nil, fmt.Errorf("invalid conjunction line %q", line)
		}
		assignment := strings.TrimPrefix(line, "/\\ ")
		separator := strings.Index(assignment, " = ")
		if separator < 1 {
			return nil, fmt.Errorf("invalid assignment %q", assignment)
		}
		name := assignment[:separator]
		if !validVariable(name) {
			return nil, fmt.Errorf("invalid variable %q", name)
		}
		if _, duplicate := state[name]; duplicate {
			return nil, fmt.Errorf("duplicate variable %q", name)
		}
		value, err := ParseValue(assignment[separator+3:])
		if err != nil {
			return nil, fmt.Errorf("variable %s: %w", name, err)
		}
		state[name] = value
	}
	if len(state) == 0 {
		return nil, errors.New("empty state label")
	}
	return state, nil
}

func validVariable(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, char := range name[1:] {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

// ParseValue parses int, string, int set, or BOOLEAN and rejects every other
// TLA+ value shape.
func ParseValue(input string) (Value, error) {
	parser := valueParser{input: strings.TrimSpace(input)}
	value, err := parser.parse()
	if err != nil {
		return Value{}, err
	}
	parser.skipSpace()
	if parser.offset != len(parser.input) {
		return Value{}, fmt.Errorf("trailing value text %q", parser.input[parser.offset:])
	}
	return value, nil
}

type valueParser struct {
	input  string
	offset int
}

func (parser *valueParser) parse() (Value, error) {
	parser.skipSpace()
	if parser.offset >= len(parser.input) {
		return Value{}, errors.New("empty value")
	}
	switch parser.input[parser.offset] {
	case '"':
		return parser.parseString()
	case '{':
		return parser.parseSet()
	default:
		if strings.HasPrefix(parser.input[parser.offset:], "TRUE") {
			parser.offset += len("TRUE")
			return Value{Kind: Boolean, Bool: true}, nil
		}
		if strings.HasPrefix(parser.input[parser.offset:], "FALSE") {
			parser.offset += len("FALSE")
			return Value{Kind: Boolean, Bool: false}, nil
		}
		integer, err := parser.parseInteger()
		return Value{Kind: Integer, Int: integer}, err
	}
}

func (parser *valueParser) parseString() (Value, error) {
	start, escaped := parser.offset, false
	parser.offset++
	for parser.offset < len(parser.input) {
		char := parser.input[parser.offset]
		parser.offset++
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == '"' {
			decoded, err := strconv.Unquote(parser.input[start:parser.offset])
			if err != nil {
				return Value{}, err
			}
			return Value{Kind: String, String: decoded}, nil
		}
	}
	return Value{}, errors.New("unterminated string")
}

func (parser *valueParser) parseSet() (Value, error) {
	parser.offset++
	parser.skipSpace()
	values := make([]int64, 0)
	if parser.offset < len(parser.input) && parser.input[parser.offset] == '}' {
		parser.offset++
		return Value{Kind: IntegerSet, Set: values}, nil
	}
	for {
		value, err := parser.parseInteger()
		if err != nil {
			return Value{}, fmt.Errorf("integer set: %w", err)
		}
		values = append(values, value)
		parser.skipSpace()
		if parser.offset >= len(parser.input) {
			return Value{}, errors.New("unterminated integer set")
		}
		switch parser.input[parser.offset] {
		case '}':
			parser.offset++
			for index := 1; index < len(values); index++ {
				if values[index] <= values[index-1] {
					return Value{}, errors.New("integer set is not strictly sorted")
				}
			}
			return Value{Kind: IntegerSet, Set: values}, nil
		case ',':
			parser.offset++
			parser.skipSpace()
		default:
			return Value{}, fmt.Errorf("unexpected integer set token %q", parser.input[parser.offset])
		}
	}
}

func (parser *valueParser) parseInteger() (int64, error) {
	parser.skipSpace()
	start := parser.offset
	if parser.offset < len(parser.input) && parser.input[parser.offset] == '-' {
		parser.offset++
	}
	digits := parser.offset
	for parser.offset < len(parser.input) && parser.input[parser.offset] >= '0' && parser.input[parser.offset] <= '9' {
		parser.offset++
	}
	if parser.offset == digits {
		return 0, fmt.Errorf("expected integer at %q", parser.input[start:])
	}
	value, err := strconv.ParseInt(parser.input[start:parser.offset], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("integer: %w", err)
	}
	return value, nil
}

func (parser *valueParser) skipSpace() {
	for parser.offset < len(parser.input) && (parser.input[parser.offset] == ' ' || parser.input[parser.offset] == '\t') {
		parser.offset++
	}
}
