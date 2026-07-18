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
	must(os.MkdirAll(*output, 0o755))
	entries, err := os.ReadDir(*output)
	must(err)
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			must(os.Remove(filepath.Join(*output, entry.Name())))
		}
	}
	sort.Slice(schedules, func(left, right int) bool { return schedules[left].Name < schedules[right].Name })
	for _, named := range schedules {
		encoded, encodeErr := schedule.Marshal(named.Schedule)
		must(encodeErr)
		path := filepath.Join(*output, named.Name)
		must(os.WriteFile(path, encoded, 0o644))
		fmt.Println(path)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
