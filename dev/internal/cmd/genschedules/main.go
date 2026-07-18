package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/fgn/raptorgo/dev/internal/mbt/dotparse"
	"github.com/fgn/raptorgo/dev/internal/mbt/schedule"
)

func main() {
	dot := flag.String("dot", "", "TLC dot,actionlabels input")
	cfg := flag.String("cfg", "", "model configuration tag")
	fixture := flag.String("fixture", "", "driver fixture name")
	output := flag.String("output", "", "schedule output directory")
	flag.Parse()
	if *dot == "" || *cfg == "" || *fixture == "" || *output == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	file, err := os.Open(*dot)
	must(err)
	graph, err := dotparse.Parse(file)
	closeErr := file.Close()
	must(err)
	must(closeErr)
	schedules, err := schedule.Build(graph, *cfg, *fixture)
	must(err)
	parent, base := filepath.Dir(*output), filepath.Base(*output)
	must(os.MkdirAll(parent, 0o755))
	staging, err := os.MkdirTemp(parent, "."+base+".staging-")
	must(err)
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(staging)
		}
	}()
	sort.Slice(schedules, func(left, right int) bool { return schedules[left].Name < schedules[right].Name })
	for _, named := range schedules {
		encoded, encodeErr := schedule.Marshal(named.Schedule)
		must(encodeErr)
		path := filepath.Join(staging, named.Name)
		must(os.WriteFile(path, encoded, 0o644))
	}
	must(replaceDirectory(staging, *output))
	complete = true
	for _, named := range schedules {
		fmt.Println(filepath.Join(*output, named.Name))
	}
}

func replaceDirectory(staging, output string) error {
	backup := output + ".previous"
	if err := os.RemoveAll(backup); err != nil {
		return err
	}
	_, statErr := os.Stat(output)
	hadOutput := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	if hadOutput {
		if err := os.Rename(output, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(staging, output); err != nil {
		if hadOutput {
			_ = os.Rename(backup, output)
		}
		return err
	}
	if hadOutput {
		if err := os.RemoveAll(backup); err != nil {
			return err
		}
	}
	return nil
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
