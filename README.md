# actup

Actup is a native Go CLI that updates GitHub Actions dependencies in a Git working tree. Run it from any directory in the repository:

```bash
actup
```

It discovers workflows directly under `.github/workflows`, selects the newest eligible stable, complete semantic version of each direct external Action or reusable workflow, and applies updates automatically. It warns about major version changes. It uses GitHub Releases first and falls back to tags when no eligible Release is available. Tag-only fallback cannot verify publication age.

## Repository modes

**Legacy mode** applies when `.github/workflows/actions.lock` is absent. Actup pins external Actions directly to full commit SHAs:

```yaml
- uses: actions/checkout@08c6903cd8c0fde910a37f88322edcfb5dd907a8 # v5.0.0
```

**Lockfile mode** applies when `.github/workflows/actions.lock` already exists. Actup writes exact semantic version refs in workflows and maintains the immutable commits, repository IDs, and flattened transitive dependency graph in the existing lockfile:

```yaml
- uses: actions/checkout@v5.0.0
```

Actup does not create a lockfile in a legacy repository. It parses and validates existing lockfiles using GitHub's `actions-lockfile` Go package and emits schema v0.0.3. Composite Action dependencies are read from action metadata at the resolved commit. Actup resolves upstream transitive refs exactly as declared and does not upgrade them independently.

External reusable workflows in `jobs.<job>.uses` are SHA-pinned directly in YAML in both modes, because the workflow dependency lockfile does not cover them. Local reusable workflows and Docker references are not included as Action lock dependencies.

Local workspace actions (`./...`) are inspected transitively. If they reach a remote Action, lockfile mode fails because the lockfile cannot safely represent that workspace dependency. Legacy mode also fails conservatively for that case. Commit-bound same-repository actions (`$/...`) can contribute their remote dependencies to a lockfile.

## Commands

```text
actup
actup --check
actup --dry-run
actup --config <path>
actup --version
```

`--check` writes nothing and exits 0 only when source, lockfile graph, repository identities, refs, and locked commits are current and verified. It exits 1 for normal source or lockfile updates, and 2 for verification, security, or operational errors. `--dry-run` performs the same analysis and reports direct updates, major-version warnings, reusable-workflow pin changes, and lockfile additions, removals, and graph changes without writing. Normal mode prepares the complete graph before writing files and restores prior file contents if a later write fails.

## Configuration and authentication

The optional `.actup.toml` contains exactly one setting:

```toml
min-release-age = "24h"
```

The default is 24 hours. Use a positive whole number followed by `s`, `m`, `h`, `d`, or `w`. Unknown keys are errors.

Set `GITHUB_TOKEN` for authenticated API access. It is optional for public repositories but recommended for rate limits and required when token permissions are needed for private repositories.

## Security behavior

Actup verifies every locked dependency's `owner_id` and `repo_id` against GitHub, checks that its ref still exists, and treats a moved exact semantic-version tag as a security error. Full commit refs are checked for existence and must match their locked commit. For floating refs such as `v5` or `main`, Actup retains the locked commit when the ref advances only if GitHub's commit comparison confirms that commit is still an ancestor of the current tip. A diverged or unverifiable comparison is an error; Actup never silently advances the lock. Actup does not write a partial graph or claim dynamic expressions are locked.

Workflow edits preserve every byte outside dependency references and Actup-owned version comments. Actup does not use GitHub CLI, Node.js, Git subprocesses, or any other dependency manager.
