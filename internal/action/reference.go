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

// Package action parses external GitHub Action references from workflow lines.
package action

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode"
)

// Reference identifies an external GitHub Action or reusable workflow.
type Reference struct {
	Owner      string
	Repository string
	Subpath    string
	Ref        string
}

// RepositoryID returns the owner and repository in GitHub's owner/repository form.
func (reference Reference) RepositoryID() string {
	return reference.Owner + "/" + reference.Repository
}

// String returns the complete value used by a workflow's uses key.
func (reference Reference) String() string {
	path := reference.RepositoryID()
	if reference.Subpath != "" {
		path += "/" + reference.Subpath
	}
	return path + "@" + reference.Ref
}

// Use records a parsed reference and its source line.
type Use struct {
	Line       string
	LineNumber int
	Reference  Reference
	Comment    string
}

// ParseReference parses owner/repository[/path]@ref values. Unsupported forms
// return false.
func ParseReference(value string) (Reference, bool) {
	value = strings.TrimSpace(value)
	at := strings.LastIndexByte(value, '@')
	if at <= 0 || at == len(value)-1 {
		return Reference{}, false
	}

	path, ref := value[:at], value[at+1:]
	parts := strings.Split(path, "/")
	if len(parts) < 2 || !validRepositoryPart(parts[0]) || !validRepositoryPart(parts[1]) {
		return Reference{}, false
	}
	for _, part := range parts[2:] {
		if !validSubpathPart(part) {
			return Reference{}, false
		}
	}
	if strings.IndexFunc(ref, invalidRefRune) >= 0 {
		return Reference{}, false
	}

	return Reference{
		Owner:      parts[0],
		Repository: parts[1],
		Subpath:    strings.Join(parts[2:], "/"),
		Ref:        ref,
	}, true
}

// ParseLine parses a supported uses line at a one-based source line number.
func ParseLine(line string, lineNumber int) (Use, bool) {
	content := strings.TrimLeftFunc(line, unicode.IsSpace)
	if strings.HasPrefix(content, "#") {
		return Use{}, false
	}
	if strings.HasPrefix(content, "-") {
		content = content[1:]
		trimmed := strings.TrimLeftFunc(content, unicode.IsSpace)
		if len(trimmed) == len(content) {
			return Use{}, false
		}
		content = trimmed
	}
	if !strings.HasPrefix(content, "uses:") {
		return Use{}, false
	}

	value, comment := splitComment(strings.TrimSpace(content[len("uses:"):]))
	reference, ok := ParseReference(value)
	if !ok {
		return Use{}, false
	}
	return Use{
		Line:       line,
		LineNumber: lineNumber,
		Reference:  reference,
		Comment:    comment,
	}, true
}

// Parse reads lines and returns all supported external references.
func Parse(reader io.Reader) ([]Use, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var uses []Use
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		if use, ok := ParseLine(scanner.Text(), lineNumber); ok {
			uses = append(uses, use)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan action references: %w", err)
	}
	return uses, nil
}

// IsSHA reports whether ref is a full 40-character hexadecimal commit SHA.
func IsSHA(ref string) bool {
	if len(ref) != 40 {
		return false
	}
	for _, character := range ref {
		if !(character >= '0' && character <= '9') &&
			!(character >= 'a' && character <= 'f') &&
			!(character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}

// IsVersionAnnotation reports whether comment contains only a concrete semantic
// version, with an optional leading comment marker and v.
func IsVersionAnnotation(comment string) bool {
	comment = strings.TrimSpace(comment)
	if strings.HasPrefix(comment, "#") {
		comment = strings.TrimSpace(comment[1:])
	}
	if strings.HasPrefix(comment, "v") {
		comment = comment[1:]
	}
	parts := strings.Split(comment, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.IndexFunc(part, func(character rune) bool {
			return character < '0' || character > '9'
		}) >= 0 || len(part) > 1 && part[0] == '0' {
			return false
		}
	}
	return true
}

func splitComment(value string) (string, string) {
	previousWhitespace := false
	for index, character := range value {
		if character == '#' && (index == 0 || previousWhitespace) {
			return strings.TrimSpace(value[:index]), value[index:]
		}
		previousWhitespace = unicode.IsSpace(character)
	}
	return strings.TrimSpace(value), ""
}

func validRepositoryPart(part string) bool {
	if part == "" || part == "." || part == ".." {
		return false
	}
	for _, character := range part {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) &&
			character != '-' && character != '_' && character != '.' {
			return false
		}
	}
	return true
}

func validSubpathPart(part string) bool {
	if part == "" || part == "." || part == ".." {
		return false
	}
	return strings.IndexFunc(part, func(character rune) bool {
		return unicode.IsSpace(character) || character == '@' || character == '#' || character == ':'
	}) < 0
}

func invalidRefRune(character rune) bool {
	return unicode.IsSpace(character) || character == '@' || character == '#' ||
		character == ':' || character == '\\' || character == '~' || character == '^' ||
		character == '?' || character == '*' || character == '['
}
