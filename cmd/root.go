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
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jcouture/actup/internal/action"
	"github.com/jcouture/actup/internal/config"
	"github.com/jcouture/actup/internal/discover"
	githubapi "github.com/jcouture/actup/internal/github"
	"github.com/jcouture/actup/internal/resolver"
	"github.com/spf13/cobra"
)

var githubAPIURL = "https://api.github.com"

// Execute runs the actup root command with the supplied build version.
func Execute(version string) error {
	return newRootCommand(version).ExecuteContext(context.Background())
}

func newRootCommand(version string) *cobra.Command {
	var configPath string
	command := &cobra.Command{
		Use:           "actup",
		Short:         "Find external actions used in a Git repository",
		Args:          cobra.NoArgs,
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, _ []string) error {
			ctx, cancel := context.WithCancel(command.Context())
			defer cancel()
			if command.Flags().Changed("config") && configPath == "" {
				return fmt.Errorf("--config requires a non-empty path")
			}
			root, err := discover.RepositoryRoot()
			if err != nil {
				return err
			}
			configuration, err := config.Load(root, configPath)
			if err != nil {
				return err
			}

			files, err := discover.Files(root)
			if err != nil {
				return err
			}

			var occurrences []resolver.Occurrence
			for _, file := range files {
				contents, err := os.OpenInRoot(root, filepath.FromSlash(file))
				if err != nil {
					return fmt.Errorf("open %q: %w", file, err)
				}
				uses, parseErr := action.Parse(contents)
				closeErr := contents.Close()
				if parseErr != nil {
					return fmt.Errorf("parse %q: %w", file, parseErr)
				}
				if closeErr != nil {
					return fmt.Errorf("close %q: %w", file, closeErr)
				}
				for _, use := range uses {
					occurrences = append(occurrences, resolver.Occurrence{File: file, Use: use})
				}
			}

			apiClient := githubapi.NewClient(githubapi.ClientConfig{
				BaseURL: githubAPIURL,
				Token:   os.Getenv("GITHUB_TOKEN"),
			})
			results, err := resolver.New(apiClient).Resolve(ctx, occurrences, configuration.MinReleaseAge)
			if err != nil {
				return err
			}
			if !printResults(command, results) {
				fmt.Fprintln(command.OutOrStdout(), "All GitHub Actions are current.")
			}
			return nil
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "path to configuration file")

	return command
}

func printResults(command *cobra.Command, results []resolver.Result) bool {
	printed := false
	currentFile := ""
	for _, result := range results {
		if !result.Changed {
			continue
		}
		if result.Occurrence.File != currentFile {
			if printed {
				fmt.Fprintln(command.OutOrStdout())
			}
			fmt.Fprintln(command.OutOrStdout(), result.Occurrence.File)
			fmt.Fprintln(command.OutOrStdout())
			currentFile = result.Occurrence.File
		} else {
			fmt.Fprintln(command.OutOrStdout())
		}
		fmt.Fprintf(command.OutOrStdout(), "  UPDATE  %s\n", result.Occurrence.Use.Reference.RepositoryID())
		fmt.Fprintf(command.OutOrStdout(), "          %s -> %s\n", result.Current, result.Target.Tag)
		if result.MajorChange {
			fmt.Fprintln(command.OutOrStdout(), "          warning: major version change")
		}
		printed = true
	}
	return printed
}
