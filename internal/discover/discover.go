// Copyright 2026 Jean-Philippe Couture
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// Package discover locates a Git repository root and its GitHub Actions files.
package discover

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

var errNotRepository = errors.New("not inside a Git repository")

// RepositoryRoot finds the Git repository containing the current directory.
func RepositoryRoot() (string, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get current working directory: %w", err)
	}

	return repositoryRoot(workingDirectory)
}

func repositoryRoot(start string) (string, error) {
	directory, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve starting directory: %w", err)
	}

	for {
		info, err := os.Lstat(filepath.Join(directory, ".git"))
		if err == nil && (info.IsDir() || info.Mode().IsRegular()) {
			return directory, nil
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("inspect .git entry in %q: %w", directory, err)
		}

		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errNotRepository
		}
		directory = parent
	}
}

// Files returns GitHub workflow and action definition paths relative to root.
func Files(root string) ([]string, error) {
	var files []string

	for _, name := range []string{"action.yml", "action.yaml"} {
		path := filepath.Join(root, name)
		regular, err := isRegularFile(path)
		if err != nil {
			return nil, err
		}
		if regular {
			files = append(files, name)
		}
	}

	github := filepath.Join(root, ".github")
	githubDirectory, err := isDirectory(github)
	if err != nil {
		return nil, err
	}
	if githubDirectory {
		workflowFiles, err := filesInWorkflowDirectory(root, github)
		if err != nil {
			return nil, err
		}
		files = append(files, workflowFiles...)

		actionFiles, err := filesInActionsDirectory(root, github)
		if err != nil {
			return nil, err
		}
		files = append(files, actionFiles...)
	}

	sort.Strings(files)
	return files, nil
}

func filesInWorkflowDirectory(root, github string) ([]string, error) {
	directory := filepath.Join(github, "workflows")
	exists, err := isDirectory(directory)
	if err != nil || !exists {
		return nil, err
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read workflow directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			continue
		}
		if extension := filepath.Ext(entry.Name()); extension != ".yml" && extension != ".yaml" {
			continue
		}

		regular, err := isRegularFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		if regular {
			relative, err := relativePath(root, filepath.Join(directory, entry.Name()))
			if err != nil {
				return nil, err
			}
			files = append(files, relative)
		}
	}
	return files, nil
}

func filesInActionsDirectory(root, github string) ([]string, error) {
	directory := filepath.Join(github, "actions")
	exists, err := isDirectory(directory)
	if err != nil || !exists {
		return nil, err
	}

	var files []string
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			if path != directory && entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "action.yml" && entry.Name() != "action.yaml" {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			relative, err := relativePath(root, path)
			if err != nil {
				return err
			}
			files = append(files, relative)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk actions directory: %w", err)
	}
	return files, nil
}

func isDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %q: %w", path, err)
	}
	return info.IsDir(), nil
}

func isRegularFile(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %q: %w", path, err)
	}
	return info.Mode().IsRegular(), nil
}

func relativePath(root, path string) (string, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", fmt.Errorf("make %q relative to repository root: %w", path, err)
	}
	return filepath.ToSlash(relative), nil
}
