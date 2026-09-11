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

package resolver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcouture/actup/internal/action"
	githubapi "github.com/jcouture/actup/internal/github"
)

const resolvedSHA = "1234567890abcdef1234567890abcdef12345678"

func TestHighestEligibleReleaseAndMajorChange(t *testing.T) {
	api, closeServer := resolverAPI(t, func(response http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases"):
			fmt.Fprint(response, `[
				{"tag_name":"v5.0.0","published_at":"2020-01-01T00:00:00Z"},
				{"tag_name":"v4.9.0","published_at":"2020-01-01T00:00:00Z"}
			]`)
		case strings.HasSuffix(request.URL.Path, "/git/ref/tags/v5.0.0"):
			commit(response, resolvedSHA)
		}
	})
	defer closeServer()

	results, err := New(api).Resolve(context.Background(), []Occurrence{occurrence("ci.yml", 1, "owner/repo", "v4", "")}, 0)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(results) != 1 || results[0].Target.Tag != "v5.0.0" || !results[0].Changed || !results[0].MajorChange {
		t.Fatalf("Resolve() = %#v", results)
	}
}

func TestMinimumAgeSelectsOlderRelease(t *testing.T) {
	now := time.Now().UTC()
	api, closeServer := resolverAPI(t, func(response http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases"):
			fmt.Fprintf(response, `[
				{"tag_name":"v2.0.0","published_at":%q},
				{"tag_name":"v1.5.0","published_at":%q}
			]`, now.Add(-time.Hour).Format(time.RFC3339), now.Add(-48*time.Hour).Format(time.RFC3339))
		case strings.HasSuffix(request.URL.Path, "/git/ref/tags/v1.5.0"):
			commit(response, resolvedSHA)
		}
	})
	defer closeServer()

	results, err := New(api).Resolve(context.Background(), []Occurrence{occurrence("ci.yml", 1, "owner/repo", "v1", "")}, 24*time.Hour)
	if err != nil || len(results) != 1 || results[0].Target.Tag != "v1.5.0" {
		t.Fatalf("Resolve() = %#v, %v (now %s)", results, err, now)
	}
}

func TestTagFallback(t *testing.T) {
	api, closeServer := resolverAPI(t, func(response http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases"):
			fmt.Fprint(response, `[{"tag_name":"main","published_at":"2020-01-01T00:00:00Z"}]`)
		case strings.HasSuffix(request.URL.Path, "/tags"):
			fmt.Fprint(response, `[{"name":"v2.3.0"}]`)
		case strings.HasSuffix(request.URL.Path, "/git/ref/tags/v2.3.0"):
			commit(response, resolvedSHA)
		}
	})
	defer closeServer()
	results, err := New(api).Resolve(context.Background(), []Occurrence{occurrence("ci.yml", 1, "owner/repo", "v2", "")}, 0)
	if err != nil || results[0].Target.Tag != "v2.3.0" {
		t.Fatalf("Resolve() = %#v, %v", results, err)
	}
}

func TestUnknownMajorAndAlreadyCurrentSHA(t *testing.T) {
	api, closeServer := resolverAPI(t, func(response http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/releases") {
			fmt.Fprint(response, `[{"tag_name":"v5.0.0","published_at":"2020-01-01T00:00:00Z"}]`)
		} else {
			commit(response, resolvedSHA)
		}
	})
	defer closeServer()
	occurrences := []Occurrence{
		occurrence("ci.yml", 1, "owner/repo", resolvedSHA, ""),
		occurrence("ci.yml", 2, "owner/repo", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ""),
		occurrence("ci.yml", 3, "owner/repo", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "# v4.2.2"),
	}
	results, err := New(api).Resolve(context.Background(), occurrences, 0)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if results[0].Changed || results[0].MajorChange {
		t.Errorf("current SHA result = %#v", results[0])
	}
	if !results[1].Changed || results[1].MajorChange {
		t.Errorf("unknown SHA result = %#v", results[1])
	}
	if !results[2].MajorChange || results[2].Current != "v4.2.2" {
		t.Errorf("annotated SHA result = %#v", results[2])
	}
}

func TestDeduplicatesAndOrdersConcurrentResolution(t *testing.T) {
	var mutex sync.Mutex
	releaseCalls := make(map[string]int)
	api, closeServer := resolverAPI(t, func(response http.ResponseWriter, request *http.Request) {
		parts := strings.Split(request.URL.Path, "/")
		repository := parts[2] + "/" + parts[3]
		if strings.HasSuffix(request.URL.Path, "/releases") {
			mutex.Lock()
			releaseCalls[repository]++
			mutex.Unlock()
			fmt.Fprint(response, `[{"tag_name":"v2.0.0","published_at":"2020-01-01T00:00:00Z"}]`)
			return
		}
		commit(response, resolvedSHA)
	})
	defer closeServer()
	occurrences := []Occurrence{
		occurrence("z.yml", 2, "owner/repo", "v1", ""),
		occurrence("a.yml", 3, "other/tool", "v1", ""),
		occurrence("a.yml", 1, "owner/repo", "v1", ""),
		occurrence("z.yml", 1, "owner/repo", "v1", ""),
	}
	results, err := New(api).Resolve(context.Background(), occurrences, 0)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	gotOrder := fmt.Sprintf("%s:%d,%s:%d,%s:%d,%s:%d",
		results[0].Occurrence.File, results[0].Occurrence.Use.LineNumber,
		results[1].Occurrence.File, results[1].Occurrence.Use.LineNumber,
		results[2].Occurrence.File, results[2].Occurrence.Use.LineNumber,
		results[3].Occurrence.File, results[3].Occurrence.Use.LineNumber)
	if gotOrder != "a.yml:1,a.yml:3,z.yml:1,z.yml:2" {
		t.Errorf("result order = %s", gotOrder)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if releaseCalls["owner/repo"] != 1 || releaseCalls["other/tool"] != 1 {
		t.Errorf("release calls = %#v", releaseCalls)
	}
}

func occurrence(file string, line int, repository, ref, comment string) Occurrence {
	owner, name, _ := strings.Cut(repository, "/")
	return Occurrence{File: file, Use: action.Use{
		LineNumber: line,
		Comment:    comment,
		Reference:  action.Reference{Owner: owner, Repository: name, Ref: ref},
	}}
}

func resolverAPI(t *testing.T, handler http.HandlerFunc) (*githubapi.Client, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	return githubapi.NewClient(githubapi.ClientConfig{BaseURL: server.URL}), server.Close
}

func commit(response http.ResponseWriter, sha string) {
	fmt.Fprintf(response, `{"object":{"type":"commit","sha":%q}}`, sha)
}
