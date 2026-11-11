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

func TestLoad(t *testing.T) {
	tests := []struct {
		name, content string
		want          time.Duration
		bad           bool
	}{
		{"default", "", 24 * time.Hour, false}, {"seconds", `min-release-age = "30s"`, 30 * time.Second, false}, {"minutes", `min-release-age = "30m"`, 30 * time.Minute, false}, {"days", `min-release-age = "3d"`, 72 * time.Hour, false}, {"weeks", `min-release-age = "1w"`, 168 * time.Hour, false},
		{"zero", `min-release-age = "0h"`, 0, true}, {"negative", `min-release-age = "-1h"`, 0, true}, {"fraction", `min-release-age = "1.5h"`, 0, true}, {"compound", `min-release-age = "1d12h"`, 0, true}, {"missing unit", `min-release-age = "24"`, 0, true}, {"unknown key", `ignore = ["foo"]`, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.content != "" {
				if err := os.WriteFile(filepath.Join(root, ".actup.toml"), []byte(tt.content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := Load(root, "")
			if (err != nil) != tt.bad {
				t.Fatalf("Load() = %#v, %v", cfg, err)
			}
			if !tt.bad && cfg.MinReleaseAge != tt.want {
				t.Fatalf("age = %v, want %v", cfg.MinReleaseAge, tt.want)
			}
		})
	}
}
func TestExplicitConfig(t *testing.T) {
	root := t.TempDir()
	if _, err := Load(root, filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing explicit config accepted")
	}
	path := filepath.Join(root, "custom.toml")
	if err := os.WriteFile(path, []byte(`min-release-age = "2h"`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(root, path)
	if err != nil || cfg.MinReleaseAge != 2*time.Hour {
		t.Fatalf("Load() = %#v, %v", cfg, err)
	}
}
