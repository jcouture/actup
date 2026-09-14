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

package output

import (
	"bytes"
	"testing"

	"github.com/jcouture/actup/internal/action"
	githubapi "github.com/jcouture/actup/internal/github"
	"github.com/jcouture/actup/internal/resolver"
)

func TestPrintOrdersAndClassifiesChanges(t *testing.T) {
	results := []resolver.Result{
		result(".github/workflows/ci.yml", 1, "actions", "setup-go", "v5", "v6.1.0", true),
		result(".github/workflows/ci.yml", 2, "actions", "checkout", "v5", "v5.0.0", false),
	}
	var output bytes.Buffer
	if got := Print(&output, results, Normal); got != 2 {
		t.Fatalf("Print() = %d, want 2", got)
	}
	want := ".github/workflows/ci.yml\n\n" +
		"  UPDATE  actions/setup-go\n" +
		"          v5 -> v6.1.0\n" +
		"          warning: major version change\n\n" +
		"  PIN     actions/checkout\n" +
		"          v5 -> v5.0.0\n\n" +
		"Updated 2 references in 1 file.\n" +
		"1 major version update.\n"
	if output.String() != want {
		t.Errorf("output = %q, want %q", output.String(), want)
	}
}

func TestPrintCurrent(t *testing.T) {
	var output bytes.Buffer
	if got := Print(&output, nil, DryRun); got != 0 {
		t.Fatalf("Print() = %d, want 0", got)
	}
	if output.String() != "All GitHub Actions are current.\n" {
		t.Errorf("output = %q", output.String())
	}
}

func TestPrintWarningOnly(t *testing.T) {
	results := []resolver.Result{
		{
			Occurrence: resolver.Occurrence{File: "ci.yml", Use: action.Use{
				LineNumber: 1,
				Reference:  action.Reference{Owner: "owner", Repository: "repo", Ref: "v3"},
			}},
			Current: "v3",
			Warning: "no version of owner/repo satisfies allow \"^3\"",
		},
	}
	var output bytes.Buffer
	if got := Print(&output, results, Normal); got != 0 {
		t.Fatalf("Print() = %d, want 0", got)
	}
	want := "warning: no version of owner/repo satisfies allow \"^3\"\n\nAll GitHub Actions are current.\n"
	if output.String() != want {
		t.Errorf("output = %q, want %q", output.String(), want)
	}
}

func TestPrintChangesWithWarning(t *testing.T) {
	results := []resolver.Result{
		result(".github/workflows/ci.yml", 1, "actions", "checkout", "v4", "v5.0.0", true),
		{
			Occurrence: resolver.Occurrence{File: "ci.yml", Use: action.Use{
				LineNumber: 2,
				Reference:  action.Reference{Owner: "owner", Repository: "repo", Ref: "v3"},
			}},
			Current: "v3",
			Warning: "no version of owner/repo satisfies allow \"^3\"",
		},
	}
	var output bytes.Buffer
	if got := Print(&output, results, Normal); got != 1 {
		t.Fatalf("Print() = %d, want 1", got)
	}
	want := ".github/workflows/ci.yml\n\n" +
		"  UPDATE  actions/checkout\n" +
		"          v4 -> v5.0.0\n" +
		"          warning: major version change\n\n" +
		"warning: no version of owner/repo satisfies allow \"^3\"\n\n" +
		"Updated 1 reference in 1 file.\n" +
		"1 major version update.\n"
	if output.String() != want {
		t.Errorf("output = %q, want %q", output.String(), want)
	}
}

func result(file string, line int, owner, repository, current, target string, major bool) resolver.Result {
	return resolver.Result{
		Occurrence: resolver.Occurrence{File: file, Use: action.Use{
			LineNumber: line,
			Reference:  action.Reference{Owner: owner, Repository: repository, Ref: current},
		}},
		Target:      githubapi.Target{Tag: target, SHA: "0123456789abcdef0123456789abcdef01234567"},
		Current:     current,
		Changed:     true,
		MajorChange: major,
	}
}
