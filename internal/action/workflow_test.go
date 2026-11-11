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

package action

import "testing"

func TestWorkflowStructureAndOffsets(t *testing.T) {
	src := []byte("# uses: wrong/repo@v1\r\nname: 'actions/checkout@v4'\r\njobs:\r\n  build:\r\n    steps:\r\n      - uses: 'actions/checkout@v4' # keep\r\n  call:\r\n    uses: acme/flows/.github/workflows/build.yml@v3\r\n")
	got, err := ParseWorkflow(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Kind != Action || got[1].Kind != ReusableWorkflow {
		t.Fatalf("dependencies = %#v", got)
	}
	if string(src[got[0].Start:got[0].End]) != "actions/checkout@v4" || got[0].Use.LineNumber != 6 {
		t.Fatalf("offset = %#v", got[0])
	}
	if string(src[got[1].Start:got[1].End]) != "acme/flows/.github/workflows/build.yml@v3" {
		t.Fatalf("offset = %#v", got[1])
	}
}
func TestClassifyOtherReferences(t *testing.T) {
	for _, tc := range []struct {
		value string
		kind  Kind
	}{{"./.github/actions/build", LocalWorkspaceAction}, {"$/actions/build", SameRepositoryAction}, {"docker://alpine:3.22", DockerAction}, {"${{ matrix.action }}", ExpressionReference}, {"invalid", UnsupportedReference}} {
		_, kind := Classify(tc.value, false)
		if kind != tc.kind {
			t.Errorf("%s: %v", tc.value, kind)
		}
	}
}

func TestWorkflowFlowStyleUnicodeOffset(t *testing.T) {
	src := []byte("jobs: {build: {steps: [{name: 'é', uses: actions/checkout@v4}]}}\n")
	deps, err := ParseWorkflow(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || string(src[deps[0].Start:deps[0].End]) != "actions/checkout@v4" {
		t.Fatalf("deps=%#v, %v", deps, err)
	}
}
