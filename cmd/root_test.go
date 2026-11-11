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
const cacheSHA = "abcdef0123456789abcdef0123456789abcdef01"

func fixtureServer(t *testing.T, failCache bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if failCache && strings.Contains(p, "/contents/") {
			http.Error(w, "failure", 500)
			return
		}
		switch {
		case p == "/repos/foo/build":
			fmt.Fprint(w, `{"id":11,"owner":{"id":10}}`)
		case p == "/repos/actions/cache":
			fmt.Fprint(w, `{"id":21,"owner":{"id":20}}`)
		case p == "/repos/foo/build/releases":
			fmt.Fprint(w, `[{"tag_name":"v2.0.0","published_at":"2020-01-01T00:00:00Z"}]`)
		case p == "/repos/actions/cache/releases":
			fmt.Fprint(w, `[{"tag_name":"v4.0.0","published_at":"2020-01-01T00:00:00Z"}]`)
		case p == "/repos/foo/build/git/ref/tags/v2.0.0":
			fmt.Fprintf(w, `{"object":{"type":"commit","sha":%q}}`, sha)
		case p == "/repos/actions/cache/git/ref/tags/v3":
			fmt.Fprintf(w, `{"object":{"type":"commit","sha":%q}}`, cacheSHA)
		case p == "/repos/foo/build/contents/action.yml":
			if r.URL.Query().Get("ref") != sha {
				http.Error(w, "wrong commit", 400)
				return
			}
			fmt.Fprint(w, `{"encoding":"base64","content":"cnVuczoKICB1c2luZzogY29tcG9zaXRlCiAgc3RlcHM6CiAgICAtIHVzZXM6IGFjdGlvbnMvY2FjaGVAdjMK"}`)
		case p == "/repos/actions/cache/contents/action.yml":
			if r.URL.Query().Get("ref") != cacheSHA {
				http.Error(w, "wrong commit", 400)
				return
			}
			fmt.Fprint(w, `{"encoding":"base64","content":"cnVuczoKICB1c2luZzogbm9kZTIwCg=="}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}
func setup(t *testing.T, lock bool, contents string) (string, string) {
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
	if err := os.WriteFile(file, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	if lock {
		data := []byte("version: v0.0.3\nworkflows:\n  .github/workflows/ci.yml: []\ndependencies: {}\n")
		if err := os.WriteFile(filepath.Join(dir, "actions.lock"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
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
	command := newRootCommand("test")
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetArgs(args)
	err = command.Execute()
	return out.String(), err
}
func useServer(t *testing.T, s *httptest.Server) {
	old := githubAPIURL
	githubAPIURL = s.URL
	t.Cleanup(func() { githubAPIURL = old })
}
func TestLegacyIntegration(t *testing.T) {
	root, file := setup(t, false, "jobs:\n  build:\n    steps:\n      - uses: foo/build@v1 # keep\n")
	useServer(t, fixtureServer(t, false))
	out, err := run(t, root)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	want := "jobs:\n  build:\n    steps:\n      - uses: foo/build@" + sha + " # keep\n"
	if string(got) != want {
		t.Fatalf("file=%q want=%q", got, want)
	}
	if !strings.Contains(out, "major version change") {
		t.Fatalf("output=%q", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".github", "workflows", "actions.lock")); !os.IsNotExist(err) {
		t.Fatalf("lockfile created: %v", err)
	}
}
func TestLockfileIntegration(t *testing.T) {
	root, file := setup(t, true, "jobs:\n  build:\n    steps:\n      - uses: foo/build@v1\n")
	useServer(t, fixtureServer(t, false))
	out, err := run(t, root)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	if !strings.Contains(string(got), "foo/build@v2.0.0") {
		t.Fatalf("workflow=%q", got)
	}
	lock, _ := os.ReadFile(filepath.Join(root, ".github", "workflows", "actions.lock"))
	for _, needle := range []string{"foo/build@v2.0.0", "actions/cache@v3", "sha1-" + sha, "sha1-" + cacheSHA, "owner_id: 20", "repo_id: 21", "uses:"} {
		if !strings.Contains(string(lock), needle) {
			t.Errorf("lock lacks %q: %s", needle, lock)
		}
	}
	if !strings.Contains(out, "lockfile") {
		t.Fatalf("output=%q", out)
	}
	out, err = run(t, root, "--check")
	if err != nil {
		t.Fatalf("current check: %v, %s", err, out)
	}
	out, err = run(t, root)
	if err != nil || out != "All GitHub Actions dependencies and the workflow lockfile are current.\n" {
		t.Fatalf("no-change output=%q, %v", out, err)
	}
}
func TestCheckDryRunAndFailedResolution(t *testing.T) {
	for _, args := range [][]string{{"--check"}, {"--dry-run"}} {
		root, file := setup(t, false, "jobs:\n  build:\n    steps:\n      - uses: foo/build@v1\n")
		useServer(t, fixtureServer(t, false))
		before, _ := os.ReadFile(file)
		_, err := run(t, root, args...)
		if args[0] == "--check" && ExitCode(err) != 1 {
			t.Fatalf("check err=%v", err)
		}
		if args[0] == "--dry-run" && err != nil {
			t.Fatal(err)
		}
		after, _ := os.ReadFile(file)
		if !bytes.Equal(before, after) {
			t.Fatalf("file changed in %s", args[0])
		}
	}
	root, file := setup(t, true, "jobs:\n  build:\n    steps:\n      - uses: foo/build@v1\n")
	useServer(t, fixtureServer(t, true))
	before, _ := os.ReadFile(file)
	oldLock, _ := os.ReadFile(filepath.Join(root, ".github", "workflows", "actions.lock"))
	_, err := run(t, root)
	if err == nil {
		t.Fatal("expected graph failure")
	}
	after, _ := os.ReadFile(file)
	newLock, _ := os.ReadFile(filepath.Join(root, ".github", "workflows", "actions.lock"))
	if !bytes.Equal(before, after) || !bytes.Equal(oldLock, newLock) {
		t.Fatal("failure wrote files")
	}
}
func TestUsage(t *testing.T) {
	root, _ := setup(t, false, "")
	for _, args := range [][]string{{"--check", "--dry-run"}, {"other"}, {"--yes"}} {
		_, err := run(t, root, args...)
		if err == nil {
			t.Fatalf("%v accepted", args)
		}
	}
}

func TestSecurityRejectsIdentityReplacementAndMovedTag(t *testing.T) {
	for _, tc := range []struct {
		name, commit string
		repoID       int64
		want         string
	}{
		{"identity", sha, 999, "repository identity changed"},
		{"moved tag", cacheSHA, 11, "tag v2.0.0 for foo/build moved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, file := setup(t, true, "jobs:\n  build:\n    steps:\n      - uses: foo/build@v2.0.0\n")
			useServer(t, fixtureServer(t, false))
			lockPath := filepath.Join(root, ".github", "workflows", "actions.lock")
			body := fmt.Sprintf("version: v0.0.3\nworkflows:\n  .github/workflows/ci.yml:\n    - foo/build@v2.0.0\ndependencies:\n  foo/build@v2.0.0:\n    ref: v2.0.0\n    commit: sha1-%s\n    owner_id: 10\n    repo_id: %d\n", tc.commit, tc.repoID)
			if err := os.WriteFile(lockPath, []byte(body), 0644); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(file)
			_, err := run(t, root)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v", err)
			}
			after, _ := os.ReadFile(file)
			lockAfter, _ := os.ReadFile(lockPath)
			if !bytes.Equal(before, after) || string(lockAfter) != body {
				t.Fatal("security error changed files")
			}
		})
	}
}
func TestLocalWorkspaceActionCoverage(t *testing.T) {
	for _, tc := range []struct {
		name, metadata string
		blocked        bool
	}{
		{"leaf", "runs:\n  using: node20\n", false},
		{"remote", "runs:\n  using: composite\n  steps:\n    - uses: actions/cache@v3\n", true},
		{"nested", "runs:\n  using: composite\n  steps:\n    - uses: ./.github/actions/child\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := setup(t, true, "jobs:\n  build:\n    steps:\n      - uses: ./.github/actions/build\n")
			actionDir := filepath.Join(root, ".github", "actions", "build")
			os.MkdirAll(actionDir, 0755)
			os.WriteFile(filepath.Join(actionDir, "action.yml"), []byte(tc.metadata), 0644)
			childDir := filepath.Join(root, ".github", "actions", "child")
			os.MkdirAll(childDir, 0755)
			os.WriteFile(filepath.Join(childDir, "action.yml"), []byte("runs:\n  using: composite\n  steps:\n    - uses: actions/cache@v3\n"), 0644)
			_, err := run(t, root)
			if tc.blocked && (err == nil || !strings.Contains(err.Error(), "cannot be safely represented")) {
				t.Fatalf("error=%v", err)
			}
			if !tc.blocked && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReusableWorkflowPinnedOutsideLockfile(t *testing.T) {
	root, file := setup(t, true, "jobs:\n  call:\n    uses: foo/build/.github/workflows/test.yml@v1\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/foo/build/releases":
			fmt.Fprint(w, `[{"tag_name":"v2.0.0","published_at":"2020-01-01T00:00:00Z"}]`)
		case "/repos/foo/build/git/ref/tags/v2.0.0":
			fmt.Fprintf(w, `{"object":{"type":"commit","sha":%q}}`, sha)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	useServer(t, server)
	_, err := run(t, root)
	if err != nil {
		t.Fatal(err)
	}
	workflow, _ := os.ReadFile(file)
	if !strings.Contains(string(workflow), "foo/build/.github/workflows/test.yml@"+sha+" # v2.0.0") {
		t.Fatalf("workflow=%q", workflow)
	}
	locked, _ := os.ReadFile(filepath.Join(root, ".github", "workflows", "actions.lock"))
	if strings.Contains(string(locked), "foo/build@") {
		t.Fatalf("reusable workflow entered lockfile: %s", locked)
	}
}

func TestWriteTransactionRestoresPreviousFile(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.yml")
	if err := os.WriteFile(first, []byte("original\r\n"), 0640); err != nil {
		t.Fatal(err)
	}
	err := writeTransaction([]fileUpdate{{path: first, contents: []byte("changed\n"), original: []byte("original\r\n")}, {path: filepath.Join(dir, "missing.lock"), contents: []byte("new"), original: []byte("old")}})
	if err == nil {
		t.Fatal("expected write failure")
	}
	got, readErr := os.ReadFile(first)
	if readErr != nil || string(got) != "original\r\n" {
		t.Fatalf("restored file=%q, %v", got, readErr)
	}
	info, statErr := os.Stat(first)
	if statErr != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("mode=%v, %v", info.Mode(), statErr)
	}
}

func TestCheckDetectsStaleLockWithoutReformattingCurrentLock(t *testing.T) {
	root, file := setup(t, true, "jobs:\n  build:\n    steps:\n      - uses: foo/build@v2.0.0\n")
	useServer(t, fixtureServer(t, false))
	lockPath := filepath.Join(root, ".github", "workflows", "actions.lock")
	before, _ := os.ReadFile(lockPath)
	_, err := run(t, root, "--check")
	if ExitCode(err) != 1 {
		t.Fatalf("stale check=%v", err)
	}
	after, _ := os.ReadFile(lockPath)
	if !bytes.Equal(before, after) {
		t.Fatal("check wrote lockfile")
	}
	_, err = run(t, root)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(lockPath)
	// A valid alternate spelling of the same semantic model stays untouched.
	alternate := bytes.ReplaceAll(current, []byte("version: \"v0.0.3\""), []byte("version: 'v0.0.3'"))
	if err := os.WriteFile(lockPath, alternate, 0644); err != nil {
		t.Fatal(err)
	}
	output, err := run(t, root)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, _ := os.ReadFile(lockPath)
	if !bytes.Equal(alternate, unchanged) {
		t.Fatal("semantic no-op reformatted lockfile")
	}
	if output != "All GitHub Actions dependencies and the workflow lockfile are current.\n" {
		t.Fatalf("output=%q", output)
	}
	workflow, _ := os.ReadFile(file)
	if !strings.Contains(string(workflow), "foo/build@v2.0.0") {
		t.Fatalf("workflow=%q", workflow)
	}
}
