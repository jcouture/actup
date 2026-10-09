# Changelog

All notable changes to actup are documented here.

---

## v0.2.0 - 2026-10-09

- Add `--verbose` (`-v`) to show diagnostic output on stderr while scanning workflows, resolving actions, calling the GitHub API, and writing updates.
- Fix the Homebrew cask postflight hook that removes macOS quarantine from the installed binary.
- Update CodeQL to v4.38.2 and add the Go dependencies needed for diagnostic logging.

## v0.1.1 - 2026-09-24

- Publish macOS, Linux, and Windows archives automatically from version tags, with a Homebrew cask for macOS.
- Sign release checksums with Cosign and document how to verify downloaded archives.
- Add CodeQL analysis for pushes, pull requests, and weekly scans.
- Pin release and development tools with mise and add a local release snapshot target.

## v0.1.0 - 2026-09-15

Initial version.
