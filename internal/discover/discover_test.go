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

package discover

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRepositoryRoot(t *testing.T) {
	tests := []struct {
		name    string
		gitFile bool
	}{
		{name: ".git directory"},
		{name: ".git file", gitFile: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			gitPath := filepath.Join(root, ".git")
			if test.gitFile {
				if err := os.WriteFile(gitPath, []byte("gitdir: ../worktrees/example\n"), 0o644); err != nil {
					t.Fatalf("create .git file: %v", err)
				}
			} else if err := os.Mkdir(gitPath, 0o755); err != nil {
				t.Fatalf("create .git directory: %v", err)
			}

			nested := filepath.Join(root, "one", "two")
			if err := os.MkdirAll(nested, 0o755); err != nil {
				t.Fatalf("create nested directory: %v", err)
			}

			got, err := repositoryRoot(nested)
			if err != nil {
				t.Fatalf("repositoryRoot() error = %v", err)
			}
			if got != root {
				t.Errorf("repositoryRoot() = %q, want %q", got, root)
			}
		})
	}
}

func TestRepositoryRootNotFound(t *testing.T) {
	_, err := repositoryRoot(t.TempDir())
	if !errors.Is(err, errNotRepository) {
		t.Fatalf("repositoryRoot() error = %v, want %v", err, errNotRepository)
	}
}

func TestRepositoryRootFromRejectsInvalidScanPath(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		if _, err := RepositoryRootFrom(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Fatal("RepositoryRootFrom() error = nil, want error")
		}
	})

	t.Run("file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "workflow.yml")
		writeFile(t, file)
		if _, err := RepositoryRootFrom(file); err == nil {
			t.Fatal("RepositoryRootFrom() error = nil, want error")
		}
	})
}

func TestFiles(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		".github/workflows/ci.yml",
		".github/workflows/release.yaml",
		".github/actions/foo/action.yml",
		".github/actions/foo/nested/action.yaml",
		"action.yml",
		"README.md",
		".github/dependabot.yml",
		".github/workflows/nested/ignored.yml",
		".github/actions/action.txt",
		"somewhere/action.yaml",
		".github/actions/tool/.git/action.yml",
	}
	for _, path := range paths {
		writeFile(t, filepath.Join(root, filepath.FromSlash(path)))
	}

	got, err := Files(root)
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	want := []string{
		".github/actions/foo/action.yml",
		".github/actions/foo/nested/action.yaml",
		".github/workflows/ci.yml",
		".github/workflows/release.yaml",
		"action.yml",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Files() = %v, want %v", got, want)
	}
}

func TestFilesEmptyRepository(t *testing.T) {
	got, err := Files(t.TempDir())
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Files() = %v, want empty list", got)
	}
}

func TestFilesIgnoreSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.yml")
	writeFile(t, target)

	symlinks := []string{
		"action.yml",
		".github/workflows/linked.yml",
		".github/actions/linked/action.yaml",
	}
	for _, path := range symlinks {
		link := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatalf("create parent directory: %v", err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("create symlink: %v", err)
		}
	}

	got, err := Files(root)
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Files() = %v, want empty list", got)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("test\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
