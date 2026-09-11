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
	"path"
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

// Execute runs the actup root command with the supplied build version.
func Execute(version string) error {
	return newRootCommand(version).ExecuteContext(context.Background())
}

// ExitCode maps command results to the CLI's documented exit statuses.
func ExitCode(err error) int {
	if errors.Is(err, errUpdatesRequired) {
		return 1
	}
	return 2
}

// IsCheckFailure reports whether check mode found available updates.
func IsCheckFailure(err error) bool {
	return errors.Is(err, errUpdatesRequired)
}

func newRootCommand(version string) *cobra.Command {
	var configPath string
	var dryRun bool
	var check bool
	var ignorePatterns []string
	command := &cobra.Command{
		Use:           "actup [directory]",
		Short:         "Pin GitHub Actions to current immutable commit SHAs",
		Args:          cobra.MaximumNArgs(1),
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, args []string) error {
			if dryRun && check {
				return fmt.Errorf("--check and --dry-run are mutually exclusive")
			}
			ctx, cancel := context.WithCancel(command.Context())
			defer cancel()
			if command.Flags().Changed("config") && configPath == "" {
				return fmt.Errorf("--config requires a non-empty path")
			}
			start := "."
			if len(args) == 1 {
				start = args[0]
			}
			root, err := discover.RepositoryRootFrom(start)
			if err != nil {
				return err
			}
			configuration, err := config.Load(root, configPath)
			if err != nil {
				return err
			}
			for index, pattern := range ignorePatterns {
				if _, err := path.Match(pattern, ""); err != nil {
					return fmt.Errorf("--ignore[%d]: %w", index, err)
				}
			}
			configuration.Ignore = append(configuration.Ignore, ignorePatterns...)

			files, err := discover.Files(root)
			if err != nil {
				return err
			}

			var occurrences []resolver.Occurrence
			contentsByFile := make(map[string][]byte, len(files))
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
				uses, parseErr := action.Parse(bytes.NewReader(contents))
				if parseErr != nil {
					return fmt.Errorf("parse %q: %w", file, parseErr)
				}
				if closeErr != nil {
					return fmt.Errorf("close %q: %w", file, closeErr)
				}
				contentsByFile[file] = contents
				for _, use := range uses {
					occurrences = append(occurrences, resolver.Occurrence{File: file, Use: use})
				}
			}
			occurrences, err = filterIgnored(occurrences, configuration.Ignore)
			if err != nil {
				return err
			}

			apiClient := githubapi.NewClient(githubapi.ClientConfig{
				BaseURL: githubAPIURL,
				Token:   os.Getenv("GITHUB_TOKEN"),
			})
			results, err := resolver.New(apiClient).Resolve(ctx, occurrences, configuration.MinReleaseAge)
			if err != nil {
				return err
			}

			prepared := make([]fileUpdate, 0, len(files))
			for _, file := range files {
				fileResults := resultsForFile(results, file)
				if len(fileResults) == 0 {
					continue
				}
				contents, err := update.Prepare(contentsByFile[file], fileResults)
				if err != nil {
					return fmt.Errorf("prepare %q: %w", file, err)
				}
				prepared = append(prepared, fileUpdate{path: filepath.Join(root, filepath.FromSlash(file)), contents: contents})
			}

			mode := output.Normal
			if dryRun {
				mode = output.DryRun
			} else if check {
				mode = output.Check
			}
			if !dryRun && !check {
				for _, file := range prepared {
					if err := update.WriteAtomic(file.path, file.contents); err != nil {
						return err
					}
				}
			}
			changed := output.Print(command.OutOrStdout(), results, mode)
			if check && changed > 0 {
				return errUpdatesRequired
			}
			return nil
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "path to configuration file")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "print updates without writing files")
	command.Flags().BoolVar(&check, "check", false, "exit 1 when updates are available")
	command.Flags().StringArrayVar(&ignorePatterns, "ignore", nil, "action pattern to exclude (may be repeated)")

	return command
}

func filterIgnored(occurrences []resolver.Occurrence, patterns []string) ([]resolver.Occurrence, error) {
	for _, pattern := range patterns {
		if _, err := path.Match(pattern, ""); err != nil {
			return nil, fmt.Errorf("ignore pattern %q: %w", pattern, err)
		}
	}
	filtered := make([]resolver.Occurrence, 0, len(occurrences))
	for _, occurrence := range occurrences {
		ignored := false
		for _, pattern := range patterns {
			matches, err := path.Match(pattern, occurrence.Use.Reference.RepositoryID())
			if err != nil {
				return nil, fmt.Errorf("ignore pattern %q: %w", pattern, err)
			}
			if matches {
				ignored = true
				break
			}
		}
		if !ignored {
			filtered = append(filtered, occurrence)
		}
	}
	return filtered, nil
}

func resultsForFile(results []resolver.Result, file string) []resolver.Result {
	var matching []resolver.Result
	for _, result := range results {
		if result.Changed && result.Occurrence.File == file {
			matching = append(matching, result)
		}
	}
	return matching
}
