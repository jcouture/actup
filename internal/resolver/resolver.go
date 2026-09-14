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

// Package resolver resolves parsed action uses concurrently and reports changes.
package resolver

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jcouture/actup/internal/action"
	githubapi "github.com/jcouture/actup/internal/github"
)

const defaultConcurrency = 8

var majorPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*))?)?$`)

type client interface {
	Resolve(context.Context, string, time.Duration, *semver.Constraints) (githubapi.Target, error)
}

// Occurrence associates a parsed use with its repository-relative file path.
type Occurrence struct {
	File string
	Use  action.Use
}

// Result describes the target selected for one source occurrence.
type Result struct {
	Occurrence  Occurrence
	Target      githubapi.Target
	Current     string
	Changed     bool
	MajorChange bool
	Warning     string
}

// Resolver performs bounded, deduplicated repository resolution.
type Resolver struct {
	client        client
	concurrency   int
	pinConstraint func(string) *semver.Constraints
}

// New creates a resolver with the default limit of eight concurrent repositories.
// The pinConstraint function returns the semver constraint for a repository, or
// nil when no pin applies.
func New(apiClient *githubapi.Client, pinConstraint func(string) *semver.Constraints) *Resolver {
	return &Resolver{client: apiClient, concurrency: defaultConcurrency, pinConstraint: pinConstraint}
}

// Resolve resolves every unique repository once and preserves occurrence order.
func (resolver *Resolver) Resolve(ctx context.Context, occurrences []Occurrence, minimumAge time.Duration) ([]Result, error) {
	occurrences = append([]Occurrence(nil), occurrences...)
	sort.SliceStable(occurrences, func(left, right int) bool {
		if occurrences[left].File != occurrences[right].File {
			return occurrences[left].File < occurrences[right].File
		}
		return occurrences[left].Use.LineNumber < occurrences[right].Use.LineNumber
	})
	repositories := make([]string, 0, len(occurrences))
	canonical := make(map[string]string)
	for _, occurrence := range occurrences {
		repository := occurrence.Use.Reference.RepositoryID()
		key := strings.ToLower(repository)
		if _, exists := canonical[key]; !exists {
			canonical[key] = repository
			repositories = append(repositories, repository)
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan string, len(repositories))
	for _, repository := range repositories {
		jobs <- repository
	}
	close(jobs)

	targets := make(map[string]githubapi.Target, len(repositories))
	warnings := make(map[string]string)
	var mu sync.Mutex
	workerCount := min(resolver.concurrency, len(repositories))
	errs := make(chan error, workerCount)
	var workers sync.WaitGroup
	for range workerCount {
		workers.Go(func() {
			for repository := range jobs {
				var constraint *semver.Constraints
				if resolver.pinConstraint != nil {
					constraint = resolver.pinConstraint(repository)
				}
				target, err := resolver.client.Resolve(ctx, repository, minimumAge, constraint)
				if err != nil {
					if constraint != nil && errors.Is(err, githubapi.ErrConstraintUnsatisfied) {
						mu.Lock()
						warnings[strings.ToLower(repository)] = fmt.Sprintf("no version of %s satisfies allow %q", repository, constraint)
						mu.Unlock()
						continue
					}
					select {
					case errs <- err:
					default:
					}
					cancel()
					return
				}
				mu.Lock()
				targets[strings.ToLower(repository)] = target
				mu.Unlock()
			}
		})
	}
	workers.Wait()
	close(errs)
	if err := <-errs; err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(occurrences))
	for _, occurrence := range occurrences {
		key := strings.ToLower(occurrence.Use.Reference.RepositoryID())
		if warning, ok := warnings[key]; ok {
			results = append(results, Result{
				Occurrence: occurrence,
				Current:    currentVersion(occurrence.Use),
				Warning:    warning,
			})
			continue
		}
		target := targets[key]
		current := currentVersion(occurrence.Use)
		currentMajor, knownMajor := semanticMajor(current)
		changed := !action.IsSHA(occurrence.Use.Reference.Ref) ||
			!strings.EqualFold(occurrence.Use.Reference.Ref, target.SHA)
		results = append(results, Result{
			Occurrence:  occurrence,
			Target:      target,
			Current:     current,
			Changed:     changed,
			MajorChange: changed && knownMajor && currentMajor != target.Major,
		})
	}
	return results, nil
}

func currentVersion(use action.Use) string {
	if action.IsSHA(use.Reference.Ref) && action.IsVersionAnnotation(use.Comment) {
		return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(use.Comment), "#"))
	}
	return use.Reference.Ref
}

func semanticMajor(value string) (uint64, bool) {
	matches := majorPattern.FindStringSubmatch(value)
	if matches == nil {
		return 0, false
	}
	major, err := strconv.ParseUint(matches[1], 10, 64)
	return major, err == nil
}
