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
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/jcouture/actup/internal/action"
)

type Identity struct {
	OwnerID int64
	RepoID  int64
}

func (client *Client) Repository(ctx context.Context, repository string) (Identity, error) {
	client.mu.Lock()
	if id, ok := client.identities[strings.ToLower(repository)]; ok {
		client.mu.Unlock()
		return id, nil
	}
	client.mu.Unlock()
	owner, name, err := repositoryParts(repository)
	if err != nil {
		return Identity{}, err
	}
	var body struct {
		ID    int64 `json:"id"`
		Owner struct {
			ID int64 `json:"id"`
		} `json:"owner"`
	}
	if err := client.get(ctx, fmt.Sprintf("/repos/%s/%s", owner, name), &body); err != nil {
		return Identity{}, fmt.Errorf("repository %s: %w", repository, err)
	}
	if body.ID <= 0 || body.Owner.ID <= 0 {
		return Identity{}, fmt.Errorf("repository %s has invalid identity", repository)
	}
	id := Identity{body.Owner.ID, body.ID}
	client.mu.Lock()
	client.identities[strings.ToLower(repository)] = id
	client.mu.Unlock()
	return id, nil
}

func (client *Client) ResolveRef(ctx context.Context, repository, ref string) (string, error) {
	key := strings.ToLower(repository) + "@" + ref
	client.mu.Lock()
	cached, ok := client.refs[key]
	client.mu.Unlock()
	if ok {
		return cached, nil
	}
	sha, err := client.resolveRef(ctx, repository, ref)
	if err == nil {
		client.mu.Lock()
		client.refs[key] = sha
		client.mu.Unlock()
	}
	return sha, err
}

func (client *Client) resolveRef(ctx context.Context, repository, ref string) (string, error) {
	if action.IsSHA(ref) {
		sha := strings.ToLower(ref)
		if err := client.VerifyCommit(ctx, repository, sha); err != nil {
			return "", err
		}
		return sha, nil
	}
	owner, name, err := repositoryParts(repository)
	if err != nil {
		return "", err
	}
	// A ref can be a tag or a branch. GitHub returns 404 for the absent kind.
	for _, kind := range []string{"tags", "heads"} {
		var object gitObject
		endpoint := fmt.Sprintf("/repos/%s/%s/git/ref/%s/%s", owner, name, kind, url.PathEscape(ref))
		err := client.get(ctx, endpoint, &object)
		if err != nil {
			if IsNotFound(err) {
				continue
			}
			return "", fmt.Errorf("resolve %s@%s: %w", repository, ref, err)
		}
		if kind == "heads" {
			if object.Object.Type != "commit" || !validSHA(object.Object.SHA) {
				return "", fmt.Errorf("branch %s@%s has invalid commit", repository, ref)
			}
			return strings.ToLower(object.Object.SHA), nil
		}
		return client.dereferenceTag(ctx, owner, name, ref, object)
	}
	return "", fmt.Errorf("resolve %s@%s: GitHub API returned 404", repository, ref)
}

func (client *Client) ActionMetadata(ctx context.Context, repository, subpath, sha string) ([]byte, error) {
	cacheKey := strings.ToLower(repository) + "/" + subpath + "@" + strings.ToLower(sha)
	client.mu.Lock()
	if cached, ok := client.metadata[cacheKey]; ok {
		client.mu.Unlock()
		return append([]byte(nil), cached...), nil
	}
	client.mu.Unlock()
	owner, name, err := repositoryParts(repository)
	if err != nil {
		return nil, err
	}
	for _, filename := range []string{"action.yml", "action.yaml"} {
		path := filename
		if subpath != "" {
			path = subpath + "/" + filename
		}
		var content struct {
			Encoding string `json:"encoding"`
			Content  string `json:"content"`
		}
		endpoint := fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s", owner, name, strings.Join(escapedSegments(path), "/"), url.QueryEscape(sha))
		err := client.get(ctx, endpoint, &content)
		if err != nil {
			if IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("fetch %s/%s at %s: %w", repository, path, sha, err)
		}
		if content.Encoding != "base64" {
			return nil, fmt.Errorf("action metadata %s/%s has unsupported encoding %q", repository, path, content.Encoding)
		}
		decoded, err := base64.StdEncoding.DecodeString(content.Content)
		if err != nil {
			return nil, fmt.Errorf("decode %s/%s: %w", repository, path, err)
		}
		if len(decoded) > 1<<20 {
			return nil, fmt.Errorf("action metadata %s/%s too large", repository, path)
		}
		client.mu.Lock()
		client.metadata[cacheKey] = append([]byte(nil), decoded...)
		client.mu.Unlock()
		return decoded, nil
	}
	return nil, fmt.Errorf("action metadata not found for %s/%s at %s", repository, subpath, sha)
}

// VerifyCommit confirms that GitHub still has this exact commit in the named repository.
func (client *Client) VerifyCommit(ctx context.Context, repository, sha string) error {
	key := strings.ToLower(repository) + "@" + strings.ToLower(sha)
	client.mu.Lock()
	ok := client.commits[key]
	client.mu.Unlock()
	if ok {
		return nil
	}
	owner, name, err := repositoryParts(repository)
	if err != nil {
		return err
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := client.get(ctx, fmt.Sprintf("/repos/%s/%s/git/commits/%s", owner, name, url.PathEscape(sha)), &commit); err != nil {
		return fmt.Errorf("verify commit %s for %s: %w", sha, repository, err)
	}
	if !strings.EqualFold(commit.SHA, sha) {
		return fmt.Errorf("verify commit %s for %s: GitHub returned %q", sha, repository, commit.SHA)
	}
	client.mu.Lock()
	client.commits[key] = true
	client.mu.Unlock()
	return nil
}

// VerifyReachable checks ancestry using GitHub's compare endpoint. Only
// identical and ahead mean the locked commit is reachable from the current tip.
func (client *Client) VerifyReachable(ctx context.Context, repository, locked, tip string) error {
	key := strings.ToLower(repository) + "@" + strings.ToLower(locked) + "..." + strings.ToLower(tip)
	client.mu.Lock()
	ok := client.reachable[key]
	client.mu.Unlock()
	if ok {
		return nil
	}
	owner, name, err := repositoryParts(repository)
	if err != nil {
		return err
	}
	var comparison struct {
		Status     string `json:"status"`
		BaseCommit struct {
			SHA string `json:"sha"`
		} `json:"base_commit"`
		HeadCommit struct {
			SHA string `json:"sha"`
		} `json:"head_commit"`
	}
	endpoint := fmt.Sprintf("/repos/%s/%s/compare/%s...%s", owner, name, url.PathEscape(locked), url.PathEscape(tip))
	if err := client.get(ctx, endpoint, &comparison); err != nil {
		return fmt.Errorf("compare locked commit %s with current tip %s for %s: %w", locked, tip, repository, err)
	}
	if !strings.EqualFold(comparison.BaseCommit.SHA, locked) || !strings.EqualFold(comparison.HeadCommit.SHA, tip) {
		return fmt.Errorf("compare %s: GitHub returned unexpected commit identities", repository)
	}
	if comparison.Status != "ahead" && comparison.Status != "identical" {
		return fmt.Errorf("locked commit %s is not reachable from current ref tip %s for %s (comparison: %s)", locked, tip, repository, comparison.Status)
	}
	client.mu.Lock()
	client.reachable[key] = true
	client.mu.Unlock()
	return nil
}
func escapedSegments(path string) []string {
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return parts
}
