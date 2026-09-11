# actup

A Go CLI that updates external GitHub Actions to the newest eligible stable release and pins them to immutable commit SHAs.

## Vision

GitHub Action tags such as `v4` are convenient, but they are mutable and can move without a workflow changing.
Actup replaces those tags with the full commit SHA behind the newest eligible stable release.
It is built for repository maintainers, security-conscious teams, and CI owners who want reproducible automation.
It scans workflows, local action definitions, and reusable workflow references from anywhere inside a Git repository.
Unlike YAML rewriters, it makes surgical text edits so formatting, comments, line endings, and unrelated content stay intact.
A configurable release cooldown avoids adopting a release immediately after publication.
Stable semantic-version releases are preferred, with semantic-version tags used when a project does not publish GitHub Releases.
Repository lookups are deduplicated and resolved concurrently, keeping larger workflow collections practical.
Dry-run mode makes it useful as an upgrade preview, while check mode turns stale pins into a CI policy failure.
Ignore patterns let teams leave selected actions under manual or separate dependency management.
Beyond routine upgrades, Actup can audit action usage, reveal major-version jumps, and normalize mutable references into reviewable security pins.
It also handles action subpaths and reusable workflows while deliberately leaving local actions, Docker actions, expressions, and unsupported YAML forms untouched.

## Installation

### Technology stack

- **Go 1.26.8** — the implementation language and required toolchain version, declared in `go.mod` and `mise.toml`.
- **Cobra 1.10.2** — command-line parsing, help, flags, and version output.
- **go-toml/v2 2.4.3** — strict `.actup.toml` decoding.
- **Masterminds/semver 3.5.0** — stable release and tag comparison.
- **GitHub REST API** — release discovery and tag-to-commit resolution.

Install the latest module directly with Go:

```bash
go install github.com/jcouture/actup@latest
```

Ensure Go's binary directory is on your `PATH` if necessary:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

To build from source with the repository's pinned Go version, install [mise](https://mise.jdx.dev/) first, then run:

```bash
git clone https://github.com/jcouture/actup.git
cd actup
mise install
go mod download
make build
./bin/actup --version
```

Without mise, use a local Go 1.26.8 installation:

```bash
go mod download
make build
./bin/actup --version
```

You should see output beginning with `actup version`; source builds use the current Git description, while unversioned builds report `dev`.

## How to Use

### Update actions in the current repository

Run Actup anywhere inside a Git working tree. It walks upward to the repository root, resolves every supported external `uses:` reference, updates files atomically, and prints a summary.

```bash
actup
```

Actup scans these locations:

```text
.github/workflows/*.yml
.github/workflows/*.yaml
.github/actions/**/action.yml
.github/actions/**/action.yaml
action.yml
action.yaml
```

Workflow files are scanned only directly under `.github/workflows`; action definitions under `.github/actions` are recursive. Symlinks are ignored.

Given this input:

```yaml
steps:
  - uses: actions/checkout@v5
```

Actup writes a full SHA and records the selected release when no human comment is present:

```yaml
steps:
  - uses: actions/checkout@08c6903cd8c0fde910a37f88322edcfb5dd907a8 # v5.0.0
```

The SHA above is an example; the actual value comes from GitHub. Existing human comments are preserved. Version-only comments such as `# v4.2.2` are maintained by Actup, and a reference already pinned to the selected SHA remains byte-for-byte unchanged.

Typical output looks like:

```text
.github/workflows/ci.yml

  UPDATE  actions/setup-go
          v5 -> v6.1.0
          warning: major version change

  PIN     actions/checkout
          v5 -> v5.0.0

Updated 2 references in 1 file.
1 major version update.
```

### Preview changes

Use dry-run mode to resolve and report updates without writing files. Available changes still exit successfully.

```bash
actup --dry-run
```

Expected summary:

```text
2 references would be updated in 1 file.
```

### Enforce current pins in CI

Check mode reports changes without writing them and exits `1` when any update is required. Exit `0` means all references are current; exit codes greater than `1` indicate an actual error.

```bash
actup --check
```

A minimal GitHub Actions step is:

```yaml
- name: Check GitHub Action pins
  run: actup --check
```

`--check` and `--dry-run` are mutually exclusive.

### Scan another repository

Pass one directory to scan a different working tree. The path must be an existing directory; Actup searches upward from it for `.git`.

```bash
actup ../another-repository
actup --dry-run ../another-repository/subdirectory
```

### Configure release age and exclusions

By default, Actup reads `.actup.toml` from the target repository root. A missing default file is allowed. The default minimum release age is 24 hours.

```toml
min-release-age = "3d"
ignore = [
  "actions/checkout",
  "myorg/internal-*",
]
```

`min-release-age` accepts positive whole numbers followed by `s`, `m`, `h`, `d`, or `w`. Unknown settings and invalid durations or patterns are errors. The cooldown applies to GitHub Releases; tag-only fallback has no publication timestamp and therefore cannot enforce it.

Load another config file with:

```bash
actup --config config/actup.toml
```

Add exclusions on the command line with repeatable, case-sensitive Go path-style patterns matched against `owner/repository`:

```bash
actup --ignore 'actions/*' --ignore 'github/codeql-action'
```

Command-line ignore patterns are added to patterns from the config file.

### Authenticate to GitHub

Actup works without authentication, subject to GitHub's unauthenticated API rate limits. Set `GITHUB_TOKEN` for private-access permissions or a higher rate limit:

```bash
export GITHUB_TOKEN='github_pat_your_token_here'
actup
```

Do not commit the token. `.env.example` documents the variable, but Actup reads it from the process environment and does not load `.env` files itself.

### Understand supported references

Actup handles single-line external actions, action subpaths, and reusable workflows:

```yaml
- uses: actions/checkout@v5
- uses: github/codeql-action/analyze@v3
- uses: org/repo/.github/workflows/build.yml@v2
```

It intentionally ignores local actions, Docker actions, expressions, multiline YAML scalars, anchors, and aliases:

```yaml
- uses: ./.github/actions/build
- uses: docker://alpine:latest
- uses: ${{ matrix.action }}
```

Only concrete stable versions such as `v5.0.0` or `5.0.0` are selected. Drafts, prereleases, moving major tags, and non-semantic tags are not release candidates. Major upgrades are applied automatically and called out in the report.

## Contributing

### Report bugs and discuss changes

Use [GitHub Issues](https://github.com/jcouture/actup/issues) for bug reports, feature proposals, and project discussion; no mailing list or separate discussion forum is declared in the repository. Bug reports should include reproduction steps, the command used, relevant workflow or configuration excerpts with secrets removed, expected behavior, actual output, and the Actup and Go versions.

### Submit a pull request

1. Fork `github.com/jcouture/actup`.
2. Create a branch from `main`.
3. Make a focused change and add or update tests.
4. Run the project checks.
5. Commit the change and open a pull request against `main`.

```bash
git switch -c fix/describe-the-change
make precommit
git add .
git commit -m "fix: describe the change"
git push -u origin fix/describe-the-change
```

Recent history uses Conventional Commits for feature work, including `feat: ...` and scoped forms such as `feat(config): ...`. Follow the same lowercase `type(optional-scope): imperative summary` format; use an appropriate type such as `fix`, `docs`, `test`, or `chore` when the change is not a feature.

The available development commands are:

```bash
make help
make build
make test
make vet
make gosec
make vulncheck
make precommit
```

`make precommit` formats and fixes Go code, updates dependencies with `go get -u ./...` and `go mod tidy`, then runs static analysis, security checks, vulnerability scanning, and tests. Review dependency changes before committing them.

Documentation-only contributions can edit `README.md` or the phase notes in `docs/` and run `make test` for a quick regression check. UX improvements that do not require backend changes can clarify examples, error-message wording, command help proposals, or expected output in an issue or documentation pull request.

## Versioning

No release tags or changelog are present yet. Until the project publishes an explicit policy, releases should follow [Semantic Versioning](https://semver.org/) with tags such as `v1.2.3`. The Makefile embeds `git describe --tags --always --dirty` into the binary; builds without injected version information report `dev`.

## License

Actup is available under the [MIT License](LICENSE), which permits use, modification, distribution, and sublicensing while retaining the copyright and permission notice.

## Credits

- Jean-Philippe Couture (`jcouture@gmail.com`) — author and sole contributor found in the Git history and license metadata.
