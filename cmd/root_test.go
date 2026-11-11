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
	"strings"
	"testing"
)

const sha = "0123456789abcdef0123456789abcdef01234567"

func setup(t *testing.T, lock bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "ci.yml")
	if err := os.WriteFile(file, []byte("- uses: actions/checkout@v4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if lock {
		if err := os.WriteFile(filepath.Join(dir, "actions.lock"), []byte("untouched"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/repos/actions/checkout/") {
			http.NotFound(w, r)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases"):
			fmt.Fprint(w, `[{"tag_name":"v5.0.0","published_at":"2020-01-01T00:00:00Z"}]`)
		case strings.HasSuffix(r.URL.Path, "/git/ref/tags/v5.0.0"):
			fmt.Fprintf(w, `{"object":{"type":"commit","sha":%q}}`, sha)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	old := githubAPIURL
	githubAPIURL = server.URL
	t.Cleanup(func() { githubAPIURL = old })
	return root, file
}
func run(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	cmd := newRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), err
}
func TestLegacyIntegration(t *testing.T) {
	root, file := setup(t, false)
	out, err := run(t, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	want := "- uses: actions/checkout@" + sha + " # v5.0.0\n"
	if string(got) != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
	if !strings.Contains(out, "warning: major version change") {
		t.Fatalf("output = %q", out)
	}
}
func TestLockfileModes(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		exit int
		want string
	}{{"normal", nil, 2, "workflow dependency lockfile detected"}, {"dry run", []string{"--dry-run"}, 0, "Workflow dependency lockfile detected."}, {"check", []string{"--check"}, 1, "requires updates"}} {
		t.Run(tc.name, func(t *testing.T) {
			root, file := setup(t, true)
			before, _ := os.ReadFile(file)
			out, err := run(t, root, tc.args...)
			if tc.exit == 0 && err != nil {
				t.Fatal(err)
			}
			if tc.exit != 0 && (err == nil || ExitCode(err) != tc.exit) {
				t.Fatalf("error = %v", err)
			}
			if !strings.Contains(out+fmt.Sprint(err), tc.want) {
				t.Fatalf("output = %q, err = %v", out, err)
			}
			after, _ := os.ReadFile(file)
			if !bytes.Equal(before, after) {
				t.Fatalf("workflow changed: %q", after)
			}
			lock, _ := os.ReadFile(filepath.Join(root, ".github", "workflows", "actions.lock"))
			if string(lock) != "untouched" {
				t.Fatalf("lockfile changed: %q", lock)
			}
		})
	}
}
func TestUsage(t *testing.T) {
	root, _ := setup(t, false)
	for _, args := range [][]string{{"--check", "--dry-run"}, {"other"}, {"--yes"}} {
		_, err := run(t, root, args...)
		if err == nil {
			t.Fatalf("%v accepted", args)
		}
	}
}
func TestNoFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, root)
	if err != nil || out != "No GitHub Actions files found.\n" {
		t.Fatalf("output %q, error %v", out, err)
	}
}

func TestLockfileCurrentNoOp(t *testing.T) {
	root, file := setup(t, true)
	if err := os.WriteFile(file, []byte("- uses: actions/checkout@v5.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, root)
	if err != nil || out != "All GitHub Actions are current.\n" {
		t.Fatalf("output = %q, error = %v", out, err)
	}
	contents, err := os.ReadFile(file)
	if err != nil || string(contents) != "- uses: actions/checkout@v5.0.0\n" {
		t.Fatalf("workflow = %q, error = %v", contents, err)
	}
}

func TestLockfileReusableWorkflowRequiresPin(t *testing.T) {
	root, file := setup(t, true)
	if err := os.WriteFile(file, []byte("jobs:\n  build:\n    uses: actions/checkout/.github/workflows/build.yml@v4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, root, "--dry-run")
	if err != nil || !strings.Contains(out, "reusable workflow: immutable SHA pinning required") {
		t.Fatalf("output = %q, error = %v", out, err)
	}
}

func TestResolutionFailureWritesNothing(t *testing.T) {
	root, file := setup(t, false)
	before := []byte("- uses: actions/checkout@v4\n- uses: missing/repository@v1\n")
	if err := os.WriteFile(file, before, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := run(t, root)
	if err == nil || !strings.Contains(err.Error(), "resolve missing/repository: GitHub API returned 404") {
		t.Fatalf("error = %v", err)
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("workflow = %q, error = %v", after, err)
	}
}
