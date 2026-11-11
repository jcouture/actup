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

package update

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/jcouture/actup/internal/action"
	"github.com/jcouture/actup/internal/resolver"
)

type Change struct {
	Dependency action.Dependency
	Result     resolver.Result
	Lockfile   bool
}

func PrepareWorkflow(src []byte, changes []Change) ([]byte, error) {
	edits := make([]edit, 0, len(changes))
	for _, change := range changes {
		dep := change.Dependency
		if dep.Start < 0 || dep.End > len(src) || dep.Start >= dep.End || string(src[dep.Start:dep.End]) != dep.Value {
			return nil, fmt.Errorf("source changed at line %d", dep.Use.LineNumber)
		}
		ref := dep.Use.Reference
		ref.Ref = change.Result.Target.SHA
		if change.Lockfile && dep.Kind == action.Action {
			ref.Ref = change.Result.Target.Tag
		}
		replacement := ref.String()
		end := dep.End
		lineEnd := bytes.IndexByte(src[end:], '\n')
		if lineEnd < 0 {
			lineEnd = len(src)
		} else {
			lineEnd += end
		}
		if lineEnd > end && src[lineEnd-1] == '\r' {
			lineEnd--
		}
		suffix := src[end:lineEnd]
		if action.IsVersionAnnotation(dep.Use.Comment) {
			idx := bytes.IndexByte(suffix, '#')
			if idx < 0 {
				return nil, fmt.Errorf("version annotation missing at line %d", dep.Use.LineNumber)
			}
			commentEnd := len(suffix)
			for commentEnd > idx && (suffix[commentEnd-1] == ' ' || suffix[commentEnd-1] == '\t') {
				commentEnd--
			}
			end += commentEnd
			if !change.Lockfile || dep.Kind == action.ReusableWorkflow {
				replacement += string(suffix[:idx]) + "# " + change.Result.Target.Tag
			} else {
				replacement += strings.TrimRight(string(suffix[:idx]), " \t")
			}
		} else if dep.Use.Comment == "" && (!change.Lockfile || dep.Kind == action.ReusableWorkflow) {
			replacement += " # " + change.Result.Target.Tag
		}
		edits = append(edits, edit{start: dep.Start, end: end, replacement: []byte(replacement)})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := append([]byte(nil), src...)
	for i, e := range edits {
		if i > 0 && e.end > edits[i-1].start {
			return nil, fmt.Errorf("overlapping source edits")
		}
		out = append(out[:e.start], append(e.replacement, out[e.end:]...)...)
	}
	return out, nil
}
