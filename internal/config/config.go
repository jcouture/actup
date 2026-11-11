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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Config struct{ MinReleaseAge time.Duration }
type fileConfig struct {
	MinReleaseAge string `toml:"min-release-age"`
}

func Load(root, path string) (Config, error) {
	explicit := path != ""
	if !explicit {
		path = filepath.Join(root, ".actup.toml")
	}
	file, err := os.Open(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) && !explicit {
		return Config{24 * time.Hour}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("open config %q: %w", path, err)
	}
	defer file.Close()
	decoded := fileConfig{MinReleaseAge: "24h"}
	if err := toml.NewDecoder(file).DisallowUnknownFields().Decode(&decoded); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	age, err := parseDuration(decoded.MinReleaseAge)
	if err != nil {
		return Config{}, fmt.Errorf("parse config %q: min-release-age: %w", path, err)
	}
	return Config{age}, nil
}

func parseDuration(value string) (time.Duration, error) {
	invalid := func() (time.Duration, error) { return 0, fmt.Errorf("invalid duration %q", value) }
	if len(value) < 2 {
		return invalid()
	}
	var unit time.Duration
	switch value[len(value)-1] {
	case 's':
		unit = time.Second
	case 'm':
		unit = time.Minute
	case 'h':
		unit = time.Hour
	case 'd':
		unit = 24 * time.Hour
	case 'w':
		unit = 7 * 24 * time.Hour
	default:
		return invalid()
	}
	number := value[:len(value)-1]
	for _, c := range number {
		if c < '0' || c > '9' {
			return invalid()
		}
	}
	amount, err := strconv.ParseInt(number, 10, 64)
	if err != nil || amount == 0 || amount > (1<<63-1)/int64(unit) {
		return invalid()
	}
	return time.Duration(amount) * unit, nil
}
