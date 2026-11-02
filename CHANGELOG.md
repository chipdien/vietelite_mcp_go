# Changelog

All notable changes to the VietElite Go MCP Server are documented here.

Release notes for a GitHub Release are taken from the section whose heading
matches the pushed tag. Keep headings in the form `## vX.Y.Z` (optionally
`## [vX.Y.Z]`) so `.github/workflows/release.yml` can extract them.

## v1.2.0

- Add 15+ domain and operational MCP tools across Academic, Finance, Payroll, Reconciliation, and Analytics:
  - Academic: `get_class_detail`, `get_session_detail`, `list_students_at_risk`, `list_ungraded_homework`, `get_homework_submissions`.
  - Finance: `get_invoice_detail`, `list_unpaid_invoices`, `find_unallocated_receipts`.
  - Payroll & Reconciliation: `get_my_payroll`, `get_teacher_payroll_detail`, `get_my_contract`, `reconcile_payroll_vs_contract`, `reconcile_invoice_vs_receipts`, `reconcile_bank_vs_receipts`.
  - Aggregates: `get_center_dashboard`, `compare_centers_performance`, `get_network_overview`.
  - Core Business: `get_student_payment_history`, `get_student_enrollment_history`, `get_class_debt_summary`.
- Add MCP Resources: database schema docs (`schema://*`), capability gaps report (`gaps://report`), and role guides (`guide://tools-by-role/*`).
- Add SQLite capability gap store (`data/gaps.db`) with automatic query logging from `execute_read_query` and explicit gap reporting tools (`log_capability_gap`, `summarize_capability_gaps`).
- Set active school year 2026-2027 (`school_year_id = 2026`) as default across Go and TypeScript tools (`search_classes`, `get_student_detail`, `get_daily_class_schedule`, `get_student_enrollment_history`, `get_absent_alerts`, `computeCenterMetrics`, `get_network_overview`).
- Fix `get_absent_alerts` attendance status matching (`absence`, `n_absence`), add center filter, and enrich with student/parent contact profiles.

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
