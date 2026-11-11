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
	"strings"
	"unicode"
)

// Reference identifies an external GitHub Action or reusable workflow.
type Kind uint8

const (
	Action Kind = iota
	ReusableWorkflow
)

type Reference struct {
	Kind       Kind
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
		Kind:       classify(parts),
	}, true
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

func classify(parts []string) Kind {
	if len(parts) == 5 && parts[2] == ".github" && parts[3] == "workflows" && (strings.HasSuffix(parts[4], ".yml") || strings.HasSuffix(parts[4], ".yaml")) && len(parts[4]) > 5 {
		return ReusableWorkflow
	}
	return Action
}
