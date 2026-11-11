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
	"testing"

	"github.com/jcouture/actup/internal/action"
	githubapi "github.com/jcouture/actup/internal/github"
	"github.com/jcouture/actup/internal/resolver"
)

func TestPrepareWorkflowPreservesUnrelatedBytes(t *testing.T) {
	input := "# uses: foo/build@v1\r\nname: 'foo/build@v1'\r\njobs:\r\n  build:\r\n    steps:\r\n      - uses: 'foo/build@v1'  # human note  \r\n"
	deps, err := action.ParseWorkflow([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	result := resolver.Result{Target: githubapi.Target{Tag: "v2.0.0", SHA: "0123456789abcdef0123456789abcdef01234567"}, Changed: true}
	got, err := PrepareWorkflow([]byte(input), []Change{{Dependency: deps[0], Result: result}})
	if err != nil {
		t.Fatal(err)
	}
	want := "# uses: foo/build@v1\r\nname: 'foo/build@v1'\r\njobs:\r\n  build:\r\n    steps:\r\n      - uses: 'foo/build@0123456789abcdef0123456789abcdef01234567'  # human note  \r\n"
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPrepareWorkflowVersionAnnotationsAndDuplicates(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		lock              bool
	}{
		{"legacy owned", "jobs:\n  a:\n    steps:\n      - uses: foo/build@v1  # v1.0.0  ", "jobs:\n  a:\n    steps:\n      - uses: foo/build@0123456789abcdef0123456789abcdef01234567  # v2.0.0  ", false},
		{"lock owned", "jobs:\n  a:\n    steps:\n      - uses: foo/build@0123456789abcdef0123456789abcdef01234567  # v1.0.0  ", "jobs:\n  a:\n    steps:\n      - uses: foo/build@v2.0.0  ", true},
		{"duplicate", "jobs:\n  a:\n    steps:\n      - uses: foo/build@v1\n      - uses: foo/build@v1\n", "jobs:\n  a:\n    steps:\n      - uses: foo/build@v2.0.0\n      - uses: foo/build@v2.0.0\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps, err := action.ParseWorkflow([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			changes := make([]Change, 0, len(deps))
			for _, dep := range deps {
				changes = append(changes, Change{Dependency: dep, Result: resolver.Result{Target: githubapi.Target{Tag: "v2.0.0", SHA: "0123456789abcdef0123456789abcdef01234567"}, Changed: true}, Lockfile: tc.lock})
			}
			got, err := PrepareWorkflow([]byte(tc.input), changes)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
