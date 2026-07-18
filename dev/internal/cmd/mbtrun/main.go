package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fgn/raptorgo/dev/internal/mbt/driver"
	"github.com/fgn/raptorgo/dev/internal/mbt/schedule"
	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

const maxScheduleEntries = 4096

func main() {
	schedules := flag.String("schedules", "", "schedule file or directory")
	traces := flag.String("traces", "", "optional output directory for NDJSON traces")
	failures := flag.String("failures", "", "optional output directory for minimized failure artifacts")
	flag.Parse()
	if *schedules == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	paths, err := schedulePaths(*schedules)
	must(err)
	if len(paths) == 0 {
		panic("mbtrun: no schedule files")
	}
	if *traces != "" {
		must(os.MkdirAll(*traces, 0o755))
	}
	if *failures != "" {
		must(os.MkdirAll(*failures, 0o755))
	}
	for _, path := range paths {
		value, err := driver.ReadScheduleFile(path)
		must(err)
		result, err := driver.Run(value)
		if err != nil {
			if *failures != "" {
				artifactErr := writeFailureArtifacts(*failures, path, value, err)
				if artifactErr != nil {
					err = errors.Join(err, artifactErr)
				}
			}
			panic(fmt.Errorf("mbtrun: %s: %w", path, err))
		}
		fingerprint, err := objtrace.ShortFingerprint(result.Events)
		must(err)
		fmt.Printf("%s %s\n", fingerprint, path)
		if *traces != "" {
			encoded, err := objtrace.CanonicalNDJSON(result.Events)
			must(err)
			name := filepath.Base(path[:len(path)-len(filepath.Ext(path))]) + ".ndjson"
			must(os.WriteFile(filepath.Join(*traces, name), encoded, 0o644))
		}
	}
}

func writeFailureArtifacts(
	root, source string,
	value schedule.Schedule,
	runErr error,
) error {
	signature, ok := driver.SignatureOf(runErr)
	if !ok {
		return errors.New("mbtrun: failure is not a structured driver mismatch")
	}
	shrunk, err := driver.MinimizeFailure(value, signature, driver.ShrinkOptions{})
	if err != nil {
		return fmt.Errorf("mbtrun: minimize failure: %w", err)
	}
	base := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	directory := filepath.Join(root, value.Cfg+"-"+base)
	paths, err := driver.WriteFailureArtifacts(directory, shrunk)
	if err != nil {
		return fmt.Errorf("mbtrun: write failure artifacts: %w", err)
	}
	fmt.Fprintf(
		os.Stderr,
		"mbtrun: minimized %s from %d to %d steps in %d attempts; %s (strict=%t)\n",
		source, shrunk.OriginalSteps, len(shrunk.Schedule.Steps), shrunk.Attempts,
		paths.Failure, paths.StrictSchedule,
	)
	fmt.Fprintf(
		os.Stderr,
		"mbtrun: repro: go -C dev run ./internal/cmd/mbtrun -schedules %s\n",
		shellQuote(paths.Schedule),
	)
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func schedulePaths(root string) ([]string, error) {
	return schedulePathsLimit(root, maxScheduleEntries)
}

func schedulePathsLimit(root string, maximum int) ([]string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("mbtrun: schedule input %s is not a regular file", root)
		}
		return []string{root}, nil
	}
	var paths []string
	visited := 0
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		visited++
		if visited > maximum {
			return fmt.Errorf("mbtrun: schedule tree has more than %d entries", maximum)
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("mbtrun: schedule %s is not a regular file", path)
		}
		paths = append(paths, path)
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
