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

// Package output renders human-readable update reports.
package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/jcouture/actup/internal/action"
	"github.com/jcouture/actup/internal/resolver"
)

// Mode selects the summary language for an update report.
type Mode uint8

const (
	Normal Mode = iota
	DryRun
	Check
)

// Print writes changed results and a mode-specific summary. It returns the
// number of changed references.
func Print(writer io.Writer, results []resolver.Result, mode Mode) int {
	var warnings []string
	warned := make(map[string]bool)
	for _, result := range results {
		if result.Warning != "" {
			repo := result.Occurrence.Use.Reference.RepositoryID()
			if !warned[repo] {
				warned[repo] = true
				warnings = append(warnings, result.Warning)
			}
		}
	}

	changed, files, majors := 0, 0, 0
	currentFile := ""
	for _, result := range results {
		if !result.Changed {
			continue
		}
		if result.Occurrence.File != currentFile {
			if changed > 0 {
				fmt.Fprintln(writer)
			}
			fmt.Fprintln(writer, result.Occurrence.File)
			fmt.Fprintln(writer)
			currentFile = result.Occurrence.File
			files++
		} else {
			fmt.Fprintln(writer)
		}
		kind := "UPDATE"
		if isPin(result) {
			kind = "PIN"
		}
		fmt.Fprintf(writer, "  %-6s  %s\n", kind, result.Occurrence.Use.Reference.RepositoryID())
		fmt.Fprintf(writer, "          %s -> %s\n", result.Current, result.Target.Tag)
		if result.MajorChange {
			fmt.Fprintln(writer, "          warning: major version change")
			majors++
		}
		changed++
	}

	if changed == 0 && len(warnings) == 0 {
		fmt.Fprintln(writer, "All GitHub Actions are current.")
		return 0
	}

	if changed > 0 {
		fmt.Fprintln(writer)
	}
	for _, w := range warnings {
		fmt.Fprintf(writer, "warning: %s\n", w)
	}

	if changed == 0 {
		fmt.Fprintln(writer)
		fmt.Fprintln(writer, "All GitHub Actions are current.")
		return 0
	}

	if len(warnings) > 0 {
		fmt.Fprintln(writer)
	}
	switch mode {
	case DryRun:
		fmt.Fprintf(writer, "%d %s would be updated in %d %s.\n", changed, plural(changed, "reference", "references"), files, plural(files, "file", "files"))
	case Check:
		verb := "require"
		if changed == 1 {
			verb = "requires"
		}
		fmt.Fprintf(writer, "%d %s %s updates in %d %s.\n", changed, plural(changed, "reference", "references"), verb, files, plural(files, "file", "files"))
	default:
		fmt.Fprintf(writer, "Updated %d %s in %d %s.\n", changed, plural(changed, "reference", "references"), files, plural(files, "file", "files"))
	}
	if majors > 0 {
		fmt.Fprintf(writer, "%d major version %s.\n", majors, plural(majors, "update", "updates"))
	}
	return changed
}

func isPin(result resolver.Result) bool {
	if action.IsSHA(result.Occurrence.Use.Reference.Ref) {
		return false
	}
	current, currentIsVersion := versionParts(result.Current)
	target, targetIsVersion := versionParts(result.Target.Tag)
	if !currentIsVersion || !targetIsVersion {
		return true
	}
	if len(current) > len(target) {
		return false
	}
	return strings.Join(target[:len(current)], ".") == strings.Join(current, ".")
}

func versionParts(value string) ([]string, bool) {
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) == 0 || len(parts) > 3 {
		return nil, false
	}
	for _, part := range parts {
		if part == "" || strings.IndexFunc(part, func(character rune) bool {
			return character < '0' || character > '9'
		}) >= 0 {
			return nil, false
		}
	}
	return parts, true
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
