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

package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	commitSHA = "0123456789abcdef0123456789abcdef01234567"
	tagSHA    = "abcdef0123456789abcdef0123456789abcdef01"
)

func TestRequestHeaders(t *testing.T) {
	for _, test := range []struct {
		name, token, authorization string
	}{
		{name: "authenticated", token: "secret", authorization: "Bearer secret"},
		{name: "unauthenticated"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if got := request.Header.Get("Authorization"); got != test.authorization {
					t.Errorf("Authorization = %q, want %q", got, test.authorization)
				}
				if request.Header.Get("Accept") != "application/vnd.github+json" ||
					request.Header.Get("X-GitHub-Api-Version") != "2022-11-28" ||
					request.Header.Get("User-Agent") != "actup" {
					t.Errorf("required headers missing: %#v", request.Header)
				}
				fmt.Fprint(response, `[]`)
			}))
			defer server.Close()
			client := NewClient(ClientConfig{BaseURL: server.URL, Token: test.token})
			var target []tag
			if err := client.get(context.Background(), "/headers", &target); err != nil {
				t.Fatalf("get() error = %v", err)
			}
		})
	}
}

func TestReleaseSelectionFiltersAndMinimumAge(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.FixedZone("test", 3600))
	releases := `[
		{"tag_name":"v9.0.0","draft":true,"published_at":"2020-01-01T00:00:00Z"},
		{"tag_name":"v8.0.0","prerelease":true,"published_at":"2020-01-01T00:00:00Z"},
		{"tag_name":"v7.0.0-beta.1","published_at":"2020-01-01T00:00:00Z"},
		{"tag_name":"latest","published_at":"2020-01-01T00:00:00Z"},
		{"tag_name":"v6.0.0","published_at":"2026-09-11T00:00:00Z"},
		{"tag_name":"v5.2.0","published_at":"2026-09-01T00:00:00Z"},
		{"tag_name":"v5.1.0","published_at":"2026-08-01T00:00:00Z"}
	]`
	client, closeServer := testClient(t, func(response http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases"):
			fmt.Fprint(response, releases)
		case strings.HasSuffix(request.URL.Path, "/git/ref/tags/v5.2.0"):
			fmt.Fprintf(response, `{"object":{"type":"commit","sha":%q}}`, commitSHA)
		default:
			http.NotFound(response, request)
		}
	})
	defer closeServer()
	client.now = func() time.Time { return now }

	target, err := client.Resolve(context.Background(), "owner/repo", 24*time.Hour)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if target.Tag != "v5.2.0" || target.SHA != commitSHA || target.Major != 5 {
		t.Errorf("Resolve() = %#v", target)
	}
}

func TestReleasePagination(t *testing.T) {
	var pages atomic.Int32
	client, closeServer := testClient(t, func(response http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases"):
			pages.Add(1)
			if request.URL.Query().Get("page") == "1" {
				writeReleases(response, 100, "v1.0.0")
			} else {
				fmt.Fprint(response, `[{"tag_name":"v2.0.0","published_at":"2020-01-01T00:00:00Z"}]`)
			}
		case strings.Contains(request.URL.Path, "/git/ref/tags/"):
			fmt.Fprintf(response, `{"object":{"type":"commit","sha":%q}}`, commitSHA)
		}
	})
	defer closeServer()
	target, err := client.Resolve(context.Background(), "owner/repo", 0)
	if err != nil || target.Tag != "v2.0.0" || pages.Load() != 2 {
		t.Fatalf("Resolve() = %#v, %v; pages = %d", target, err, pages.Load())
	}
}

func TestTagFallbackFiltersAndPaginates(t *testing.T) {
	var pages atomic.Int32
	client, closeServer := testClient(t, func(response http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases"):
			fmt.Fprint(response, `[]`)
		case strings.HasSuffix(request.URL.Path, "/tags"):
			pages.Add(1)
			if request.URL.Query().Get("page") == "1" {
				writeTags(response, 100, "main")
			} else {
				fmt.Fprint(response, `[{"name":"v4"},{"name":"v3.2.1"}]`)
			}
		case strings.HasSuffix(request.URL.Path, "/git/ref/tags/v3.2.1"):
			fmt.Fprintf(response, `{"object":{"type":"commit","sha":%q}}`, commitSHA)
		default:
			http.NotFound(response, request)
		}
	})
	defer closeServer()
	target, err := client.Resolve(context.Background(), "owner/repo", 365*24*time.Hour)
	if err != nil || target.Tag != "v3.2.1" || pages.Load() != 2 {
		t.Fatalf("Resolve() = %#v, %v; pages = %d", target, err, pages.Load())
	}
}

func TestAnnotatedTagResolution(t *testing.T) {
	client, closeServer := testClient(t, func(response http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases"):
			fmt.Fprint(response, `[{"tag_name":"v1.0.0","published_at":"2020-01-01T00:00:00Z"}]`)
		case strings.HasSuffix(request.URL.Path, "/git/ref/tags/v1.0.0"):
			fmt.Fprintf(response, `{"object":{"type":"tag","sha":%q}}`, tagSHA)
		case strings.HasSuffix(request.URL.Path, "/git/tags/"+tagSHA):
			fmt.Fprintf(response, `{"object":{"type":"commit","sha":%q}}`, commitSHA)
		default:
			http.NotFound(response, request)
		}
	})
	defer closeServer()
	target, err := client.Resolve(context.Background(), "owner/repo", 0)
	if err != nil || target.SHA != commitSHA {
		t.Fatalf("Resolve() = %#v, %v", target, err)
	}
}

func TestInvalidSHARejected(t *testing.T) {
	client, closeServer := testClient(t, func(response http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/releases") {
			fmt.Fprint(response, `[{"tag_name":"v1.0.0","published_at":"2020-01-01T00:00:00Z"}]`)
		} else {
			fmt.Fprint(response, `{"object":{"type":"commit","sha":"short"}}`)
		}
	})
	defer closeServer()
	_, err := client.Resolve(context.Background(), "owner/repo", 0)
	if err == nil || !strings.Contains(err.Error(), "resolve owner/repo") || !strings.Contains(err.Error(), "invalid commit SHA") {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestHTTPFailuresIdentifyRepository(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client, closeServer := testClient(t, func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(status)
				fmt.Fprint(response, "body must not appear")
			})
			defer closeServer()
			_, err := client.Resolve(context.Background(), "owner/repo", 0)
			want := fmt.Sprintf("resolve owner/repo: GitHub API returned %d", status)
			if err == nil || err.Error() != want || strings.Contains(err.Error(), "body must not appear") {
				t.Fatalf("Resolve() error = %v, want %q", err, want)
			}
		})
	}
}

func TestRateLimitError(t *testing.T) {
	client, closeServer := testClient(t, func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("X-RateLimit-Remaining", "0")
		response.WriteHeader(http.StatusForbidden)
	})
	defer closeServer()
	_, err := client.Resolve(context.Background(), "owner/repo", 0)
	if err == nil || err.Error() != "resolve owner/repo: GitHub API rate limit exceeded" {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestMalformedResponseAndMissingVersionsFail(t *testing.T) {
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		contains string
	}{
		{
			name: "malformed response",
			handler: func(response http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(response, `{not-json`)
			},
			contains: "decode GitHub API response",
		},
		{
			name: "no stable versions",
			handler: func(response http.ResponseWriter, request *http.Request) {
				if strings.HasSuffix(request.URL.Path, "/releases") {
					fmt.Fprint(response, `[]`)
				} else {
					fmt.Fprint(response, `[{"name":"v2.0.0-rc.1"},{"name":"v2"}]`)
				}
			},
			contains: "no stable semantic tags or releases exist",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, closeServer := testClient(t, test.handler)
			defer closeServer()
			_, err := client.Resolve(context.Background(), "owner/repo", 0)
			if err == nil || !strings.Contains(err.Error(), "resolve owner/repo") || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("Resolve() error = %v", err)
			}
		})
	}
}

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	return NewClient(ClientConfig{BaseURL: server.URL, Now: func() time.Time {
		return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	}}), server.Close
}

func writeReleases(response http.ResponseWriter, count int, version string) {
	response.Write([]byte("["))
	for index := range count {
		if index > 0 {
			response.Write([]byte(","))
		}
		fmt.Fprintf(response, `{"tag_name":%q,"published_at":"2020-01-01T00:00:00Z"}`, version)
	}
	response.Write([]byte("]"))
}

func writeTags(response http.ResponseWriter, count int, name string) {
	response.Write([]byte("["))
	for index := range count {
		if index > 0 {
			response.Write([]byte(","))
		}
		fmt.Fprintf(response, `{"name":%q}`, name)
	}
	response.Write([]byte("]"))
}
