package harvest

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/fgn/raptorgo/dev/internal/tracecorpus"
)

const (
	maxFuzzCorpusBytes = 1 << 20
	maxFuzzCorpusFiles = 4096
)

// FuzzCandidates replays the checked Go fuzz corpus and, when supplied, the
// matching Go fuzz cache. The cache path must name this fuzz target's leaf
// directory; accepting an entire cache tree would mix incompatible schemas.
func FuzzCandidates(corpus, cache string) ([]tracecorpus.Candidate, error) {
	paths, err := fuzzCorpusPaths(corpus, cache)
	if err != nil {
		return nil, err
	}
	result := make([]tracecorpus.Candidate, 0, len(paths))
	for _, path := range paths {
		seed, err := readFuzzSeed(path)
		if err != nil {
			return nil, err
		}
		events, err := runInlineFuzzSeed(seed.data, seed.size, seed.blocks, seed.subBlocks)
		if err != nil {
			return nil, fmt.Errorf("harvest: replay fuzz seed %s: %w", path, err)
		}
		result = append(result, tracecorpus.Candidate{
			Source:   "fuzz:" + filepath.ToSlash(path),
			Features: []tracecorpus.Feature{FeatureInlineFuzzSeed},
			Events:   events,
		})
	}
	return result, nil
}

func fuzzCorpusPaths(corpus, cache string) ([]string, error) {
	if corpus == "" {
		return nil, errors.New("harvest: fuzz corpus path is empty")
	}
	paths, err := filesBelow(corpus)
	if err != nil {
		return nil, fmt.Errorf("harvest: fuzz corpus: %w", err)
	}
	if cache != "" {
		cached, cacheErr := filesBelow(cache)
		if cacheErr != nil {
			return nil, fmt.Errorf("harvest: fuzz cache: %w", cacheErr)
		}
		paths = append(paths, cached...)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, errors.New("harvest: fuzz corpus contains no seeds")
	}
	return paths, nil
}

func filesBelow(root string) ([]string, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	var paths []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("seed %s is not a regular file", path)
		}
		paths = append(paths, path)
		if len(paths) > maxFuzzCorpusFiles {
			return fmt.Errorf("corpus has more than %d files", maxFuzzCorpusFiles)
		}
		return nil
	})
	return paths, err
}

type fuzzSeed struct {
	data                    []byte
	size, blocks, subBlocks uint8
}

func readFuzzSeed(path string) (fuzzSeed, error) {
	file, err := os.Open(path)
	if err != nil {
		return fuzzSeed{}, fmt.Errorf("harvest: open fuzz seed %s: %w", path, err)
	}
	content, readErr := io.ReadAll(io.LimitReader(file, maxFuzzCorpusBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return fuzzSeed{}, fmt.Errorf("harvest: read fuzz seed %s: %w", path, readErr)
	}
	if closeErr != nil {
		return fuzzSeed{}, fmt.Errorf("harvest: close fuzz seed %s: %w", path, closeErr)
	}
	if len(content) > maxFuzzCorpusBytes {
		return fuzzSeed{}, fmt.Errorf("harvest: fuzz seed %s exceeds %d bytes", path, maxFuzzCorpusBytes)
	}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	lines := make([]string, 0, 5)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fuzzSeed{}, fmt.Errorf("harvest: scan fuzz seed %s: %w", path, err)
	}
	if len(lines) != 5 || lines[0] != "go test fuzz v1" {
		return fuzzSeed{}, fmt.Errorf("harvest: fuzz seed %s has invalid four-argument envelope", path)
	}
	data, err := parseByteSlice(lines[1])
	if err != nil {
		return fuzzSeed{}, fmt.Errorf("harvest: fuzz seed %s argument 0: %w", path, err)
	}
	values := [3]uint8{}
	for index := range values {
		values[index], err = parseUint8(lines[index+2])
		if err != nil {
			return fuzzSeed{}, fmt.Errorf("harvest: fuzz seed %s argument %d: %w", path, index+1, err)
		}
	}
	return fuzzSeed{data: data, size: values[0], blocks: values[1], subBlocks: values[2]}, nil
}

func parseByteSlice(value string) ([]byte, error) {
	if !strings.HasPrefix(value, "[]byte(") || !strings.HasSuffix(value, ")") {
		return nil, errors.New("want []byte quoted literal")
	}
	decoded, err := strconv.Unquote(strings.TrimSuffix(strings.TrimPrefix(value, "[]byte("), ")"))
	if err != nil {
		return nil, fmt.Errorf("invalid byte literal: %w", err)
	}
	return []byte(decoded), nil
}

func parseUint8(value string) (uint8, error) {
	if !strings.HasPrefix(value, "uint8(") || !strings.HasSuffix(value, ")") {
		return 0, errors.New("want uint8 decimal literal")
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(value, "uint8("), ")")
	parsed, err := strconv.ParseUint(raw, 10, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid uint8 literal: %w", err)
	}
	return uint8(parsed), nil
}
