// Command purego verifies that the production Go module has no CGo files or
// external runtime dependencies. Development tooling lives in a nested module
// and is deliberately outside the production dependency graph.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type listedPackage struct {
	ImportPath string
	Standard   bool
	CgoFiles   []string
}

func main() {
	root, err := filepath.Abs("..")
	if err != nil {
		fatalf("resolve production module root: %v", err)
	}
	modulePath := commandOutput("go", "-C", root, "list", "-m")
	command := exec.Command("go", "-C", root, "list", "-deps", "-json", "./...")
	command.Env = replaceEnvironment(os.Environ(), "CGO_ENABLED", "1")
	output, err := command.StdoutPipe()
	if err != nil {
		fatalf("create go list output pipe: %v", err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		fatalf("start go list: %v", err)
	}

	decoder := json.NewDecoder(output)
	failed := false
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			fatalf("decode go list output: %v", err)
		}
		local := pkg.ImportPath == modulePath || strings.HasPrefix(pkg.ImportPath, modulePath+"/")
		if local && len(pkg.CgoFiles) != 0 {
			fmt.Fprintf(os.Stderr, "%s contains CGo files: %v\n", pkg.ImportPath, pkg.CgoFiles)
			failed = true
		}
		if !local && !pkg.Standard {
			fmt.Fprintf(os.Stderr, "%s is an external runtime dependency\n", pkg.ImportPath)
			failed = true
		}
	}
	if err := command.Wait(); err != nil {
		fatalf("go list failed: %v", err)
	}
	if failed {
		os.Exit(1)
	}
}

func commandOutput(name string, arguments ...string) string {
	output, err := exec.Command(name, arguments...).Output()
	if err != nil {
		fatalf("%s failed: %v", name, err)
	}
	return strings.TrimSpace(string(output))
}

func replaceEnvironment(environment []string, name, value string) []string {
	prefix := name + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
