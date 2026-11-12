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

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseReference(t *testing.T) {
	tests := []struct {
		value string
		want  Reference
	}{
		{
			value: "actions/checkout@v4",
			want:  Reference{Owner: "actions", Repository: "checkout", Ref: "v4"},
		},
		{
			value: "github/codeql-action/analyze@v3",
			want: Reference{
				Owner: "github", Repository: "codeql-action", Subpath: "analyze", Ref: "v3",
			},
		},
		{
			value: "org/repo/.github/workflows/build.yml@v2",
			want: Reference{
				Kind: ReusableWorkflow, Owner: "org", Repository: "repo", Subpath: ".github/workflows/build.yml", Ref: "v2",
			},
		},
		{
			value: "actions/checkout@v4.2.2",
			want:  Reference{Owner: "actions", Repository: "checkout", Ref: "v4.2.2"},
		},
		{
			value: "actions/checkout@abc123def456abc123def456abc123def456abcd",
			want: Reference{
				Owner: "actions", Repository: "checkout", Ref: "abc123def456abc123def456abc123def456abcd",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			got, ok := ParseReference(test.value)
			if !ok {
				t.Fatal("ParseReference() rejected a valid reference")
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("ParseReference() = %#v, want %#v", got, test.want)
			}
			if got.RepositoryID() != got.Owner+"/"+got.Repository {
				t.Errorf("RepositoryID() = %q", got.RepositoryID())
			}
			if got.String() != strings.TrimSpace(test.value) {
				t.Errorf("String() = %q, want %q", got.String(), test.value)
			}
		})
	}
}

func TestParseReferenceIgnored(t *testing.T) {
	values := []string{
		"./.github/actions/build",
		"docker://alpine:latest",
		"${{ matrix.action }}",
		"invalid",
		"foo@v1",
		"",
		"owner/repo@",
		"owner//repo@v1",
		"owner/repo@v1 # comment",
	}

	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			if got, ok := ParseReference(value); ok {
				t.Errorf("ParseReference() = %#v, want ignored", got)
			}
		})
	}
}

func TestIsSHA(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{ref: "abc123def456abc123def456abc123def456abcd", want: true},
		{ref: "ABC123DEF456ABC123DEF456ABC123DEF456ABCD", want: true},
		{ref: "abc123", want: false},
		{ref: "zbc123def456abc123def456abc123def456abcd", want: false},
	}
	for _, test := range tests {
		if got := IsSHA(test.ref); got != test.want {
			t.Errorf("IsSHA(%q) = %v, want %v", test.ref, got, test.want)
		}
	}
}

func TestIsVersionAnnotation(t *testing.T) {
	tests := []struct {
		comment string
		want    bool
	}{
		{comment: "# v4.2.2", want: true},
		{comment: "# 4.2.2", want: true},
		{comment: "# checkout code", want: false},
		{comment: "# v4.2.2 - checkout", want: false},
		{comment: "# v04.2.2", want: false},
	}
	for _, test := range tests {
		if got := IsVersionAnnotation(test.comment); got != test.want {
			t.Errorf("IsVersionAnnotation(%q) = %v, want %v", test.comment, got, test.want)
		}
	}
}

func TestClassifyRef(t *testing.T) {
	for _, tc := range []struct {
		ref  string
		kind RefKind
	}{
		{"v5.0.0", ExactSemanticVersionRef},
		{"5.0.0", ExactSemanticVersionRef},
		{"v5", FloatingRef},
		{"v5.0", FloatingRef},
		{"main", OtherRef},
		{"release", OtherRef},
		{"v5.0.0-beta", OtherRef},
		{"0123456789abcdef0123456789abcdef01234567", CommitRef},
	} {
		if got := ClassifyRef(tc.ref); got != tc.kind {
			t.Errorf("ClassifyRef(%q) = %v, want %v", tc.ref, got, tc.kind)
		}
	}
}
