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
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRootCommandPrintsReferencesGroupedByFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		tags := map[string]string{
			"actions/checkout":             "v5.0.0",
			"actions/setup-go":             "v6.1.0",
			"goreleaser/goreleaser-action": "v7.0.0",
		}
		for repository, tag := range tags {
			switch request.URL.Path {
			case "/repos/" + repository + "/releases":
				fmt.Fprintf(response, `[{"tag_name":%q,"published_at":"2020-01-01T00:00:00Z"}]`, tag)
				return
			case "/repos/" + repository + "/git/ref/tags/" + tag:
				fmt.Fprint(response, `{"object":{"type":"commit","sha":"0123456789abcdef0123456789abcdef01234567"}}`)
				return
			}
		}
		http.NotFound(response, request)
	}))
	defer server.Close()
	oldAPIURL := githubAPIURL
	githubAPIURL = server.URL
	t.Cleanup(func() { githubAPIURL = oldAPIURL })

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
	want := ".github/workflows/ci.yml\n\n" +
		"  UPDATE  actions/checkout\n" +
		"          v4 -> v5.0.0\n" +
		"          warning: major version change\n\n" +
		"  UPDATE  actions/setup-go\n" +
		"          v5 -> v6.1.0\n" +
		"          warning: major version change\n\n" +
		".github/workflows/release.yml\n\n" +
		"  UPDATE  goreleaser/goreleaser-action\n" +
		"          v6.3.0 -> v7.0.0\n" +
		"          warning: major version change\n\n" +
		"Updated 3 references in 2 files.\n" +
		"3 major version updates.\n"
	if got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	contents, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wantFile := "jobs:\n  test:\n    steps:\n" +
		"      - uses: actions/checkout@0123456789abcdef0123456789abcdef01234567 # v5.0.0\n" +
		"      - uses: ./.github/actions/build\n" +
		"      - uses: actions/setup-go@0123456789abcdef0123456789abcdef01234567 # v6.1.0\n"
	if string(contents) != wantFile {
		t.Errorf("workflow = %q, want %q", contents, wantFile)
	}
}

func TestRootCommandDryRunAndCheckDoNotWrite(t *testing.T) {
	server := currentReleaseServer(t)
	defer server.Close()
	oldAPIURL := githubAPIURL
	githubAPIURL = server.URL
	t.Cleanup(func() { githubAPIURL = oldAPIURL })

	for _, test := range []struct {
		name      string
		flag      string
		wantError bool
		summary   string
	}{
		{name: "dry run", flag: "--dry-run", summary: "1 reference would be updated in 1 file.\n"},
		{name: "check", flag: "--check", wantError: true, summary: "1 reference requires updates in 1 file.\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := repository(t)
			original := "steps:\n  - uses: actions/checkout@v4\n"
			writeWorkflow(t, root, ".github/workflows/ci.yml", original)
			got, err := executeArgsIn(t, root, test.flag)
			if (err != nil) != test.wantError {
				t.Fatalf("Execute() error = %v", err)
			}
			if test.wantError && !IsCheckFailure(err) {
				t.Fatalf("error = %v, want check failure", err)
			}
			if !bytes.Contains([]byte(got), []byte(test.summary)) {
				t.Errorf("output = %q, want to contain %q", got, test.summary)
			}
			contents, readErr := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
			if readErr != nil || string(contents) != original {
				t.Errorf("workflow = %q, %v; want unchanged", contents, readErr)
			}
		})
	}
}

func TestRootCommandRejectsConflictingModes(t *testing.T) {
	root := repository(t)
	_, err := executeArgsIn(t, root, "--check", "--dry-run")
	if err == nil || err.Error() != "--check and --dry-run are mutually exclusive" {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestRootCommandFindsRepositoryFromNestedDirectory(t *testing.T) {
	root := repository(t)
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := executeIn(t, nested); got != "All GitHub Actions are current.\n" {
		t.Errorf("output = %q", got)
	}
}

func TestRootCommandWithoutReferences(t *testing.T) {
	root := repository(t)
	writeWorkflow(t, root, ".github/workflows/ci.yml", "steps:\n  - uses: docker://alpine:latest\n")

	if got, want := executeIn(t, root), "All GitHub Actions are current.\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRootCommandLoadsConfiguration(t *testing.T) {
	root := repository(t)
	if err := os.WriteFile(filepath.Join(root, ".actup.toml"), []byte("min-release-age = \"3d\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if got, want := executeIn(t, root), "All GitHub Actions are current.\n"; got != want {
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
	if want := "All GitHub Actions are current.\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRootCommandMissingExplicitConfigFails(t *testing.T) {
	root := repository(t)
	if _, err := executeArgsIn(t, root, "--config", filepath.Join(root, "missing.toml")); err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
}

func TestRootCommandIgnoresActionsFromFlagAndConfig(t *testing.T) {
	requests := make(chan string, 10)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests <- request.URL.Path
		switch request.URL.Path {
		case "/repos/goreleaser/goreleaser-action/releases":
			fmt.Fprint(response, `[{"tag_name":"v7.0.0","published_at":"2020-01-01T00:00:00Z"}]`)
		case "/repos/goreleaser/goreleaser-action/git/ref/tags/v7.0.0":
			fmt.Fprint(response, `{"object":{"type":"commit","sha":"0123456789abcdef0123456789abcdef01234567"}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	oldAPIURL := githubAPIURL
	githubAPIURL = server.URL
	t.Cleanup(func() { githubAPIURL = oldAPIURL })

	root := repository(t)
	if err := os.WriteFile(filepath.Join(root, ".actup.toml"), []byte("ignore = [\"actions/checkout\"]\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	original := "steps:\n" +
		"  - uses: actions/checkout@v4\n" +
		"  - uses: github/codeql-action/analyze@v3\n" +
		"  - uses: goreleaser/goreleaser-action@v6.3.0\n"
	writeWorkflow(t, root, ".github/workflows/ci.yml", original)

	got, err := executeArgsIn(t, root, "--ignore", "github/*")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := ".github/workflows/ci.yml\n\n" +
		"  UPDATE  goreleaser/goreleaser-action\n" +
		"          v6.3.0 -> v7.0.0\n" +
		"          warning: major version change\n\n" +
		"Updated 1 reference in 1 file.\n" +
		"1 major version update.\n"
	if got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	contents, readErr := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	wantFile := "steps:\n" +
		"  - uses: actions/checkout@v4\n" +
		"  - uses: github/codeql-action/analyze@v3\n" +
		"  - uses: goreleaser/goreleaser-action@0123456789abcdef0123456789abcdef01234567 # v7.0.0\n"
	if string(contents) != wantFile {
		t.Errorf("workflow = %q, want %q", contents, wantFile)
	}
	close(requests)
	for request := range requests {
		if request != "/repos/goreleaser/goreleaser-action/releases" &&
			request != "/repos/goreleaser/goreleaser-action/git/ref/tags/v7.0.0" {
			t.Errorf("unexpected API request %q", request)
		}
	}
}

func TestRootCommandIgnoreInReadOnlyModes(t *testing.T) {
	for _, test := range []struct {
		name      string
		flag      string
		wantError bool
		summary   string
	}{
		{name: "dry run", flag: "--dry-run", summary: "1 reference would be updated in 1 file.\n"},
		{name: "check", flag: "--check", wantError: true, summary: "1 reference requires updates in 1 file.\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/repos/goreleaser/goreleaser-action/releases":
					fmt.Fprint(response, `[{"tag_name":"v7.0.0","published_at":"2020-01-01T00:00:00Z"}]`)
				case "/repos/goreleaser/goreleaser-action/git/ref/tags/v7.0.0":
					fmt.Fprint(response, `{"object":{"type":"commit","sha":"0123456789abcdef0123456789abcdef01234567"}}`)
				default:
					http.NotFound(response, request)
				}
			}))
			defer server.Close()
			oldAPIURL := githubAPIURL
			githubAPIURL = server.URL
			t.Cleanup(func() { githubAPIURL = oldAPIURL })

			root := repository(t)
			original := "steps:\n" +
				"  - uses: actions/checkout@v4\n" +
				"  - uses: goreleaser/goreleaser-action@v6.3.0\n"
			writeWorkflow(t, root, ".github/workflows/ci.yml", original)
			got, err := executeArgsIn(t, root, test.flag, "--ignore", "actions/*")
			if (err != nil) != test.wantError {
				t.Fatalf("Execute() error = %v", err)
			}
			if test.wantError && !IsCheckFailure(err) {
				t.Fatalf("error = %v, want check failure", err)
			}
			if !bytes.Contains([]byte(got), []byte(test.summary)) {
				t.Errorf("output = %q, want to contain %q", got, test.summary)
			}
			if bytes.Contains([]byte(got), []byte("actions/checkout")) {
				t.Errorf("output = %q, want ignored action omitted", got)
			}
			contents, readErr := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
			if readErr != nil || string(contents) != original {
				t.Errorf("workflow = %q, %v; want unchanged", contents, readErr)
			}
		})
	}
}

func TestRootCommandRejectsInvalidIgnorePattern(t *testing.T) {
	root := repository(t)
	_, err := executeArgsIn(t, root, "--ignore", "actions/[invalid")
	if err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
}

func TestFilterIgnoredRejectsInvalidPatternWithoutOccurrences(t *testing.T) {
	if _, err := filterIgnored(nil, []string{"actions/[invalid"}); err == nil {
		t.Fatal("filterIgnored() error = nil, want error")
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

func currentReleaseServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/actions/checkout/releases":
			fmt.Fprint(response, `[{"tag_name":"v5.0.0","published_at":"2020-01-01T00:00:00Z"}]`)
		case "/repos/actions/checkout/git/ref/tags/v5.0.0":
			fmt.Fprint(response, `{"object":{"type":"commit","sha":"0123456789abcdef0123456789abcdef01234567"}}`)
		default:
			http.NotFound(response, request)
		}
	}))
}
