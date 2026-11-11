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
	"fmt"
	"sort"
	"strings"

	"github.com/github/actions-lockfile/go/pkg/lockfile"
	"github.com/jcouture/actup/internal/action"
	"github.com/jcouture/actup/internal/github"
)

type Builder struct {
	API      *github.Client
	Existing lockfile.File
	Nodes    map[string]lockfile.Action
	state    map[string]uint8
	chain    []string
	metadata map[string][]action.Dependency
	Warnings []string
}

func New(api *github.Client, existing lockfile.File) *Builder {
	return &Builder{API: api, Existing: existing, Nodes: make(map[string]lockfile.Action), state: make(map[string]uint8), metadata: make(map[string][]action.Dependency)}
}

// VerifyExisting checks security-sensitive metadata before any dependency update
// can replace or remove an old lock entry.
func VerifyExisting(ctx context.Context, api *github.Client, existing lockfile.File) error {
	keys := make([]string, 0, len(existing.Dependencies))
	for key := range existing.Dependencies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		pin, ok := lockfile.ParsePin(key)
		if !ok {
			return fmt.Errorf("invalid locked dependency %q", key)
		}
		old := existing.Dependencies[key]
		id, err := api.Repository(ctx, pin.NWO)
		if err != nil {
			return fmt.Errorf("verify %s: %w", key, err)
		}
		if old.OwnerID != id.OwnerID || old.RepoID != id.RepoID {
			return fmt.Errorf("repository identity changed for %s: locked owner_id %d repo_id %d, current owner_id %d repo_id %d", pin.NWO, old.OwnerID, old.RepoID, id.OwnerID, id.RepoID)
		}
		sha, err := api.ResolveRef(ctx, pin.NWO, pin.Ref)
		if err != nil {
			return fmt.Errorf("verify locked dependency %s: %w", key, err)
		}
		if isExactVersion(pin.Ref) && old.Commit != "sha1-"+sha {
			return fmt.Errorf("tag %s for %s moved from %s to %s", pin.Ref, pin.NWO, strings.TrimPrefix(old.Commit, "sha1-"), sha)
		}
	}
	return nil
}

// Visit resolves the declared ref exactly; callers choose direct update targets first.
func (b *Builder) Visit(ctx context.Context, ref action.Reference) (string, error) {
	key := lockfile.IndexKey(ref.Owner, ref.Repository, ref.Ref)
	traversal := key + "/" + ref.Subpath
	if b.state[traversal] == 1 {
		return "", fmt.Errorf("composite action cycle: %s -> %s", strings.Join(b.chain, " -> "), traversal)
	}
	if b.state[traversal] == 2 {
		return key, nil
	}
	b.state[traversal] = 1
	b.chain = append(b.chain, traversal)
	defer func() { b.chain = b.chain[:len(b.chain)-1] }()
	id, err := b.API.Repository(ctx, ref.RepositoryID())
	if err != nil {
		return "", err
	}
	sha, err := b.API.ResolveRef(ctx, ref.RepositoryID(), ref.Ref)
	if err != nil {
		return "", err
	}
	if old, ok := b.Existing.Dependencies[key]; ok {
		if old.OwnerID != id.OwnerID || old.RepoID != id.RepoID {
			return "", fmt.Errorf("repository identity changed for %s: locked owner_id %d repo_id %d, current owner_id %d repo_id %d", ref.RepositoryID(), old.OwnerID, old.RepoID, id.OwnerID, id.RepoID)
		}
		oldSHA := strings.TrimPrefix(old.Commit, "sha1-")
		if oldSHA != sha {
			if isExactVersion(ref.Ref) {
				return "", fmt.Errorf("tag %s for %s moved from %s to %s", ref.Ref, ref.RepositoryID(), oldSHA, sha)
			}
			if !action.IsSHA(ref.Ref) {
				b.Warnings = append(b.Warnings, fmt.Sprintf("floating reference %s moved from %s to %s; retaining locked commit", key, oldSHA, sha))
				sha = oldSHA
			}
		}
	}
	metadataKey := ref.RepositoryID() + "/" + ref.Subpath + "@" + sha
	nested, ok := b.metadata[metadataKey]
	if !ok {
		body, err := b.API.ActionMetadata(ctx, ref.RepositoryID(), ref.Subpath, sha)
		if err != nil {
			return "", err
		}
		nested, err = action.ParseActionMetadata(body)
		if err != nil {
			return "", fmt.Errorf("parse action metadata for %s: %w", key, err)
		}
		b.metadata[metadataKey] = nested
	}
	children := make(map[string]struct{})
	for _, dep := range nested {
		switch dep.Kind {
		case action.Action:
			child, err := b.Visit(ctx, dep.Use.Reference)
			if err != nil {
				return "", fmt.Errorf("%s: %w", key, err)
			}
			children[child] = struct{}{}
		case action.DockerAction:
			continue
		default:
			return "", fmt.Errorf("unsupported nested uses %q in %s", dep.Value, key)
		}
	}
	childKeys := make([]string, 0, len(children))
	for child := range children {
		childKeys = append(childKeys, child)
	}
	sort.Strings(childKeys)
	node := lockfile.Action{Ref: ref.Ref, Commit: "sha1-" + sha, OwnerID: id.OwnerID, RepoID: id.RepoID, Uses: childKeys}
	if old, ok := b.Existing.Dependencies[key]; ok && action.IsSHA(ref.Ref) && old.Ref != "" {
		node.Ref = old.Ref
	}
	if previous, exists := b.Nodes[key]; exists {
		if previous.Commit != node.Commit {
			return "", fmt.Errorf("conflicting commits for %s", key)
		}
		node.Uses = union(previous.Uses, node.Uses)
	}
	b.Nodes[key] = node
	b.state[traversal] = 2
	return key, nil
}
func isExactVersion(ref string) bool { return action.IsVersionAnnotation(ref) }
func union(a, b []string) []string {
	set := map[string]struct{}{}
	for _, x := range a {
		set[x] = struct{}{}
	}
	for _, x := range b {
		set[x] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for x := range set {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}
func (b *Builder) Closure(roots []string) []string {
	seen := map[string]struct{}{}
	var walk func(string)
	walk = func(key string) {
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		for _, child := range b.Nodes[key].Uses {
			walk(child)
		}
	}
	for _, root := range roots {
		walk(root)
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
