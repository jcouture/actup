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

package action

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Dependency is a uses scalar in a workflow or action definition.
type Dependency struct {
	Use        Use
	Value      string
	Kind       Kind
	Start, End int
}

const (
	LocalWorkspaceAction Kind = 2 + iota
	SameRepositoryAction
	DockerAction
	ExpressionReference
	UnsupportedReference
	LocalReusableWorkflow
)

func Classify(value string, job bool) (Reference, Kind) {
	if job && strings.HasPrefix(value, "./.github/workflows/") && (strings.HasSuffix(value, ".yml") || strings.HasSuffix(value, ".yaml")) {
		return Reference{}, LocalReusableWorkflow
	}
	if strings.HasPrefix(value, "${{") {
		return Reference{}, ExpressionReference
	}
	if strings.HasPrefix(value, "docker://") {
		return Reference{}, DockerAction
	}
	if strings.HasPrefix(value, "./") {
		return Reference{}, LocalWorkspaceAction
	}
	if strings.HasPrefix(value, "$/") {
		return Reference{}, SameRepositoryAction
	}
	ref, ok := ParseReference(value)
	if !ok {
		return Reference{}, UnsupportedReference
	}
	if job {
		if ref.Kind != ReusableWorkflow {
			return ref, UnsupportedReference
		}
		return ref, ReusableWorkflow
	}
	if ref.Kind == ReusableWorkflow {
		return ref, UnsupportedReference
	}
	return ref, Action
}

func mapping(n *yaml.Node, name string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == name {
			return n.Content[i+1]
		}
	}
	return nil
}

// ParseWorkflow reads only jobs.*.steps[*].uses and jobs.*.uses.
func ParseWorkflow(src []byte) ([]Dependency, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, fmt.Errorf("parse workflow YAML: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	root := doc.Content[0]
	jobs := mapping(root, "jobs")
	if jobs == nil {
		return nil, nil
	}
	if jobs.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("jobs must be a mapping")
	}
	var out []Dependency
	for i := 1; i < len(jobs.Content); i += 2 {
		job := jobs.Content[i]
		if u := mapping(job, "uses"); u != nil {
			d, e := dependency(src, u, true)
			if e != nil {
				return nil, e
			}
			out = append(out, d)
		}
		steps := mapping(job, "steps")
		if steps == nil {
			continue
		}
		if steps.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("steps must be a sequence")
		}
		for _, step := range steps.Content {
			if u := mapping(step, "uses"); u != nil {
				d, e := dependency(src, u, false)
				if e != nil {
					return nil, e
				}
				out = append(out, d)
			}
		}
	}
	return out, nil
}

// ParseActionMetadata reads composite step dependencies from local metadata.
func ParseActionMetadata(src []byte) ([]Dependency, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, fmt.Errorf("parse action YAML: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("empty action metadata")
	}
	runs := mapping(doc.Content[0], "runs")
	using := mapping(runs, "using")
	if using == nil || !strings.EqualFold(using.Value, "composite") {
		return nil, nil
	}
	steps := mapping(runs, "steps")
	if steps == nil {
		return nil, nil
	}
	if steps.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("composite steps must be a sequence")
	}
	var out []Dependency
	for _, step := range steps.Content {
		if u := mapping(step, "uses"); u != nil {
			d, e := dependency(src, u, false)
			if e != nil {
				return nil, e
			}
			out = append(out, d)
		}
	}
	return out, nil
}

func dependency(src []byte, n *yaml.Node, job bool) (Dependency, error) {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" || n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return Dependency{}, fmt.Errorf("uses at line %d must be a single-line string", n.Line)
	}
	lineStart := 0
	for i, line := 0, 1; line < n.Line; line++ {
		j := bytes.IndexByte(src[i:], '\n')
		if j < 0 {
			return Dependency{}, fmt.Errorf("uses source line missing")
		}
		i += j + 1
		lineStart = i
	}
	start := lineStart
	for col := 1; col < n.Column; col++ {
		if start >= len(src) {
			return Dependency{}, fmt.Errorf("uses source position invalid")
		}
		_, width := utf8.DecodeRune(src[start:])
		start += width
	}
	if start < 0 || start >= len(src) {
		return Dependency{}, fmt.Errorf("uses source position invalid")
	}
	raw := src[start:]
	lineEnd := bytes.IndexByte(raw, '\n')
	if lineEnd >= 0 {
		raw = raw[:lineEnd]
	}
	if len(raw) > 0 && raw[len(raw)-1] == '\r' {
		raw = raw[:len(raw)-1]
	}
	tokenStart := start
	if n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) != 0 {
		tokenStart++
	}
	if !bytes.HasPrefix(src[tokenStart:], []byte(n.Value)) {
		return Dependency{}, fmt.Errorf("uses at line %d has unsupported scalar spelling", n.Line)
	}
	tokenEnd := tokenStart + len(n.Value)
	if tokenEnd > start+len(raw) {
		return Dependency{}, fmt.Errorf("uses scalar exceeds line")
	}
	ref, kind := Classify(n.Value, job)
	comment := ""
	if idx := bytes.IndexByte(src[tokenEnd:start+len(raw)], '#'); idx >= 0 {
		comment = string(src[tokenEnd+idx : start+len(raw)])
	}
	return Dependency{Use: Use{LineNumber: n.Line, Reference: ref, Comment: comment}, Value: n.Value, Kind: kind, Start: tokenStart, End: tokenEnd}, nil
}
