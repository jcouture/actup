# actup

Actup updates external GitHub Actions to the latest eligible stable release and pins them to immutable commit SHAs. It preserves workflow formatting and comments, warns about major upgrades, and supports dry-run and CI check modes.

```yaml
# Before
- uses: actions/checkout@v5

# After
- uses: actions/checkout@08c6903cd8c0fde910a37f88322edcfb5dd907a8 # v5.0.0
```

The SHA is resolved from GitHub; the value above is only an example.

## Install

```bash
go install github.com/jcouture/actup@latest
```

To build from source with the Go version pinned in `mise.toml`:

```bash
git clone https://github.com/jcouture/actup.git
cd actup
mise install
make build
```

The binary is written to `bin/actup`.

## Use

Run Actup anywhere inside a Git repository:

```bash
actup
```

Pass a directory to target another repository:

```bash
actup ../another-repository
```

Preview changes without writing files:

```bash
actup --dry-run
```

Check for updates in CI:

```bash
actup --check
```

Check mode does not write files. It exits `1` when updates are available, `0` when everything is current, and `2` on error. `--check` and `--dry-run` cannot be combined.

Set `GITHUB_TOKEN` for private repository access or higher API rate limits:

```bash
export GITHUB_TOKEN='github_pat_your_token_here'
```

## Configuration

Actup reads `.actup.toml` from the repository root. Releases must be at least 24 hours old by default.

```toml
min-release-age = "3d"
ignore = [
  "actions/checkout",
  "myorg/internal-*",
]
```

Durations accept positive whole numbers followed by `s`, `m`, `h`, `d`, or `w`. Ignore patterns use Go path-style matching against `owner/repository`.

Use another config file or add command-line exclusions with:

```bash
actup --config config/actup.toml
actup --ignore 'actions/*' --ignore 'github/codeql-action'
```

Command-line exclusions are added to those in the config file.

### Allow

The `[allow]` table constrains which versions Actup may select for specific actions. Allowed actions are still updated and SHA-pinned, but only to versions that satisfy the constraint.

```toml
[allow]
"actions/checkout" = "^5"            # minor + patch updates within v5
"actions/setup-node" = "~4.1"        # patch updates to 4.1.x only
"aws-actions/*" = ">= 2, < 4"       # explicit range
"actions/upload-artifact" = "4.1.0"  # exact version
```

Keys are action patterns using the same glob syntax as `ignore`. Values are [semver constraints](https://github.com/Masterminds/semver#checking-version-constraints):

| Syntax | Meaning |
|--------|---------|
| `^5` or `^5.2.1` | Same major, any minor and patch |
| `~4.1` or `~4.1.3` | Same major.minor, patches only |
| `4.1.0` | Exact version |
| `>= 2, < 4` | Explicit range |

When multiple patterns match the same action, the longest pattern wins. Ignored actions take priority over allowed actions. If no version satisfies an allow constraint, Actup prints a warning and leaves the action unchanged.

## Scope

Actup scans:

- `.github/workflows/*.yml` and `.github/workflows/*.yaml`
- `.github/actions/**/action.yml` and `.github/actions/**/action.yaml`
- Root-level `action.yml` and `action.yaml`

It supports external actions, action subpaths, and reusable workflows. It ignores local and Docker actions, expressions, multiline YAML values, anchors, aliases, and symlinks.

Only stable semantic versions are selected. GitHub Releases are preferred, with tags used as a fallback. The release-age setting cannot apply to tag-only releases because tags have no publication timestamp.

## Development

```bash
make test       # unit tests
make build      # build bin/actup
make precommit  # format, analyze, scan, and test
```

Run `make help` for all targets. Issues and pull requests are welcome on [GitHub](https://github.com/jcouture/actup).

## License

[MIT](LICENSE)
