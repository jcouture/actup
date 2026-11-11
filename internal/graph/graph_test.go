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

package graph

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/github/actions-lockfile/go/pkg/lockfile"
	"github.com/jcouture/actup/internal/action"
	"github.com/jcouture/actup/internal/github"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"

func TestDiamondMultipleRefsAndCycle(t *testing.T) {
	for _, tc := range []struct {
		name  string
		meta  map[string]string
		cycle bool
		want  int
	}{
		{"diamond", map[string]string{"a": "runs:\n  using: composite\n  steps:\n    - uses: org/c@v1\n", "b": "runs:\n  using: composite\n  steps:\n    - uses: org/c@v1\n", "c": "runs:\n  using: node20\n"}, false, 3},
		{"cycle", map[string]string{"a": "runs:\n  using: composite\n  steps:\n    - uses: org/b@v1\n", "b": "runs:\n  using: composite\n  steps:\n    - uses: org/a@v1\n"}, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
				if len(parts) < 3 || parts[0] != "repos" || parts[1] != "org" {
					http.NotFound(w, r)
					return
				}
				repo := parts[2]
				switch {
				case len(parts) == 3:
					fmt.Fprintf(w, `{"id":%d,"owner":{"id":1}}`, int(repo[0]))
				case strings.Contains(r.URL.Path, "/git/ref/tags/"):
					fmt.Fprintf(w, `{"object":{"type":"commit","sha":%q}}`, testSHA)
				case strings.Contains(r.URL.Path, "/contents/action.yml"):
					fmt.Fprintf(w, `{"encoding":"base64","content":%q}`, base64.StdEncoding.EncodeToString([]byte(tc.meta[repo])))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			b := New(github.NewClient(github.ClientConfig{BaseURL: server.URL}), lockfile.File{})
			first, err := b.Visit(context.Background(), action.Reference{Owner: "org", Repository: "a", Ref: "v1"})
			if tc.cycle {
				if err == nil || !strings.Contains(err.Error(), "cycle") {
					t.Fatalf("cycle error=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			second, err := b.Visit(context.Background(), action.Reference{Owner: "org", Repository: "b", Ref: "v1"})
			if err != nil {
				t.Fatal(err)
			}
			closure := b.Closure([]string{first, second})
			if len(closure) != tc.want || len(b.Nodes) != tc.want {
				t.Fatalf("closure=%v nodes=%v", closure, b.Nodes)
			}
			if b.Nodes[first].Uses[0] != "org/c@v1" || b.Nodes[second].Uses[0] != "org/c@v1" {
				t.Fatalf("diamond edges=%v", b.Nodes)
			}
			// A second ref for the same repository is a distinct lock identity.
			_, err = b.Visit(context.Background(), action.Reference{Owner: "org", Repository: "a", Ref: "v2"})
			if err != nil {
				t.Fatal(err)
			}
			if len(b.Nodes) != 4 {
				t.Fatalf("multiple refs collapsed: %v", b.Nodes)
			}
		})
	}
}

func TestFloatingRefRetainsExistingCommit(t *testing.T) {
	const oldSHA = "0123456789abcdef0123456789abcdef01234567"
	const newSHA = "abcdef0123456789abcdef0123456789abcdef01"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/org/a":
			fmt.Fprint(w, `{"id":9,"owner":{"id":1}}`)
		case r.URL.Path == "/repos/org/a/git/ref/tags/v1":
			fmt.Fprintf(w, `{"object":{"type":"commit","sha":%q}}`, newSHA)
		case r.URL.Path == "/repos/org/a/contents/action.yml":
			if r.URL.Query().Get("ref") != oldSHA {
				http.Error(w, "used moved commit", 400)
				return
			}
			fmt.Fprintf(w, `{"encoding":"base64","content":%q}`, base64.StdEncoding.EncodeToString([]byte("runs:\n  using: node20\n")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	existing := lockfile.File{Dependencies: map[string]lockfile.Action{"org/a@v1": {Ref: "v1", Commit: "sha1-" + oldSHA, OwnerID: 1, RepoID: 9}}}
	b := New(github.NewClient(github.ClientConfig{BaseURL: server.URL}), existing)
	_, err := b.Visit(context.Background(), action.Reference{Owner: "org", Repository: "a", Ref: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Nodes["org/a@v1"].Commit != "sha1-"+oldSHA || len(b.Warnings) != 1 {
		t.Fatalf("node=%#v warnings=%v", b.Nodes["org/a@v1"], b.Warnings)
	}
}
