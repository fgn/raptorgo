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
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || shouldIgnore(line) {
			continue
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
		label, err := labelAttribute(attributes)
		if err != nil {
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
		if hasFilledStyle(attributes) {
			graph.Inits = append(graph.Inits, id)
		}
	}
	if err := scanner.Err(); err != nil {
		return Graph{}, fmt.Errorf("dotparse: scan: %w", err)
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

func shouldIgnore(line string) bool {
	return line == "strict digraph DiskGraph {" || line == "subgraph cluster_graph {" || line == "}" ||
		strings.HasPrefix(line, "node [") || strings.HasPrefix(line, "nodesep=") ||
		strings.HasPrefix(line, "color=") || strings.HasPrefix(line, "{rank = same;")
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
	label, err := labelAttribute(attributes)
	if err != nil {
		return Edge{}, err
	}
	if err := validateActionLabel(label); err != nil {
		return Edge{}, err
	}
	return Edge{Src: left, Dst: right, Action: label}, nil
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

func labelAttribute(attributes string) (string, error) {
	index := strings.Index(attributes, "label=")
	if index < 0 {
		return "", errors.New("missing label attribute")
	}
	input := strings.TrimSpace(attributes[index+len("label="):])
	if !strings.HasPrefix(input, "\"") {
		return "", errors.New("label is not quoted")
	}
	end, escaped := -1, false
	for index := 1; index < len(input); index++ {
		if escaped {
			escaped = false
			continue
		}
		if input[index] == '\\' {
			escaped = true
			continue
		}
		if input[index] == '"' {
			end = index
			break
		}
	}
	if end < 0 {
		return "", errors.New("unterminated label")
	}
	label, err := strconv.Unquote(input[:end+1])
	if err != nil {
		return "", fmt.Errorf("decode label: %w", err)
	}
	return label, nil
}

func hasFilledStyle(attributes string) bool {
	compact := strings.NewReplacer(" ", "", "\t", "").Replace(attributes)
	return strings.Contains(compact, "style=filled")
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
