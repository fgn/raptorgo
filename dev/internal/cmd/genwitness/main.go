// Command genwitness deterministically selects one shortest legal corpus trace
// for every pinned ObjectDecoder trace action.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
	"github.com/fgn/raptorgo/dev/internal/tracecover"
)

func main() {
	corpus := flag.String("corpus", "", "harvested corpus directory")
	manifestPath := flag.String("manifest", "", "action manifest")
	output := flag.String("output", "", "witness output directory")
	flag.Parse()
	if *corpus == "" || *manifestPath == "" || *output == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	manifest, err := tracecover.LoadManifest(*manifestPath)
	must(err)
	traces, err := tracecover.LoadTraces(tracecover.SourceCorpus, []string{*corpus})
	must(err)
	witnesses, err := selectWitnesses(manifest, traces)
	must(err)
	must(writeWitnesses(*output, witnesses))
}

type witness struct {
	name   string
	events []objtrace.Event
}

func selectWitnesses(manifest tracecover.Manifest, traces []tracecover.Trace) ([]witness, error) {
	type candidate struct {
		trace       tracecover.Trace
		fingerprint string
	}
	candidates := make([]candidate, len(traces))
	for index, trace := range traces {
		fingerprint, err := objtrace.ShortFingerprint(trace.Events)
		if err != nil {
			return nil, err
		}
		candidates[index] = candidate{trace: trace, fingerprint: fingerprint}
	}
	sort.Slice(candidates, func(left, right int) bool {
		if len(candidates[left].trace.Events) != len(candidates[right].trace.Events) {
			return len(candidates[left].trace.Events) < len(candidates[right].trace.Events)
		}
		if candidates[left].fingerprint != candidates[right].fingerprint {
			return candidates[left].fingerprint < candidates[right].fingerprint
		}
		return candidates[left].trace.Name < candidates[right].trace.Name
	})
	result := make([]witness, 0, len(manifest.Actions))
	for index, action := range manifest.Actions {
		selected := -1
		for candidateIndex, candidate := range candidates {
			if traceCovers(candidate.trace.Events, action) {
				selected = candidateIndex
				break
			}
		}
		if selected < 0 {
			return nil, fmt.Errorf("genwitness: no corpus trace covers %s", action.Name)
		}
		result = append(result, witness{
			name:   fmt.Sprintf("%02d-%s.ndjson", index, action.Name),
			events: candidates[selected].trace.Events,
		})
	}
	return result, nil
}

func traceCovers(events []objtrace.Event, action tracecover.Action) bool {
	var previous uint32
	for _, event := range events {
		delta := int32(event.Completed - previous)
		previous = event.Completed
		if event.Op == "init" {
			continue
		}
		if event.Op == action.Op && event.Result == action.Result && delta == action.CompletedDelta {
			return true
		}
	}
	return false
}

func writeWitnesses(output string, witnesses []witness) error {
	parent, base := filepath.Dir(output), filepath.Base(output)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, "."+base+".staging-")
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(staging)
		}
	}()
	for _, item := range witnesses {
		encoded, err := objtrace.CanonicalNDJSON(item.events)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(staging, item.name), encoded, 0o644); err != nil {
			return err
		}
	}
	backup := output + ".previous"
	if err := os.RemoveAll(backup); err != nil {
		return err
	}
	if _, err := os.Stat(output); err == nil {
		if err := os.Rename(output, backup); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staging, output); err != nil {
		_ = os.Rename(backup, output)
		return err
	}
	complete = true
	return os.RemoveAll(backup)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
