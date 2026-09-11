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
	"fmt"
	"os"
	"path/filepath"

	"github.com/jcouture/actup/internal/action"
	"github.com/jcouture/actup/internal/discover"
	"github.com/spf13/cobra"
)

// Execute runs the actup root command with the supplied build version.
func Execute(version string) error {
	return newRootCommand(version).Execute()
}

func newRootCommand(version string) *cobra.Command {
	command := &cobra.Command{
		Use:           "actup",
		Short:         "Find external actions used in a Git repository",
		Args:          cobra.NoArgs,
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, _ []string) error {
			root, err := discover.RepositoryRoot()
			if err != nil {
				return err
			}

			files, err := discover.Files(root)
			if err != nil {
				return err
			}
			printed := false
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
				if len(uses) == 0 {
					continue
				}

				if printed {
					fmt.Fprintln(command.OutOrStdout())
				}
				fmt.Fprintln(command.OutOrStdout(), file)
				for _, use := range uses {
					fmt.Fprintf(command.OutOrStdout(), "  %s\n", use.Reference)
				}
				printed = true
			}
			if !printed {
				fmt.Fprintln(command.OutOrStdout(), "All GitHub Actions are current.")
			}
			return nil
		},
	}

	return command
}
