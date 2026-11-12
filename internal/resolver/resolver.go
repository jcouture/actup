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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jcouture/actup/internal/action"
	githubapi "github.com/jcouture/actup/internal/github"
)

var majorPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*))?)?$`)

type Occurrence struct {
	File   string
	Use    action.Use
	Offset int
}
type Result struct {
	Occurrence  Occurrence
	Target      githubapi.Target
	Current     string
	Changed     bool
	MajorChange bool
}
type Resolver struct{ client *githubapi.Client }

func New(client *githubapi.Client) *Resolver { return &Resolver{client: client} }

func (r *Resolver) Resolve(ctx context.Context, occurrences []Occurrence, age time.Duration, lockfile bool) ([]Result, error) {
	occurrences = append([]Occurrence(nil), occurrences...)
	sort.SliceStable(occurrences, func(i, j int) bool {
		if occurrences[i].File != occurrences[j].File {
			return occurrences[i].File < occurrences[j].File
		}
		if occurrences[i].Offset != occurrences[j].Offset && occurrences[i].Offset > 0 && occurrences[j].Offset > 0 {
			return occurrences[i].Offset < occurrences[j].Offset
		}
		return occurrences[i].Use.LineNumber < occurrences[j].Use.LineNumber
	})
	names := make(map[string]string)
	var keys []string
	for _, o := range occurrences {
		name := o.Use.Reference.RepositoryID()
		key := strings.ToLower(name)
		if _, ok := names[key]; !ok {
			names[key] = name
			keys = append(keys, key)
		}
	}
	targets := make(map[string]githubapi.Target, len(keys))
	failures := make(map[string]error)
	var mu sync.Mutex
	jobs := make(chan string)
	var wg sync.WaitGroup
	for range min(8, len(keys)) {
		wg.Go(func() {
			for key := range jobs {
				target, err := r.client.Resolve(ctx, names[key], age)
				mu.Lock()
				if err != nil {
					failures[key] = err
				} else {
					targets[key] = target
				}
				mu.Unlock()
			}
		})
	}
	for _, key := range keys {
		jobs <- key
	}
	close(jobs)
	wg.Wait()
	for _, key := range keys {
		if err := failures[key]; err != nil {
			return nil, err
		}
	}
	results := make([]Result, 0, len(occurrences))
	for _, o := range occurrences {
		target := targets[strings.ToLower(o.Use.Reference.RepositoryID())]
		current := currentVersion(o.Use)
		oldMajor, known := semanticMajor(current)
		ref := o.Use.Reference.Ref
		changed := !action.IsSHA(ref) || !strings.EqualFold(ref, target.SHA)
		if lockfile && o.Use.Reference.Kind == action.Action {
			changed = ref != target.Tag && !(action.IsSHA(ref) && strings.EqualFold(ref, target.SHA) && current == target.Tag)
			refKind := action.ClassifyRef(ref)
			if changed && ((refKind == action.FloatingRef && known && oldMajor == target.Major) || refKind == action.OtherRef) {
				resolved, err := r.client.ResolveRef(ctx, o.Use.Reference.RepositoryID(), ref)
				if err != nil {
					return nil, err
				}
				changed = !strings.EqualFold(resolved, target.SHA)
			}
		}
		results = append(results, Result{Occurrence: o, Target: target, Current: current, Changed: changed, MajorChange: changed && known && oldMajor != target.Major})
	}
	return results, nil
}

func currentVersion(use action.Use) string {
	if action.IsSHA(use.Reference.Ref) && action.IsVersionAnnotation(use.Comment) {
		return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(use.Comment), "#"))
	}
	return use.Reference.Ref
}
func semanticMajor(s string) (uint64, bool) {
	m := majorPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseUint(m[1], 10, 64)
	return v, err == nil
}
