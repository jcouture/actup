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

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadValidDurations(t *testing.T) {
	tests := []struct {
		value string
		want  time.Duration
	}{
		{"60s", 60 * time.Second},
		{"30m", 30 * time.Minute},
		{"24h", 24 * time.Hour},
		{"1d", 24 * time.Hour},
		{"3d", 72 * time.Hour},
		{"7d", 168 * time.Hour},
		{"1w", 168 * time.Hour},
		{"2w", 336 * time.Hour},
	}

	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			root := t.TempDir()
			path := writeConfig(t, root, "config.toml", "min-release-age = \""+test.value+"\"\n")
			got, err := Load(root, path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.MinReleaseAge != test.want {
				t.Errorf("MinReleaseAge = %v, want %v", got.MinReleaseAge, test.want)
			}
		})
	}
}

func TestLoadInvalidDurations(t *testing.T) {
	for _, value := range []string{"0h", "-1h", "1.5h", "24", "foo", "1d12h", ""} {
		t.Run(value, func(t *testing.T) {
			root := t.TempDir()
			path := writeConfig(t, root, "config.toml", "min-release-age = \""+value+"\"\n")
			if _, err := Load(root, path); err == nil {
				t.Fatal("Load() error = nil, want error")
			}
		})
	}
}

func TestLoadIgnorePatterns(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     []string
	}{
		{name: "single", contents: "ignore = [\"actions/*\"]\n", want: []string{"actions/*"}},
		{name: "multiple", contents: "ignore = [\"actions/*\", \"myorg/internal-*\"]\n", want: []string{"actions/*", "myorg/internal-*"}},
		{name: "empty", contents: "ignore = []\n", want: []string{}},
		{name: "glob characters", contents: "ignore = [\"owner/repo?\", \"github/[ac]*\"]\n", want: []string{"owner/repo?", "github/[ac]*"}},
		{name: "absent", contents: "min-release-age = \"24h\"\n", want: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := writeConfig(t, root, "config.toml", test.contents)
			got, err := Load(root, path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if len(got.Ignore) != len(test.want) {
				t.Fatalf("Ignore = %v, want %v", got.Ignore, test.want)
			}
			for index := range test.want {
				if got.Ignore[index] != test.want[index] {
					t.Errorf("Ignore[%d] = %q, want %q", index, got.Ignore[index], test.want[index])
				}
			}
		})
	}
}

func TestLoadInvalidIgnorePattern(t *testing.T) {
	root := t.TempDir()
	path := writeConfig(t, root, "config.toml", "ignore = [\"actions/[invalid\"]\n")
	if _, err := Load(root, path); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadFileBehavior(t *testing.T) {
	t.Run("missing default", func(t *testing.T) {
		got, err := Load(t.TempDir(), "")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.MinReleaseAge != 24*time.Hour {
			t.Errorf("MinReleaseAge = %v, want 24h", got.MinReleaseAge)
		}
	})

	t.Run("default path", func(t *testing.T) {
		root := t.TempDir()
		writeConfig(t, root, ".actup.toml", "min-release-age = \"3d\"\n")
		got, err := Load(root, "")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.MinReleaseAge != 72*time.Hour {
			t.Errorf("MinReleaseAge = %v, want 72h", got.MinReleaseAge)
		}
	})

	t.Run("explicit path", func(t *testing.T) {
		root := t.TempDir()
		path := writeConfig(t, root, "other.toml", "min-release-age = \"30m\"\n")
		got, err := Load(root, path)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.MinReleaseAge != 30*time.Minute {
			t.Errorf("MinReleaseAge = %v, want 30m", got.MinReleaseAge)
		}
	})

	for _, test := range []struct {
		name     string
		contents string
	}{
		{"invalid TOML", "min-release-age = [\n"},
		{"unknown key", "verbose = true\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := writeConfig(t, root, "config.toml", test.contents)
			if _, err := Load(root, path); err == nil {
				t.Fatal("Load() error = nil, want error")
			}
		})
	}

	t.Run("missing explicit", func(t *testing.T) {
		root := t.TempDir()
		if _, err := Load(root, filepath.Join(root, "missing.toml")); err == nil {
			t.Fatal("Load() error = nil, want error")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		root := t.TempDir()
		path := writeConfig(t, root, "config.toml", "")
		got, err := Load(root, path)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got.MinReleaseAge != 24*time.Hour {
			t.Errorf("MinReleaseAge = %v, want 24h", got.MinReleaseAge)
		}
	})
}

func TestLoadAllowConstraints(t *testing.T) {
	tests := []struct {
		name       string
		contents   string
		wantCount  int
		repository string
		wantMatch  bool
	}{
		{
			name:       "caret constraint",
			contents:   "[allow]\n\"actions/checkout\" = \"^5\"\n",
			wantCount:  1,
			repository: "actions/checkout",
			wantMatch:  true,
		},
		{
			name:       "tilde constraint",
			contents:   "[allow]\n\"actions/setup-go\" = \"~4.1\"\n",
			wantCount:  1,
			repository: "actions/setup-go",
			wantMatch:  true,
		},
		{
			name:       "range constraint",
			contents:   "[allow]\n\"aws-actions/*\" = \">= 2, < 4\"\n",
			wantCount:  1,
			repository: "aws-actions/configure-credentials",
			wantMatch:  true,
		},
		{
			name:       "no match",
			contents:   "[allow]\n\"actions/checkout\" = \"^5\"\n",
			wantCount:  1,
			repository: "actions/setup-go",
			wantMatch:  false,
		},
		{
			name:       "empty allow table",
			contents:   "[allow]\n",
			wantCount:  0,
			repository: "actions/checkout",
			wantMatch:  false,
		},
		{
			name:       "absent allow table",
			contents:   "min-release-age = \"24h\"\n",
			wantCount:  0,
			repository: "actions/checkout",
			wantMatch:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := writeConfig(t, root, "config.toml", test.contents)
			got, err := Load(root, path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if len(got.Allow) != test.wantCount {
				t.Fatalf("Allow count = %d, want %d", len(got.Allow), test.wantCount)
			}
			constraint := got.AllowConstraint(test.repository)
			if (constraint != nil) != test.wantMatch {
				t.Errorf("AllowConstraint(%q) match = %v, want %v", test.repository, constraint != nil, test.wantMatch)
			}
		})
	}
}

func TestLoadAllowInvalidConstraint(t *testing.T) {
	root := t.TempDir()
	path := writeConfig(t, root, "config.toml", "[allow]\n\"actions/checkout\" = \"not a constraint\"\n")
	if _, err := Load(root, path); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadAllowInvalidPattern(t *testing.T) {
	root := t.TempDir()
	path := writeConfig(t, root, "config.toml", "[allow]\n\"actions/[invalid\" = \"^5\"\n")
	if _, err := Load(root, path); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestAllowConstraintMostSpecificWins(t *testing.T) {
	root := t.TempDir()
	path := writeConfig(t, root, "config.toml", "[allow]\n\"actions/*\" = \"^5\"\n\"actions/checkout\" = \"^4\"\n")
	got, err := Load(root, path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	constraint := got.AllowConstraint("actions/checkout")
	if constraint == nil {
		t.Fatal("AllowConstraint() = nil, want constraint")
	}
	if constraint.String() != "^4" {
		t.Errorf("AllowConstraint() = %q, want ^4", constraint)
	}

	constraint = got.AllowConstraint("actions/setup-go")
	if constraint == nil {
		t.Fatal("AllowConstraint(actions/setup-go) = nil, want constraint")
	}
	if constraint.String() != "^5" {
		t.Errorf("AllowConstraint(actions/setup-go) = %q, want ^5", constraint)
	}
}

func writeConfig(t *testing.T, root, name, contents string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
