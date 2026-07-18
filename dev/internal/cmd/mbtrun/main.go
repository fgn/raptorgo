package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/fgn/raptorgo/dev/internal/mbt/driver"
	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

func main() {
	schedules := flag.String("schedules", "", "schedule file or directory")
	traces := flag.String("traces", "", "optional output directory for NDJSON traces")
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
	for _, path := range paths {
		value, err := driver.ReadScheduleFile(path)
		must(err)
		result, err := driver.Run(value)
		if err != nil {
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

func schedulePaths(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{root}, nil
	}
	var paths []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			paths = append(paths, path)
		}
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
