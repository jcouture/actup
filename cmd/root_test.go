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

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRootCommandPrintsReferencesGroupedByFile(t *testing.T) {
	root := repository(t)
	writeWorkflow(t, root, ".github/workflows/ci.yml", `jobs:
  test:
    steps:
      - uses: actions/checkout@v4
      - uses: ./.github/actions/build
      - uses: actions/setup-go@v5
`)
	writeWorkflow(t, root, ".github/workflows/release.yml", `jobs:
  release:
    uses: goreleaser/goreleaser-action@v6.3.0
`)

	got := executeIn(t, root)
	want := "min-release-age: 24h\n\n" +
		".github/workflows/ci.yml\n" +
		"  actions/checkout@v4\n" +
		"  actions/setup-go@v5\n\n" +
		".github/workflows/release.yml\n" +
		"  goreleaser/goreleaser-action@v6.3.0\n"
	if got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRootCommandWithoutReferences(t *testing.T) {
	root := repository(t)
	writeWorkflow(t, root, ".github/workflows/ci.yml", "steps:\n  - uses: docker://alpine:latest\n")

	if got, want := executeIn(t, root), "min-release-age: 24h\n\nAll GitHub Actions are current.\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRootCommandLoadsConfiguration(t *testing.T) {
	root := repository(t)
	if err := os.WriteFile(filepath.Join(root, ".actup.toml"), []byte("min-release-age = \"3d\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if got, want := executeIn(t, root), "min-release-age: 72h\n\nAll GitHub Actions are current.\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRootCommandConfigFlagOverridesDefault(t *testing.T) {
	root := repository(t)
	if err := os.WriteFile(filepath.Join(root, ".actup.toml"), []byte("min-release-age = \"3d\"\n"), 0o644); err != nil {
		t.Fatalf("write default config: %v", err)
	}
	explicit := filepath.Join(root, "other.toml")
	if err := os.WriteFile(explicit, []byte("min-release-age = \"30m\"\n"), 0o644); err != nil {
		t.Fatalf("write explicit config: %v", err)
	}

	got, err := executeArgsIn(t, root, "--config", explicit)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if want := "min-release-age: 30m\n\nAll GitHub Actions are current.\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRootCommandMissingExplicitConfigFails(t *testing.T) {
	root := repository(t)
	if _, err := executeArgsIn(t, root, "--config", filepath.Join(root, "missing.toml")); err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
}

func repository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("create .git directory: %v", err)
	}
	return root
}

func writeWorkflow(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create workflow directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
}

func executeIn(t *testing.T, root string) string {
	t.Helper()
	output, err := executeArgsIn(t, root)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	return output
}

func executeArgsIn(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(root)
	var output bytes.Buffer
	command := newRootCommand("test")
	command.SetArgs(args)
	command.SetOut(&output)
	err := command.Execute()
	return output.String(), err
}
