package cleanup

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// removableDirs contains generated dependency/build/cache directories that
// NodeMop is allowed to remove during a safe cleanup. Source files are never
// part of this operation.
var removableDirs = map[string]bool{
	"node_modules": true,
	".next":        true,
	"dist":         true,
	"build":        true,
	"coverage":     true,
	".cache":       true,
	".turbo":       true,
}

type Result struct {
	ProjectPath string
	Removed     []string
	FreedBytes  int64
}

func IsRemovableDir(name string) bool { return removableDirs[name] }

// Candidates returns generated directories that are safe to remove. Nested
// candidates are collapsed so we never try to remove both node_modules and a
// cache directory living inside the same node_modules tree.
func Candidates(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("project path is not a directory")
	}

	var found []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path == root {
			return nil
		}
		if d.Name() == ".git" {
			return filepath.SkipDir
		}
		if removableDirs[d.Name()] {
			found = append(found, path)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(found)
	return found, nil
}

func ReclaimableSize(root string) int64 {
	candidates, err := Candidates(root)
	if err != nil {
		return 0
	}
	var total int64
	for _, path := range candidates {
		total += directorySize(path)
	}
	return total
}

func Clean(root string) (Result, error) {
	result := Result{ProjectPath: root}
	candidates, err := Candidates(root)
	if err != nil {
		return result, err
	}

	for _, path := range candidates {
		size := directorySize(path)
		if err := os.RemoveAll(path); err != nil {
			return result, err
		}
		result.Removed = append(result.Removed, path)
		result.FreedBytes += size
	}
	return result, nil
}

func directorySize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
