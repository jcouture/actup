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

// Package update prepares and atomically writes surgical workflow edits.
package update

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/jcouture/actup/internal/action"
	"github.com/jcouture/actup/internal/resolver"
)

type edit struct {
	start       int
	end         int
	replacement []byte
}

// Prepare applies changed resolver results to contents without modifying bytes
// outside the reference and an optional Actup-owned version annotation.
func Prepare(contents []byte, results []resolver.Result) ([]byte, error) {
	byLine := make(map[int]resolver.Result, len(results))
	for _, result := range results {
		if result.Changed {
			byLine[result.Occurrence.Use.LineNumber] = result
		}
	}

	var edits []edit
	lineNumber := 1
	for start := 0; start < len(contents); lineNumber++ {
		end := bytes.IndexByte(contents[start:], '\n')
		if end < 0 {
			end = len(contents)
		} else {
			end += start
		}
		lineEnd := end
		if lineEnd > start && contents[lineEnd-1] == '\r' {
			lineEnd--
		}
		if result, ok := byLine[lineNumber]; ok {
			lineEdit, err := prepareLine(contents[start:lineEnd], start, result)
			if err != nil {
				return nil, err
			}
			edits = append(edits, lineEdit)
		}
		if end == len(contents) {
			break
		}
		start = end + 1
	}
	if len(byLine) != len(edits) {
		return nil, fmt.Errorf("prepare edits: resolved source line no longer exists")
	}

	sort.Slice(edits, func(left, right int) bool { return edits[left].start > edits[right].start })
	updated := append([]byte(nil), contents...)
	for _, edit := range edits {
		updated = append(updated[:edit.start], append(edit.replacement, updated[edit.end:]...)...)
	}
	return updated, nil
}

func prepareLine(line []byte, lineOffset int, result resolver.Result) (edit, error) {
	use, ok := action.ParseLine(string(line), result.Occurrence.Use.LineNumber)
	if !ok || use.Reference.String() != result.Occurrence.Use.Reference.String() {
		return edit{}, fmt.Errorf("prepare edit for line %d: source changed after parsing", result.Occurrence.Use.LineNumber)
	}

	reference := []byte(use.Reference.String())
	start := bytes.Index(line, reference)
	if start < 0 {
		return edit{}, fmt.Errorf("prepare edit for line %d: reference not found", result.Occurrence.Use.LineNumber)
	}
	end := start + len(reference)
	replacement := use.Reference
	replacement.Ref = result.Target.SHA
	replacementText := replacement.String()

	if use.Comment == "" {
		replacementText += " # " + result.Target.Tag
	} else if action.IsVersionAnnotation(use.Comment) {
		commentStart := bytes.Index(line[end:], []byte("#"))
		if commentStart < 0 {
			return edit{}, fmt.Errorf("prepare edit for line %d: version annotation not found", result.Occurrence.Use.LineNumber)
		}
		commentStart += end
		commentEnd := len(line)
		for commentEnd > commentStart && (line[commentEnd-1] == ' ' || line[commentEnd-1] == '\t') {
			commentEnd--
		}
		end = commentEnd
		replacementText += string(line[start+len(reference):commentStart]) + "# " + result.Target.Tag
	}
	return edit{start: lineOffset + start, end: lineOffset + end, replacement: []byte(replacementText)}, nil
}

// WriteAtomic replaces path with contents while preserving its file mode.
func WriteAtomic(path string, contents []byte) (returnErr error) {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %q: %w", path, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".actup-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %q: %w", path, err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if returnErr != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(info.Mode()); err != nil {
		return fmt.Errorf("set mode on temporary file for %q: %w", path, err)
	}
	if _, err := temporary.Write(contents); err != nil {
		return fmt.Errorf("write temporary file for %q: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file for %q: %w", path, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}
