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

package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jcouture/actup/internal/action"
	"github.com/jcouture/actup/internal/config"
	"github.com/jcouture/actup/internal/discover"
	githubapi "github.com/jcouture/actup/internal/github"
	"github.com/jcouture/actup/internal/output"
	"github.com/jcouture/actup/internal/resolver"
	"github.com/jcouture/actup/internal/update"
	"github.com/spf13/cobra"
)

var githubAPIURL = "https://api.github.com"
var errUpdatesRequired = errors.New("updates required")

type fileUpdate struct {
	path     string
	contents []byte
}

func Execute(version string) error {
	return newRootCommand(version).ExecuteContext(context.Background())
}
func ExitCode(err error) int {
	if errors.Is(err, errUpdatesRequired) {
		return 1
	}
	return 2
}
func IsCheckFailure(err error) bool { return errors.Is(err, errUpdatesRequired) }

func newRootCommand(version string) *cobra.Command {
	var configPath string
	var dryRun, check bool
	command := &cobra.Command{Use: "actup", Short: "Update GitHub Actions dependencies", Args: cobra.NoArgs, Version: version, SilenceErrors: true, SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			if dryRun && check {
				return fmt.Errorf("--check and --dry-run are mutually exclusive")
			}
			if command.Flags().Changed("config") && configPath == "" {
				return fmt.Errorf("--config requires a non-empty path")
			}
			root, err := discover.RepositoryRoot()
			if err != nil {
				return err
			}
			cfg, err := config.Load(root, configPath)
			if err != nil {
				return err
			}
			lockfile, err := discover.Lockfile(root)
			if err != nil {
				return err
			}
			files, err := discover.Files(root)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				fmt.Fprintln(command.OutOrStdout(), "No GitHub Actions files found.")
				return nil
			}
			contentsByFile := make(map[string][]byte, len(files))
			var occurrences []resolver.Occurrence
			for _, file := range files {
				opened, err := os.OpenInRoot(root, filepath.FromSlash(file))
				if err != nil {
					return fmt.Errorf("open %q: %w", file, err)
				}
				contents, readErr := io.ReadAll(opened)
				closeErr := opened.Close()
				if readErr != nil {
					return fmt.Errorf("read %q: %w", file, readErr)
				}
				if closeErr != nil {
					return fmt.Errorf("close %q: %w", file, closeErr)
				}
				uses, err := action.Parse(bytes.NewReader(contents))
				if err != nil {
					return fmt.Errorf("parse %q: %w", file, err)
				}
				contentsByFile[file] = contents
				for _, use := range uses {
					occurrences = append(occurrences, resolver.Occurrence{File: file, Use: use})
				}
			}
			api := githubapi.NewClient(githubapi.ClientConfig{BaseURL: githubAPIURL, Token: os.Getenv("GITHUB_TOKEN")})
			results, err := resolver.New(api).Resolve(command.Context(), occurrences, cfg.MinReleaseAge, lockfile)
			if err != nil {
				return err
			}
			var prepared []fileUpdate
			changed := 0
			for _, file := range files {
				var matching []resolver.Result
				for _, result := range results {
					if result.Changed && result.Occurrence.File == file {
						matching = append(matching, result)
						changed++
					}
				}
				if len(matching) == 0 {
					continue
				}
				if lockfile {
					continue
				}
				contents, err := update.Prepare(contentsByFile[file], matching)
				if err != nil {
					return fmt.Errorf("prepare %q: %w", file, err)
				}
				prepared = append(prepared, fileUpdate{filepath.Join(root, filepath.FromSlash(file)), contents})
			}
			mode := output.Normal
			if dryRun {
				mode = output.DryRun
			} else if check {
				mode = output.Check
			}
			if lockfile && !dryRun && !check && changed > 0 {
				return fmt.Errorf("workflow dependency lockfile detected; lockfile-aware updates require gh actions-lock integration (use --dry-run or --check to inspect updates)")
			}
			if !dryRun && !check {
				for _, file := range prepared {
					if err := update.WriteAtomic(file.path, file.contents); err != nil {
						return err
					}
				}
			}
			output.Print(command.OutOrStdout(), results, mode, lockfile)
			if check && changed > 0 {
				return errUpdatesRequired
			}
			return nil
		}}
	command.Flags().StringVar(&configPath, "config", "", "path to configuration file")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "show updates without writing files")
	command.Flags().BoolVar(&check, "check", false, "exit 1 when updates are available")
	return command
}
