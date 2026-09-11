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

package update

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jcouture/actup/internal/action"
	githubapi "github.com/jcouture/actup/internal/github"
	"github.com/jcouture/actup/internal/resolver"
)

const (
	targetSHA = "08c6903cd8c0fde910a37f88322edcfb5dd907a8"
	oldSHA    = "1234567890abcdef1234567890abcdef12345678"
)

func TestPreparePreservesBytesAroundEdits(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		current string
		comment string
		changed bool
		want    string
	}{
		{name: "basic", input: "uses: actions/checkout@v4\n", current: "v4", changed: true, want: "uses: actions/checkout@" + targetSHA + " # v5.0.0\n"},
		{name: "indentation and list", input: "      - uses: actions/checkout@v4\n", current: "v4", changed: true, want: "      - uses: actions/checkout@" + targetSHA + " # v5.0.0\n"},
		{name: "human comment", input: "uses: actions/checkout@v4  # checkout code  \n", current: "v4", comment: "# checkout code  ", changed: true, want: "uses: actions/checkout@" + targetSHA + "  # checkout code  \n"},
		{name: "owned comment", input: "uses: actions/checkout@" + oldSHA + "  # v4.2.2  \n", current: oldSHA, comment: "# v4.2.2  ", changed: true, want: "uses: actions/checkout@" + targetSHA + "  # v5.0.0  \n"},
		{name: "current SHA", input: "uses: actions/checkout@" + targetSHA + " # custom\n", current: targetSHA, comment: "# custom", want: "uses: actions/checkout@" + targetSHA + " # custom\n"},
		{name: "CRLF", input: "steps:\r\n  - uses: actions/checkout@v4\r\n", current: "v4", changed: true, want: "steps:\r\n  - uses: actions/checkout@" + targetSHA + " # v5.0.0\r\n"},
		{name: "no final newline", input: "uses: actions/checkout@v4", current: "v4", changed: true, want: "uses: actions/checkout@" + targetSHA + " # v5.0.0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			line := 1
			if test.name == "CRLF" {
				line = 2
			}
			result := checkoutResult(line, test.current, test.comment, test.changed)
			got, err := Prepare([]byte(test.input), []resolver.Result{result})
			if err != nil {
				t.Fatalf("Prepare() error = %v", err)
			}
			if !bytes.Equal(got, []byte(test.want)) {
				t.Errorf("Prepare() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPrepareMultipleAndRepeatedReferences(t *testing.T) {
	input := "- uses: actions/checkout@v4\n- uses: owner/tool@v1 # keep\n- uses: actions/checkout@v4\n"
	results := []resolver.Result{
		checkoutResult(1, "v4", "", true),
		{
			Occurrence: resolver.Occurrence{Use: action.Use{LineNumber: 2, Comment: "# keep", Reference: action.Reference{Owner: "owner", Repository: "tool", Ref: "v1"}}},
			Target:     githubapi.Target{Tag: "v2.0.0", SHA: oldSHA, Major: 2}, Current: "v1", Changed: true, MajorChange: true,
		},
		checkoutResult(3, "v4", "", true),
	}
	want := "- uses: actions/checkout@" + targetSHA + " # v5.0.0\n" +
		"- uses: owner/tool@" + oldSHA + " # keep\n" +
		"- uses: actions/checkout@" + targetSHA + " # v5.0.0\n"
	got, err := Prepare([]byte(input), results)
	if err != nil || string(got) != want {
		t.Fatalf("Prepare() = %q, %v; want %q", got, err, want)
	}
}

func TestUnsupportedUsesFormsRemainUntouched(t *testing.T) {
	input := []byte("- uses: ./.github/actions/build\n- uses: docker://alpine:latest\n- uses: ${{ matrix.action }}\n")
	got, err := Prepare(input, nil)
	if err != nil || !bytes.Equal(got, input) {
		t.Fatalf("Prepare() = %q, %v", got, err)
	}
}

func TestWriteAtomicPreservesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflow.yml")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("new")); err != nil {
		t.Fatalf("WriteAtomic() error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "new" {
		t.Fatalf("file = %q, %v", contents, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, %v", info.Mode(), err)
	}
}

func checkoutResult(line int, current, comment string, changed bool) resolver.Result {
	return resolver.Result{
		Occurrence: resolver.Occurrence{Use: action.Use{
			LineNumber: line,
			Comment:    comment,
			Reference:  action.Reference{Owner: "actions", Repository: "checkout", Ref: current},
		}},
		Target:  githubapi.Target{Tag: "v5.0.0", SHA: targetSHA, Major: 5},
		Current: current,
		Changed: changed,
	}
}
