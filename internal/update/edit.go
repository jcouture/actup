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
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type edit struct {
	start       int
	end         int
	replacement []byte
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
	if written, err := temporary.Write(contents); err != nil {
		return fmt.Errorf("write temporary file for %q: %w", path, err)
	} else if written != len(contents) {
		return fmt.Errorf("write temporary file for %q: %w", path, io.ErrShortWrite)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary file for %q: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file for %q: %w", path, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}
