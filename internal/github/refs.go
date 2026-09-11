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
	"net/url"
	"time"
)

const maximumTagDereferences = 8

type gitObject struct {
	Object struct {
		Type string `json:"type"`
		SHA  string `json:"sha"`
	} `json:"object"`
}

// Resolve selects the newest eligible stable version and resolves its tag to a commit SHA.
func (client *Client) Resolve(ctx context.Context, repository string, minimumAge time.Duration) (Target, error) {
	candidate, err := client.latestTag(ctx, repository, minimumAge)
	if err != nil {
		return Target{}, fmt.Errorf("resolve %s: %w", repository, err)
	}
	sha, err := client.resolveTag(ctx, repository, candidate.tag)
	if err != nil {
		return Target{}, fmt.Errorf("resolve %s: %w", repository, err)
	}
	return Target{Tag: candidate.tag, SHA: sha, Major: candidate.version.Major()}, nil
}

func (client *Client) resolveTag(ctx context.Context, repository, tag string) (string, error) {
	owner, name, err := repositoryParts(repository)
	if err != nil {
		return "", err
	}
	var object gitObject
	endpoint := fmt.Sprintf("/repos/%s/%s/git/ref/tags/%s", owner, name, url.PathEscape(tag))
	if err := client.get(ctx, endpoint, &object); err != nil {
		return "", err
	}

	seen := make(map[string]struct{})
	for dereferences := 0; ; dereferences++ {
		switch object.Object.Type {
		case "commit":
			if !validSHA(object.Object.SHA) {
				return "", fmt.Errorf("tag %q resolved to an invalid commit SHA", tag)
			}
			return object.Object.SHA, nil
		case "tag":
			if dereferences == maximumTagDereferences {
				return "", fmt.Errorf("tag %q exceeded %d dereference steps", tag, maximumTagDereferences)
			}
			if !validSHA(object.Object.SHA) {
				return "", fmt.Errorf("tag %q contains an invalid tag object SHA", tag)
			}
			if _, exists := seen[object.Object.SHA]; exists {
				return "", fmt.Errorf("tag %q contains a dereference cycle", tag)
			}
			tagObjectSHA := object.Object.SHA
			seen[tagObjectSHA] = struct{}{}
			object = gitObject{}
			endpoint = fmt.Sprintf("/repos/%s/%s/git/tags/%s", owner, name, tagObjectSHA)
			if err := client.get(ctx, endpoint, &object); err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("tag %q points to unsupported object type %q", tag, object.Object.Type)
		}
	}
}

func validSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9') &&
			!(character >= 'a' && character <= 'f') &&
			!(character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}
