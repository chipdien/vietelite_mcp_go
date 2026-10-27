# Changelog

All notable changes to the VietElite Go MCP Server are documented here.

Release notes for a GitHub Release are taken from the section whose heading
matches the pushed tag. Keep headings in the form `## vX.Y.Z` (optionally
`## [vX.Y.Z]`) so `.github/workflows/release.yml` can extract them.

## v1.1.0

- Add automated release builds: pushing a `v*` tag cross-compiles binaries for
  linux/macOS/Windows on amd64 and arm64, publishes `checksums.txt`, and creates
  a GitHub Release.
- Add `--version` to print version, commit, build date, and platform.
- Add self-update support via GitHub Releases:
  - `--check-update` reports whether a newer release exists (exit code `2` when
    one is available).
  - `--update` downloads, checksum-verifies, and atomically installs the newest
    release.
  - `MCP_AUTO_UPDATE=1` performs the same update check on startup.
- Update notices are written to stderr and the log file, never to stdout, so the
  stdio transport's JSON-RPC stream is never corrupted.

## v1.0.0

- Initial documented release of the Go MCP server with generic and business
  tools, stdio and SSE transports, and bearer-token auth for SSE.
