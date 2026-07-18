// Package schedule selects deterministic edge-covering model paths and folds
// synchronous ObjectDecoder finalization microsteps into public API calls.
package schedule

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/fgn/raptorgo/dev/internal/mbt/dotparse"
)

const DepthCap = 32

// Schedule is one executable public-API witness path.
type Schedule struct {
	V       int    `json:"v"`
	Model   string `json:"model"`
	Cfg     string `json:"cfg"`
	Fixture string `json:"fixture"`
	Steps   []Step `json:"steps"`
}

// Step is one ObjectDecoder construction or AddPacket call.
type Step struct {
	Action       string     `json:"action"`
	SBN          *uint8     `json:"sbn,omitempty"`
	ESI          *uint32    `json:"esi,omitempty"`
	Callbacks    *Callbacks `json:"callbacks,omitempty"`
	Expect       Expect     `json:"expect"`
	ModelActions []string   `json:"modelActions"`
}

// Callbacks controls one fused verifier/deliverer call sequence.
type Callbacks struct {
	Verify               string `json:"verify"`
	Deliver              string `json:"deliver,omitempty"`
	VerifyReentrantAdds  int    `json:"verifyReentrantAdds,omitempty"`
	DeliverReentrantAdds int    `json:"deliverReentrantAdds,omitempty"`
}

// Expect is Oracle 1's exact post-call observation.
type Expect struct {
	State     string   `json:"state"`
	Completed uint32   `json:"completed"`
	Result    string   `json:"result"`
	Events    []string `json:"events"`
}

// NamedSchedule couples a stable filename stem to a schedule.
type NamedSchedule struct {
	Name     string
	Schedule Schedule
}

// SelectPaths implements deterministic edge coverage. It marks edges as soon
// as they are appended, preventing an uncovered self-loop from filling the
// depth cap, and completes transient finalization suffixes before returning.
func SelectPaths(graph dotparse.Graph) ([][]dotparse.Edge, error) {
	if len(graph.Inits) != 1 {
		return nil, fmt.Errorf("schedule: graph has %d initial states, want 1", len(graph.Inits))
	}
	edgeIndex, outgoing, err := indexGraph(graph)
	if err != nil {
		return nil, err
	}
	covered := make([]bool, len(graph.Edges))
	paths := make([][]dotparse.Edge, 0)
	for target := range graph.Edges {
		if covered[target] {
			continue
		}
		prefix, err := shortestPath(graph.Inits[0], graph.Edges[target].Src, graph, outgoing)
		if err != nil {
			return nil, fmt.Errorf("schedule: edge %s --%s--> %s: %w",
				graph.Edges[target].Src, graph.Edges[target].Action, graph.Edges[target].Dst, err)
		}
		path := append(prefix, graph.Edges[target])
		markCovered(path, covered, edgeIndex)
		end := graph.Edges[target].Dst
		for len(path) < DepthCap {
			candidate := firstUncovered(outgoing[end], covered, graph.Edges)
			if candidate < 0 {
				break
			}
			minimum, err := minimumCompletion(graph.Edges[candidate].Dst, graph, outgoing)
			if err != nil {
				return nil, err
			}
			if len(path)+1+len(minimum) > DepthCap {
				break
			}
			path = append(path, graph.Edges[candidate])
			covered[candidate] = true
			end = graph.Edges[candidate].Dst
		}
		completion, err := minimumCompletion(end, graph, outgoing)
		if err != nil {
			return nil, err
		}
		if len(path)+len(completion) > DepthCap {
			return nil, fmt.Errorf("schedule: completion exceeds depth cap %d", DepthCap)
		}
		path = append(path, completion...)
		markCovered(completion, covered, edgeIndex)
		paths = append(paths, path)
	}
	for index, isCovered := range covered {
		if !isCovered {
			return nil, fmt.Errorf("schedule: edge %d remained uncovered", index)
		}
	}
	return paths, nil
}

// Build converts selected model paths to folded public-API schedules.
func Build(graph dotparse.Graph, cfg, fixture string) ([]NamedSchedule, error) {
	paths, err := SelectPaths(graph)
	if err != nil {
		return nil, err
	}
	result := make([]NamedSchedule, 0, len(paths))
	for index, path := range paths {
		steps, err := Fold(path, graph.States)
		if err != nil {
			return nil, fmt.Errorf("schedule: path %d: %w", index, err)
		}
		digest := pathDigest(path)
		result = append(result, NamedSchedule{
			Name: fmt.Sprintf("%03d-%s.json", index, digest),
			Schedule: Schedule{
				V: 1, Model: "ObjectDecoderModel", Cfg: cfg, Fixture: fixture, Steps: steps,
			},
		})
	}
	return result, nil
}

// Marshal returns the canonical committed schedule representation.
func Marshal(schedule Schedule) ([]byte, error) {
	encoded, err := json.Marshal(schedule)
	if err != nil {
		return nil, fmt.Errorf("schedule: encode: %w", err)
	}
	return append(encoded, '\n'), nil
}

// Fold collapses a complete FinalizeVerifyEnter microstep suffix into the
// completing AddPacket step. It rejects paths ending inside an API call.
func Fold(path []dotparse.Edge, states map[string]dotparse.State) ([]Step, error) {
	steps := make([]Step, 0, len(path))
	for index := 0; index < len(path); {
		edge := path[index]
		name, parameters, err := ParseAction(edge.Action)
		if err != nil {
			return nil, err
		}
		if name != "FinalizeVerifyEnter" {
			step, err := ordinaryStep(edge, name, parameters, states)
			if err != nil {
				return nil, err
			}
			steps = append(steps, step)
			index++
			continue
		}
		step, consumed, err := finalizationStep(path[index:], parameters, states)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
		index += consumed
	}
	return steps, nil
}

// ParseAction separates a full TLC action label from its integer parameters.
func ParseAction(label string) (string, []int, error) {
	open := strings.IndexByte(label, '(')
	if open < 0 {
		if label == "" {
			return "", nil, errors.New("schedule: empty action")
		}
		return label, nil, nil
	}
	if !strings.HasSuffix(label, ")") {
		return "", nil, fmt.Errorf("schedule: malformed action %q", label)
	}
	name := label[:open]
	parts := strings.Split(label[open+1:len(label)-1], ",")
	parameters := make([]int, len(parts))
	for index, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value < 0 {
			return "", nil, fmt.Errorf("schedule: malformed action parameter in %q", label)
		}
		parameters[index] = value
	}
	return name, parameters, nil
}

func ordinaryStep(edge dotparse.Edge, name string, parameters []int, states map[string]dotparse.State) (Step, error) {
	state, completed, err := expectedState(states[edge.Dst])
	if err != nil {
		return Step{}, fmt.Errorf("schedule: action %s destination: %w", edge.Action, err)
	}
	step := Step{Action: name, ModelActions: []string{edge.Action}}
	event, result := "", ""
	switch name {
	case "AddWait":
		event, result = "add_wait", "ok"
	case "BlockComplete":
		event, result = "block_complete", "ok"
	case "RejectInvalid":
		event, result = "add_reject_invalid", "invalid"
	case "RejectLimit":
		event, result = "add_reject_limit", "limit"
	case "RejectConflict":
		event, result = "add_reject_conflict", "conflict"
	case "TerminalAdd":
		event, result = "terminal_add", "terminal"
	case "DecodeFail":
		event, result = "decode_failure", "decode_error"
	case "Verified", "VerifyFailed", "DeliverEnter", "Delivered", "DeliveryFailed":
		return Step{}, fmt.Errorf("schedule: orphan finalization action %s", name)
	default:
		return Step{}, fmt.Errorf("schedule: unknown action %s", name)
	}
	if name == "AddWait" || name == "BlockComplete" || name == "RejectConflict" {
		if len(parameters) != 2 || parameters[0] > 255 {
			return Step{}, fmt.Errorf("schedule: action %s wants (sbn,esi)", edge.Action)
		}
		sbn, esi := uint8(parameters[0]), uint32(parameters[1])
		step.SBN, step.ESI = &sbn, &esi
	} else if len(parameters) != 0 {
		return Step{}, fmt.Errorf("schedule: parameterless action %s has parameters", edge.Action)
	}
	step.Expect = Expect{State: state, Completed: completed, Result: result, Events: []string{event}}
	return step, nil
}

func finalizationStep(path []dotparse.Edge, parameters []int, states map[string]dotparse.State) (Step, int, error) {
	if len(parameters) != 2 || parameters[0] > 255 {
		return Step{}, 0, errors.New("schedule: FinalizeVerifyEnter wants (sbn,esi)")
	}
	sbn, esi := uint8(parameters[0]), uint32(parameters[1])
	step := Step{
		Action: "FinalizeVerifyEnter", SBN: &sbn, ESI: &esi,
		Callbacks:    &Callbacks{Verify: "ok"},
		ModelActions: []string{path[0].Action},
	}
	events := []string{"verify_enter"}
	consumed := 1
	phase := "decoded"
	for consumed < len(path) {
		edge := path[consumed]
		name, parameters, err := ParseAction(edge.Action)
		if err != nil {
			return Step{}, 0, err
		}
		if len(parameters) != 0 {
			return Step{}, 0, fmt.Errorf("schedule: parameterized finalization action %s", edge.Action)
		}
		switch phase {
		case "decoded":
			switch name {
			case "TerminalAdd":
				step.Callbacks.VerifyReentrantAdds++
				events = append(events, "terminal_add")
			case "Verified":
				events = append(events, "verified")
				phase = "verified-before-delivery"
			case "VerifyFailed":
				step.Callbacks.Verify = "fail"
				events = append(events, "add_return")
				return finalizeExpected(step, events, edge, consumed+1, "verification_error", states)
			default:
				return Step{}, 0, fmt.Errorf("schedule: %s cannot follow decoded", name)
			}
		case "verified-before-delivery":
			if name != "DeliverEnter" {
				return Step{}, 0, fmt.Errorf("schedule: %s cannot precede delivery entry", name)
			}
			step.Callbacks.Deliver = "ok"
			events = append(events, "deliver_enter")
			phase = "verified-after-delivery"
		case "verified-after-delivery":
			switch name {
			case "TerminalAdd":
				step.Callbacks.DeliverReentrantAdds++
				events = append(events, "terminal_add")
			case "Delivered":
				events = append(events, "add_return")
				return finalizeExpected(step, events, edge, consumed+1, "ok", states)
			case "DeliveryFailed":
				step.Callbacks.Deliver = "fail"
				events = append(events, "add_return")
				return finalizeExpected(step, events, edge, consumed+1, "delivery_error", states)
			default:
				return Step{}, 0, fmt.Errorf("schedule: %s cannot finish delivery", name)
			}
		}
		step.ModelActions = append(step.ModelActions, edge.Action)
		consumed++
	}
	return Step{}, 0, errors.New("schedule: path ends inside finalization")
}

func finalizeExpected(
	step Step,
	events []string,
	edge dotparse.Edge,
	consumed int,
	result string,
	states map[string]dotparse.State,
) (Step, int, error) {
	step.ModelActions = append(step.ModelActions, edge.Action)
	state, completed, err := expectedState(states[edge.Dst])
	if err != nil {
		return Step{}, 0, err
	}
	step.Expect = Expect{State: state, Completed: completed, Result: result, Events: events}
	return step, consumed, nil
}

func expectedState(state dotparse.State) (string, uint32, error) {
	stateValue, ok := state["state"]
	if !ok || stateValue.Kind != dotparse.String {
		return "", 0, errors.New("missing string state")
	}
	completed, ok := state["completed"]
	if !ok || completed.Kind != dotparse.Integer || completed.Int < 0 || completed.Int > int64(^uint32(0)) {
		return "", 0, errors.New("missing uint32 completed")
	}
	return stateValue.String, uint32(completed.Int), nil
}

func indexGraph(graph dotparse.Graph) (map[dotparse.Edge]int, map[string][]int, error) {
	edgeIndex := make(map[dotparse.Edge]int, len(graph.Edges))
	outgoing := make(map[string][]int)
	for index, edge := range graph.Edges {
		if _, duplicate := edgeIndex[edge]; duplicate {
			return nil, nil, fmt.Errorf("schedule: duplicate edge %#v", edge)
		}
		edgeIndex[edge] = index
		outgoing[edge.Src] = append(outgoing[edge.Src], index)
	}
	return edgeIndex, outgoing, nil
}

func shortestPath(source, destination string, graph dotparse.Graph, outgoing map[string][]int) ([]dotparse.Edge, error) {
	if source == destination {
		return nil, nil
	}
	queue := []string{source}
	seen := map[string]bool{source: true}
	previousState := make(map[string]string)
	previousEdge := make(map[string]int)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, edgeIndex := range outgoing[current] {
			edge := graph.Edges[edgeIndex]
			if seen[edge.Dst] {
				continue
			}
			seen[edge.Dst] = true
			previousState[edge.Dst] = current
			previousEdge[edge.Dst] = edgeIndex
			if edge.Dst == destination {
				return rebuildPath(source, destination, previousState, previousEdge, graph.Edges), nil
			}
			queue = append(queue, edge.Dst)
		}
	}
	return nil, fmt.Errorf("no path from %s to %s", source, destination)
}

func rebuildPath(source, destination string, previousState map[string]string, previousEdge map[string]int, edges []dotparse.Edge) []dotparse.Edge {
	var reverse []dotparse.Edge
	for current := destination; current != source; current = previousState[current] {
		reverse = append(reverse, edges[previousEdge[current]])
	}
	result := make([]dotparse.Edge, len(reverse))
	for index := range reverse {
		result[len(reverse)-1-index] = reverse[index]
	}
	return result
}

func firstUncovered(indices []int, covered []bool, edges []dotparse.Edge) int {
	for _, index := range indices {
		if !covered[index] {
			return index
		}
	}
	return -1
}

func markCovered(path []dotparse.Edge, covered []bool, edgeIndex map[dotparse.Edge]int) {
	for _, edge := range path {
		if index, ok := edgeIndex[edge]; ok {
			covered[index] = true
		}
	}
}

func minimumCompletion(stateID string, graph dotparse.Graph, outgoing map[string][]int) ([]dotparse.Edge, error) {
	state, ok := graph.States[stateID]
	if !ok {
		return nil, fmt.Errorf("schedule: unknown state %s", stateID)
	}
	phase, _, err := expectedState(state)
	if err != nil {
		return nil, err
	}
	if phase != "decoded" && phase != "verified" {
		return nil, nil
	}
	if phase == "verified" {
		delivery := state["delivery"]
		if delivery.Kind != dotparse.Integer {
			return nil, errors.New("schedule: verified state has no delivery counter")
		}
		if delivery.Int == 0 {
			enter, err := edgeNamed(stateID, "DeliverEnter", graph, outgoing)
			if err != nil {
				return nil, err
			}
			delivered, err := edgeNamed(enter.Dst, "Delivered", graph, outgoing)
			if err != nil {
				return nil, err
			}
			return []dotparse.Edge{enter, delivered}, nil
		}
		delivered, err := edgeNamed(stateID, "Delivered", graph, outgoing)
		if err != nil {
			return nil, err
		}
		return []dotparse.Edge{delivered}, nil
	}
	failed, err := edgeNamed(stateID, "VerifyFailed", graph, outgoing)
	if err != nil {
		return nil, err
	}
	return []dotparse.Edge{failed}, nil
}

func edgeNamed(source, name string, graph dotparse.Graph, outgoing map[string][]int) (dotparse.Edge, error) {
	for _, index := range outgoing[source] {
		action, _, err := ParseAction(graph.Edges[index].Action)
		if err != nil {
			return dotparse.Edge{}, err
		}
		if action == name {
			return graph.Edges[index], nil
		}
	}
	return dotparse.Edge{}, fmt.Errorf("schedule: state %s has no %s edge", source, name)
}

func pathDigest(path []dotparse.Edge) string {
	var canonical strings.Builder
	for _, edge := range path {
		fmt.Fprintf(&canonical, "%s\x00%s\x00%s\n", edge.Src, edge.Action, edge.Dst)
	}
	digest := sha256.Sum256([]byte(canonical.String()))
	return hex.EncodeToString(digest[:])[:12]
}

// CoveredEdges returns the distinct edge triples represented by schedules.
func CoveredEdges(schedules []NamedSchedule) []string {
	seen := make(map[string]bool)
	for _, named := range schedules {
		for _, step := range named.Schedule.Steps {
			for _, action := range step.ModelActions {
				seen[action] = true
			}
		}
	}
	result := make([]string, 0, len(seen))
	for action := range seen {
		result = append(result, action)
	}
	sort.Strings(result)
	return result
}
