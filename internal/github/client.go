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

// Package github resolves GitHub repository releases and tags through the API.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.github.com"

// ClientConfig supplies the API endpoint, credentials, and testable clock.
type ClientConfig struct {
	BaseURL string
	Token   string
	Now     func() time.Time
}

// Client is a reusable GitHub API client.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	now     func() time.Time
}

// Target is the selected stable version and the commit to which its tag points.
type Target struct {
	Tag   string
	SHA   string
	Major uint64
}

// NewClient creates a client with a 15-second request timeout.
func NewClient(configuration ClientConfig) *Client {
	baseURL := strings.TrimRight(configuration.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	now := configuration.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		baseURL: baseURL,
		token:   configuration.Token,
		http: &http.Client{
			Timeout: 15 * time.Second,
		},
		now: now,
	}
}

func (client *Client) get(ctx context.Context, endpoint string, target any) error {
	requestURL, err := url.Parse(client.baseURL + endpoint)
	if err != nil {
		return fmt.Errorf("construct GitHub API URL: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return fmt.Errorf("construct GitHub API request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "actup")
	if client.token != "" {
		request.Header.Set("Authorization", "Bearer "+client.token)
	}

	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if (response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests) &&
			response.Header.Get("X-RateLimit-Remaining") == "0" {
			return fmt.Errorf("GitHub API rate limit exceeded")
		}
		return fmt.Errorf("GitHub API returned %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("decode GitHub API response: %w", err)
	}
	return nil
}

func repositoryParts(repository string) (string, string, error) {
	owner, name, found := strings.Cut(repository, "/")
	if !found || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("invalid repository %q", repository)
	}
	return url.PathEscape(owner), url.PathEscape(name), nil
}
