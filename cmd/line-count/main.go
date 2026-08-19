// Command line-count reports the size of Go and TypeScript source files.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type result struct {
	path  string
	lines int
}

func main() {
	max := flag.Int("max", 0, "fail when a source file has more than this many lines (zero disables the limit)")
	flag.Parse()
	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}
	results, err := countTree(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	failed := false
	for _, item := range results {
		fmt.Printf("%4d %s\n", item.lines, item.path)
		failed = failed || (*max > 0 && item.lines > *max)
	}
	if failed {
		fmt.Fprintf(os.Stderr, "Go and TypeScript files must not exceed %d lines\n", *max)
		os.Exit(1)
	}
}

func countTree(root string) ([]result, error) {
	var results []result
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != root && ignoredDirectory(entry.Name()) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !sourceFile(path) {
			return nil
		}
		lines, err := countFile(path)
		if err != nil {
			return err
		}
		display, err := filepath.Rel(root, path)
		if err != nil {
			display = path
		}
		results = append(results, result{path: filepath.ToSlash(display), lines: lines})
		return nil
	})
	sort.Slice(results, func(i, j int) bool {
		if results[i].lines == results[j].lines {
			return results[i].path < results[j].path
		}
		return results[i].lines > results[j].lines
	})
	return results, err
}

func ignoredDirectory(name string) bool {
	return name == ".git" || name == ".tools" || name == "node_modules" || name == "dist" || name == ".cache"
}

func sourceFile(path string) bool {
	return strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".ts")
}

func countFile(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	lines := 0
	for scanner.Scan() {
		lines++
	}
	if err := scanner.Err(); err != nil {
		return 0, errors.New("count " + path + ": " + err.Error())
	}
	return lines, nil
}
