# actup

Actup updates external GitHub Actions dependencies in a Git repository. Run it from any directory inside the working tree:

```bash
actup
```

It scans workflow files directly under `.github/workflows`, action definitions under `.github/actions`, and root `action.yml` or `action.yaml`. It selects the newest eligible stable, complete semantic version from GitHub Releases, then falls back to tags if no eligible release exists. Releases must be at least 24 hours old by default; tag-only results have no publication date, so that age policy cannot apply to them.

In repositories without `.github/workflows/actions.lock`, Actup automatically replaces external action and reusable workflow refs with the resolved full commit SHA. It adds a version comment when there is no comment, replaces an Actup-owned version comment, and preserves human comments. It preserves unrelated file bytes and warns when the old and new semantic major versions differ. Major updates are applied automatically.

```yaml
# Before
- uses: actions/checkout@v4
# After (example SHA)
- uses: actions/checkout@08c6903cd8c0fde910a37f88322edcfb5dd907a8 # v5.0.0
```

Use `actup --dry-run` to preview without writing. Use `actup --check` in CI: exit `0` means current, `1` means updates exist, and `2` means an error. `--check` and `--dry-run` cannot be combined. `actup --version` prints the build version.

Actup reads `.actup.toml` from the repository root, or another file supplied with `--config <path>`. The only supported setting is:

```toml
min-release-age = "24h"
```

Use a positive whole number followed by `s`, `m`, `h`, `d`, or `w`. Unknown keys are errors. Set `GITHUB_TOKEN` in the environment for authenticated GitHub API requests.

## Workflow dependency lockfiles

If `.github/workflows/actions.lock` exists as a regular file, Actup detects lockfile mode. A normal `actup` run resolves dependencies and refuses to write if any updates are needed, because lockfile-aware application requires `gh actions-lock` integration. `--dry-run` reports chosen versions and indicates that reusable workflows still require SHA pinning. `--check` reports available dependency updates without writing, but does not validate the lockfile. Full lockfile integration is the next implementation milestone.

Local actions, `$/` references, Docker actions, expressions, and unsupported multiline YAML syntax are left untouched. Actup does not create commits or pull requests.
