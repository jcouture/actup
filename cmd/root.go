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
	"reflect"
	"sort"
	"strings"

	"github.com/github/actions-lockfile/go/pkg/lockfile"
	"github.com/jcouture/actup/internal/action"
	"github.com/jcouture/actup/internal/config"
	"github.com/jcouture/actup/internal/discover"
	githubapi "github.com/jcouture/actup/internal/github"
	"github.com/jcouture/actup/internal/graph"
	internallock "github.com/jcouture/actup/internal/lock"
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
	original []byte
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
	command := &cobra.Command{Use: "actup", Short: "Update GitHub Actions dependencies", Args: cobra.NoArgs, Version: version, SilenceErrors: true, SilenceUsage: true}
	command.RunE = func(command *cobra.Command, _ []string) error {
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
		lockMode, err := discover.Lockfile(root)
		if err != nil {
			return err
		}
		files, err := discover.Files(root)
		if err != nil {
			return err
		}
		var workflows []string
		for _, f := range files {
			if strings.HasPrefix(f, ".github/workflows/") {
				workflows = append(workflows, f)
			}
		}
		var existing lockfile.File
		lockPath := filepath.Clean(filepath.Join(root, filepath.FromSlash(lockfile.Path)))
		var lockBytes []byte
		if lockMode {
			lockBytes, err = os.ReadFile(lockPath)
			if err != nil {
				return err
			}
			existing, err = internallock.Parse(lockBytes)
			if err != nil {
				return err
			}
		}
		api := githubapi.NewClient(githubapi.ClientConfig{BaseURL: githubAPIURL, Token: os.Getenv("GITHUB_TOKEN")})
		if lockMode {
			if err := graph.VerifyExisting(command.Context(), api, existing); err != nil {
				return err
			}
		}
		builder := graph.New(api, existing)
		source := make(map[string][]byte, len(workflows))
		occurrences := make([]resolver.Occurrence, 0)
		direct := make(map[string][]action.Dependency)
		localRoots := make(map[string][]action.Dependency)
		for _, f := range workflows {
			contents, err := readRepoFile(root, f)
			if err != nil {
				return err
			}
			source[f] = contents
			deps, err := action.ParseWorkflow(contents)
			if err != nil {
				return fmt.Errorf("%s: %w", f, err)
			}
			for _, dep := range deps {
				switch dep.Kind {
				case action.Action, action.ReusableWorkflow:
					occurrences = append(occurrences, resolver.Occurrence{File: f, Use: dep.Use, Offset: dep.Start})
					direct[f] = append(direct[f], dep)
				case action.LocalWorkspaceAction, action.SameRepositoryAction:
					localRoots[f] = append(localRoots[f], dep)
				case action.DockerAction, action.LocalReusableWorkflow:
					continue
				case action.ExpressionReference, action.UnsupportedReference:
					if lockMode {
						return fmt.Errorf("%s:%d: unsupported uses %q prevents complete lockfile coverage", f, dep.Use.LineNumber, dep.Value)
					}
					fmt.Fprintf(command.ErrOrStderr(), "warning: %s:%d: unsupported uses %q left unchanged\n", f, dep.Use.LineNumber, dep.Value)
				}
			}
			sort.SliceStable(direct[f], func(i, j int) bool { return direct[f][i].Start < direct[f][j].Start })
		}
		results, err := resolver.New(api).Resolve(command.Context(), occurrences, cfg.MinReleaseAge, lockMode)
		if err != nil {
			return err
		}
		resultIndex := 0
		workflowRoots := make(map[string][]string, len(workflows))
		changes := make(map[string][]update.Change)
		for _, f := range workflows {
			for _, dep := range direct[f] {
				result := results[resultIndex]
				resultIndex++
				if result.Changed {
					changes[f] = append(changes[f], update.Change{Dependency: dep, Result: result, Lockfile: lockMode})
				}
				if dep.Kind != action.Action {
					continue
				}
				ref := dep.Use.Reference
				if result.Changed {
					if lockMode {
						ref.Ref = result.Target.Tag
					} else {
						ref.Ref = result.Target.SHA
					}
				}
				key, err := builder.Visit(command.Context(), ref)
				if err != nil {
					return fmt.Errorf("%s:%d: %w", f, dep.Use.LineNumber, err)
				}
				workflowRoots[f] = append(workflowRoots[f], key)
			}
			for _, dep := range localRoots[f] {
				remote, err := localDependencies(root, dep.Value, map[string]bool{})
				if err != nil {
					return fmt.Errorf("%s:%d: %w", f, dep.Use.LineNumber, err)
				}
				if dep.Kind == action.LocalWorkspaceAction && len(remote) > 0 {
					if lockMode {
						return fmt.Errorf("local workspace action %s uses remote actions and cannot be safely represented in the workflow dependency lockfile", dep.Value)
					}
					return fmt.Errorf("local workspace action %s uses remote actions; Actup cannot safely update this local dependency graph", dep.Value)
				}
				if dep.Kind == action.SameRepositoryAction {
					if !lockMode && len(remote) > 0 {
						return fmt.Errorf("same-repository action %s uses remote actions; LegacyMode cannot safely pin this local dependency graph", dep.Value)
					}
					for _, ref := range remote {
						key, err := builder.Visit(command.Context(), ref)
						if err != nil {
							return err
						}
						workflowRoots[f] = append(workflowRoots[f], key)
					}
				}
			}
		}
		for _, warning := range builder.Warnings {
			fmt.Fprintf(command.ErrOrStderr(), "warning: %s\n", warning)
		}
		candidate := lockfile.File{Version: lockfile.Version, Workflows: make(map[string][]string, len(workflows)), Dependencies: builder.Nodes}
		for _, f := range workflows {
			candidate.Workflows[f] = builder.Closure(workflowRoots[f])
		}
		lockChanged := false
		var newLock []byte
		if lockMode {
			newLock, err = internallock.Encode(candidate)
			if err != nil {
				return err
			}
			lockChanged = !internallock.Equal(existing, candidate)
		}
		var prepared []fileUpdate
		for _, f := range workflows {
			if len(changes[f]) == 0 {
				continue
			}
			newSource, err := update.PrepareWorkflow(source[f], changes[f])
			if err != nil {
				return fmt.Errorf("%s: %w", f, err)
			}
			if !bytes.Equal(newSource, source[f]) {
				prepared = append(prepared, fileUpdate{path: filepath.Join(root, filepath.FromSlash(f)), contents: newSource, original: source[f]})
			}
		}
		if lockMode && lockChanged {
			prepared = append(prepared, fileUpdate{path: lockPath, contents: newLock, original: lockBytes})
		}
		mode := output.Normal
		if dryRun {
			mode = output.DryRun
		} else if check {
			mode = output.Check
		}
		if !dryRun && !check {
			if err := writeTransaction(prepared); err != nil {
				return err
			}
		}
		output.Print(command.OutOrStdout(), results, mode, lockMode)
		if lockMode {
			if lockChanged {
				added, removed, changedEntries, changedWorkflows := 0, 0, 0, 0
				for key := range candidate.Dependencies {
					old, ok := existing.Dependencies[key]
					if !ok {
						added++
					} else if !reflect.DeepEqual(old, candidate.Dependencies[key]) {
						changedEntries++
					}
				}
				for key := range existing.Dependencies {
					if _, ok := candidate.Dependencies[key]; !ok {
						removed++
					}
				}
				for key, deps := range candidate.Workflows {
					if !reflect.DeepEqual(existing.Workflows[key], deps) {
						changedWorkflows++
					}
				}
				for key := range existing.Workflows {
					if _, ok := candidate.Workflows[key]; !ok {
						changedWorkflows++
					}
				}
				fmt.Fprintf(command.OutOrStdout(), "Workflow dependency lockfile: %d workflows, %d dependencies; %d workflow entries changed, %d dependencies added, %d removed, %d changed; %s.\n", len(candidate.Workflows), len(candidate.Dependencies), changedWorkflows, added, removed, changedEntries, map[bool]string{true: "update required", false: "updated"}[dryRun || check])
			}
			if len(prepared) == 0 {
				fmt.Fprintln(command.OutOrStdout(), "All GitHub Actions dependencies and the workflow lockfile are current.")
			}
		}
		if check && len(prepared) > 0 {
			return errUpdatesRequired
		}
		if dryRun {
			return nil
		}
		return nil
	}
	command.Flags().StringVar(&configPath, "config", "", "path to configuration file")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "show updates without writing files")
	command.Flags().BoolVar(&check, "check", false, "exit 1 when updates are available")
	return command
}

func readRepoFile(root, relative string) ([]byte, error) {
	f, err := os.OpenInRoot(root, filepath.FromSlash(relative))
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", relative, err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", relative, err)
	}
	return data, nil
}
func localDependencies(root, value string, seen map[string]bool) ([]action.Reference, error) {
	relative := strings.TrimPrefix(strings.TrimPrefix(value, "./"), "$/")
	clean := path.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return nil, fmt.Errorf("invalid local action path %q", value)
	}
	if seen[clean] {
		return nil, fmt.Errorf("local action cycle at %s", value)
	}
	seen[clean] = true
	defer delete(seen, clean)
	var body []byte
	var err error
	for _, name := range []string{"action.yml", "action.yaml"} {
		body, err = readRepoFile(root, path.Join(clean, name))
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if err != nil {
		return nil, fmt.Errorf("local action %s has no action metadata", value)
	}
	deps, err := action.ParseActionMetadata(body)
	if err != nil {
		return nil, err
	}
	var out []action.Reference
	for _, dep := range deps {
		switch dep.Kind {
		case action.Action:
			out = append(out, dep.Use.Reference)
		case action.LocalWorkspaceAction, action.SameRepositoryAction:
			more, err := localDependencies(root, dep.Value, seen)
			if err != nil {
				return nil, err
			}
			out = append(out, more...)
		case action.DockerAction:
			continue
		default:
			return nil, fmt.Errorf("unsupported uses %q in local action %s", dep.Value, value)
		}
	}
	return out, nil
}
func writeTransaction(files []fileUpdate) error {
	for i, file := range files {
		if err := update.WriteAtomic(file.path, file.contents); err != nil {
			var rollback []error
			for j := i - 1; j >= 0; j-- {
				if restoreErr := update.WriteAtomic(files[j].path, files[j].original); restoreErr != nil {
					rollback = append(rollback, restoreErr)
				}
			}
			return errors.Join(append([]error{fmt.Errorf("write %s: %w", file.path, err)}, rollback...)...)
		}
	}
	return nil
}
