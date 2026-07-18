package tracecover

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

const maxTraceFiles = 4096

// LoadTraces recursively reads every .ndjson file below roots. Each supplied
// source family is required to contain at least one trace, preventing an empty
// directory from satisfying a coverage command vacuously.
func LoadTraces(source Source, roots []string) ([]Trace, error) {
	if source != SourceCorpus && source != SourceWitness {
		return nil, fmt.Errorf("tracecover: invalid trace source %q", source)
	}
	if len(roots) == 0 {
		return nil, nil
	}
	paths := make([]string, 0)
	seen := make(map[string]struct{})
	for _, root := range roots {
		root = filepath.Clean(root)
		info, err := os.Lstat(root)
		if err != nil {
			return nil, fmt.Errorf("tracecover: inspect %s %s: %w", source, root, err)
		}
		if !info.IsDir() {
			if filepath.Ext(root) != ".ndjson" || !info.Mode().IsRegular() {
				return nil, fmt.Errorf("tracecover: %s input %s is not an .ndjson file", source, root)
			}
			absolute, err := filepath.Abs(root)
			if err != nil {
				return nil, fmt.Errorf("tracecover: resolve %s: %w", root, err)
			}
			if _, exists := seen[absolute]; !exists {
				seen[absolute] = struct{}{}
				paths = append(paths, root)
				if len(paths) > maxTraceFiles {
					return nil, fmt.Errorf("tracecover: more than %d trace files", maxTraceFiles)
				}
			}
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".ndjson" {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("trace input %s is not a regular file", path)
			}
			absolute, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			if _, exists := seen[absolute]; exists {
				return nil
			}
			seen[absolute] = struct{}{}
			paths = append(paths, path)
			if len(paths) > maxTraceFiles {
				return fmt.Errorf("more than %d trace files", maxTraceFiles)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("tracecover: walk %s %s: %w", source, root, err)
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("tracecover: %s input contains no .ndjson traces", source)
	}
	sort.Strings(paths)

	traces := make([]Trace, 0, len(paths))
	for _, path := range paths {
		input, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("tracecover: open trace %s: %w", path, err)
		}
		events, readErr := objtrace.ReadNDJSON(input)
		closeErr := input.Close()
		if readErr != nil {
			return nil, fmt.Errorf("tracecover: read trace %s: %w", path, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("tracecover: close trace %s: %w", path, closeErr)
		}
		traces = append(traces, Trace{
			Name:   strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
			Source: source,
			Events: events,
		})
	}
	return traces, nil
}
