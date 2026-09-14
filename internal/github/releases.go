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
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

const pageSize = 100

var stableVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)

// ErrConstraintUnsatisfied is returned when no stable version satisfies a pin constraint.
var ErrConstraintUnsatisfied = errors.New("no version satisfies pin constraint")

type candidate struct {
	tag     string
	version *semver.Version
}

type release struct {
	TagName     string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
}

type tag struct {
	Name string `json:"name"`
}

func (client *Client) latestTag(ctx context.Context, repository string, minimumAge time.Duration, constraint *semver.Constraints) (candidate, error) {
	owner, name, err := repositoryParts(repository)
	if err != nil {
		return candidate{}, err
	}
	cutoff := client.now().UTC().Add(-minimumAge)
	var best candidate
	for page := 1; ; page++ {
		var releases []release
		endpoint := fmt.Sprintf("/repos/%s/%s/releases?per_page=%d&page=%d", owner, name, pageSize, page)
		if err := client.get(ctx, endpoint, &releases); err != nil {
			return candidate{}, err
		}
		for _, release := range releases {
			if release.Draft || release.Prerelease {
				continue
			}
			version, ok := stableVersion(release.TagName)
			if !ok {
				continue
			}
			if release.PublishedAt.IsZero() {
				return candidate{}, fmt.Errorf("release %q has no valid published_at", release.TagName)
			}
			if release.PublishedAt.UTC().After(cutoff) {
				continue
			}
			if constraint != nil && !constraint.Check(version) {
				continue
			}
			best = higher(best, candidate{tag: release.TagName, version: version})
		}
		if len(releases) < pageSize {
			break
		}
	}
	if best.version != nil {
		return best, nil
	}
	return client.latestRepositoryTag(ctx, owner, name, constraint)
}

func (client *Client) latestRepositoryTag(ctx context.Context, owner, name string, constraint *semver.Constraints) (candidate, error) {
	var best candidate
	for page := 1; ; page++ {
		var tags []tag
		endpoint := fmt.Sprintf("/repos/%s/%s/tags?per_page=%d&page=%d", owner, name, pageSize, page)
		if err := client.get(ctx, endpoint, &tags); err != nil {
			return candidate{}, err
		}
		for _, tag := range tags {
			version, ok := stableVersion(tag.Name)
			if !ok {
				continue
			}
			if constraint != nil && !constraint.Check(version) {
				continue
			}
			best = higher(best, candidate{tag: tag.Name, version: version})
		}
		if len(tags) < pageSize {
			break
		}
	}
	if best.version == nil {
		if constraint != nil {
			return candidate{}, ErrConstraintUnsatisfied
		}
		return candidate{}, fmt.Errorf("no stable semantic tags or releases exist")
	}
	return best, nil
}

func stableVersion(value string) (*semver.Version, bool) {
	if !stableVersionPattern.MatchString(value) {
		return nil, false
	}
	version, err := semver.StrictNewVersion(strings.TrimPrefix(value, "v"))
	if err != nil || version.Prerelease() != "" {
		return nil, false
	}
	return version, true
}

func higher(current, next candidate) candidate {
	if current.version == nil || next.version.GreaterThan(current.version) {
		return next
	}
	return current
}
