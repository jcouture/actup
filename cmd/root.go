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

	"github.com/jcouture/actup/internal/discover"
	"github.com/spf13/cobra"
)

// Execute runs the actup root command with the supplied build version.
func Execute(version string) error {
	command := &cobra.Command{
		Use:           "actup",
		Short:         "Find GitHub Actions files in a Git repository",
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
			if len(files) == 0 {
				fmt.Fprintln(command.OutOrStdout(), "No GitHub Actions files found.")
				return nil
			}

			for _, file := range files {
				fmt.Fprintln(command.OutOrStdout(), file)
			}
			return nil
		},
	}

	return command.Execute()
}
