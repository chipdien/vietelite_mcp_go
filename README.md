# VietElite Go MCP Server

A **read-only Model Context Protocol (MCP) server written in Go** for the VietElite MySQL
microservices databases. It exposes schema inspection, safe read-only SQL execution, and
domain-specific business lookup tools to any MCP-compatible LLM client (Hermes, Zed, Claude Desktop, Cursor, etc.)
over **Stdio** or **HTTP SSE (Server-Sent Events) transport**.

The server is designed to be safe by default: every query is validated before it reaches
the database, connections are opened with read-only validation, and result sets are row-capped so an
LLM can never accidentally mutate data or exhaust its context window.

## Databases Exposed

| Env var | Default database | Contents |
| --- | --- | --- |
| `MYSQL_IDENTITY_DB` | `identity_service` | User accounts, student profiles, parents, OAuth tokens |
| `MYSQL_CLASS_DB` | `class_service` | Classes, courses, enrollments, schedules, attendance, test scores |
| `MYSQL_FINANCE_DB` | `finance_service` | Invoices, tuition items, receipts, wallets, transactions |
| `MYSQL_CRM_DB` | `crm_service` | Leads, sales activities, customer care logs, campaigns |

## MCP Tools

### Generic & Inspection Tools

| Tool | Parameters | Description |
| --- | --- | --- |
| `list_databases` | *(none)* | Lists the 4 service databases, connection status, and table counts. |
| `list_tables` | `database` | Lists tables with estimated row counts and comments. |
| `describe_table` | `database`, `table` | Returns columns, data types, primary keys, and foreign keys. |
| `execute_read_query` | `database`, `query`, `limit`? | Runs a validated read-only SQL query (limit default 50, max 200). |
| `search_students` | `keyword`, `limit`? | Searches students by name, ID, phone, email, or school. |
| `get_student_detail` | `student_id` | Aggregates profile, classes, attendance, scores, and tuition. |
| `search_classes` | `keyword`?, `center_id`?, `limit`? | Searches classes by name, code, course, or center. |
| `get_finance_summary` | `student_id`?, `class_id`?, `limit`? | Fetches invoices, receipts, and wallet balances. |

### Business Helper Tools (Operations & Academic)

| Tool | Parameters | Description |
| --- | --- | --- |
| `get_daily_class_schedule` | `center_id`, `date`? | Retrieves daily class schedule, room assignment, teacher, and student headcount for a center. |
| `get_class_roster` | `class_id`, `session_date`? | Retrieves student roster and attendance status for a specific class session. |
| `get_class_detail` | `class_id` | Retrieves comprehensive class info, schedule configs, room, teacher, course, fee, and active headcount. |
| `get_session_detail` | `session_id` | Retrieves single session details: lesson materials/documents, attendance breakdown, and homework stats. |
| `get_student_attendance_history` | `student_id`, `class_id`?, `limit`? | Retrieves attendance history, absence reasons, and make-up session records for a student. |
| `get_absent_alerts` | `center_id`?, `consecutive_absent_count`? | Identifies students with consecutive absences for parent outreach. |
| `list_students_at_risk` | `class_id`, `threshold_score`?, `min_absences`?, `limit`? | Flags students at academic/attendance risk based on absences, missing homework, and low scores. |
| `list_ungraded_homework` | `class_id`, `from`?, `to`?, `limit`? | Lists homework submissions in a class that are awaiting grading or score entry. |
| `get_homework_submissions` | `session_id` | Retrieves all student homework submissions, completion flags, scores, and comments for a session. |
| `get_class_academic_summary` | `class_id`, `student_id`?, `test_id`? | Aggregates test scores, homework completion, and teacher session comments for a class or student. |
| `check_student_class_eligibility` | `student_id`, `class_id`? | Checks student enrollment status and overdue tuition balance before class entry. |

### Finance & Tuition Tools

| Tool | Parameters | Description |
| --- | --- | --- |
| `get_invoice_detail` | `invoice_id` | Full breakdown of an invoice: line items, discounts, amounts, and allocated payment receipts. |
| `list_unpaid_invoices` | `center_id`?, `from`?, `to`?, `min_days_overdue`?, `limit`? | Lists unpaid/partially paid tuition invoices with overdue days, debt labels, and student profiles. |
| `find_unallocated_receipts` | `center_id`?, `from`?, `to`?, `limit`? | Finds receipts with unallocated balance ("tiền treo") not yet applied to invoices. |
| `get_class_debt_summary` | `class_id`, `min_days_overdue`?, `limit`? | Debt summary for a class: total billed, paid, outstanding, and list of unpaid students. |
| `get_student_payment_history` | `student_id`, `from`?, `to`?, `limit`? | Full payment history for a student with receipt allocation tracing. |

## MCP Resources

MCP Resources expose static files and frequently referenced master/catalog data with in-memory caching to eliminate repetitive DB queries and conserve LLM context tokens.

| Scheme | URI | Source | Description |
| --- | --- | --- | --- |
| `schema://` | `schema://identity_service`<br>`schema://class_service`<br>`schema://finance_service`<br>`schema://crm_service` | `docs/db-schema/*.md` | Complete database schema documentation per service. |
| `schema://` | `schema://summary` | `docs/db-schema/SUMMARY.md` | High-level 4-service relational map and entity relationships. |
| `reference://` | `reference://centers` | `ve26_centers` (identity) | Master center list (ID, code, name, phone, address, admissions hotline). |
| `reference://` | `reference://courses` | `edu_courses` (class) | Course catalog (ID, name, grade, level, domain_id). |
| `reference://` | `reference://expense-categories` | `fin_expense_categories` (finance) | Active financial expense categories (ID, code, name, parent_id). |
| `reference://` | `reference://pipeline-stages` | `crm_pipeline_stages` (crm) | CRM opportunity pipeline stages (ID, pipeline_id, name, code, order, SLA). |
| `reference://` | `reference://accounts-chart` | `fin_accounts` (finance) | Chart of accounts for accounting queries. |
| `guide://` | `guide://tools-by-role/{role}` | `docs/tools-by-role-*.md` | Role-based tool and workflow guides (`class-operations`, `teaching-assistant`, `teacher`, `center-manager`, `coo`, `accountant`, `ceo`). |
| `gaps://` | `gaps://report` | SQLite Gap Store | Dynamic report of logged tool capability gaps to prioritize development. |
| `server://` | `server://capabilities` | Runtime Metadata | Server version, transport modes, safety guards, and catalog index. |

## Prerequisites

- **Go 1.22+** (see `go.mod`; the module builds with newer toolchains such as 1.26.x).
- **Network access to the VietElite MySQL databases** — host, port, and read-only credentials.
- The MySQL user only needs `SELECT` / metadata privileges; nothing in this server writes.

## Configuration

1. Copy the example environment file and edit it with your credentials:

   ```sh
   cp .env.example .env
   ```

2. Set the following variables in `.env`:

   ```ini
   MYSQL_HOST=127.0.0.1
   MYSQL_PORT=3306
   MYSQL_USER=root
   MYSQL_PASSWORD=your-password-here
   MYSQL_IDENTITY_DB=identity_service
   MYSQL_CLASS_DB=class_service
   MYSQL_FINANCE_DB=finance_service
   MYSQL_CRM_DB=crm_service
   MCP_AUTH_TOKEN=your-secret-token-here
   ```

   `.env` is git-ignored — never commit real credentials.

   > **Note:** Environment variables exported in the shell take precedence over `.env`.

### Authentication (SSE mode)

When `MCP_AUTH_TOKEN` is set, the HTTP transport requires a **Bearer token** on every request
(`/sse` and `/message`):

```sh
curl -H "Authorization: Bearer $MCP_AUTH_TOKEN" http://<HOST>:8080/sse
```

Requests with a missing or invalid token are rejected with `401 Unauthorized` and logged
under the `AUTH` event. If `MCP_AUTH_TOKEN` is left empty, the server runs **unauthenticated**
(only acceptable behind an SSH tunnel or a local-only bind address).

## Build

From the server directory:

```sh
# Development build (version reads "dev")
go build -o main .

# Release-style build with version metadata baked in
go build -trimpath \
  -ldflags "-s -w \
    -X vietelite-mcp/go-server/version.Version=v1.2.3 \
    -X vietelite-mcp/go-server/version.Commit=$(git rev-parse --short HEAD) \
    -X vietelite-mcp/go-server/version.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o main .
```

Run tests with:

```sh
# Unit and integration tests
go test ./... -v

# Format and vet (the release workflow enforces both)
gofmt -l . && go vet ./...
```

## Releases & Versioning

Pushing a tag that starts with `v` builds and publishes release binaries
automatically via `.github/workflows/release.yml`:

```sh
git tag v1.0.0
git push origin v1.0.0
```

The workflow runs tests first, then cross-compiles for six targets
(`linux`/`darwin`/`windows` × `amd64`/`arm64`), publishes a `checksums.txt`,
and attaches everything to a GitHub Release. Version and commit metadata are
injected with `-ldflags`, so `./main --version` reports exactly what was built.

Asset naming is stable and is what the updater looks for:

```
vietelite-mcp_<tag>_<goos>_<goarch>[.exe]
vietelite-mcp_v1.0.0_linux_amd64
vietelite-mcp_v1.0.0_darwin_arm64
vietelite-mcp_v1.0.0_windows_amd64.exe
```

Tags containing a hyphen (e.g. `v1.1.0-rc.1`) are published as prereleases.
You can also re-run the workflow manually from the Actions tab with an existing
tag.

### Release notes

When `go-server/CHANGELOG.md` exists, the workflow extracts the section whose
heading matches the tag and uses it as the release body. Otherwise it falls back
to a one-line description. Keep entries under a `## <tag>` heading:

```markdown
## v1.1.0

- Add `--auto-update` support.
- Fix SSE host binding.
```

## Updating the Server

The binary can update itself from GitHub Releases. Every download is verified
against the published `checksums.txt` before it replaces anything, and the swap
is atomic (a temp file in the same directory is renamed over the executable).

| Command | Behaviour |
| --- | --- |
| `./main --version` | Print version, commit, build date, and platform. |
| `./main --check-update` | Query GitHub. Exit `0` = current, `2` = update available, `1` = error. |
| `./main --update` | Download, verify, and install the newest release. |
| `MCP_AUTO_UPDATE=1 ./main` | Check and install on startup, before serving requests. |

Examples:

```sh
# Manual check
./main --check-update

# Manual update (restart afterwards)
./main --update
sudo systemctl restart vietelite-mcp

# Automatic update on start, then serve normally
MCP_AUTO_UPDATE=1 ./main --transport=sse
```

### Environment variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `MCP_AUTO_UPDATE` | `0` | Set to `1`/`true` to check and install updates on startup. |
| `MCP_UPDATE_REPO` | `chipdien/vietelite_mcp` | Override the GitHub repo for forks. |
| `MCP_UPDATE_PRERELEASE` | `0` | Allow updating to prerelease tags. |
| `MCP_GITHUB_TOKEN` | *(empty)* | Optional token; only needed to raise the anonymous API rate limit. |
| `MCP_UPDATE_TARGET` | *(running exe)* | Replace this path instead of `os.Executable()`. Useful behind wrappers. |

> **Stdio note:** update notices are written to `stderr` and the log file only —
> never `stdout` — because `stdout` carries the MCP JSON-RPC protocol and any
> stray output would corrupt the stream.

### Self-update behaviour on each platform

- **Linux / macOS:** the new binary is written to a temp file next to the
executable and renamed over it. The running process keeps its old inode, so the
current session is unaffected; restart to run the new build.
- **Windows:** a running `.exe` cannot be replaced. The new binary is staged as
`main.exe.new` and the command explains the manual swap.
- **Permissions:** the directory containing the binary must be writable by the
user running `--update` (e.g. `/opt/vietelite-mcp` for the systemd service).
  If it is not, the update fails cleanly and leaves the existing binary intact.

For a systemd deployment, run the update as the service user, then restart:

```sh
sudo -u www-data /opt/vietelite-mcp/main --update
sudo systemctl restart vietelite-mcp
```

## Transport Modes & Command-Line Flags

The server supports two transport modes:

- **Stdio Transport (Default):** Communicates over standard input/output (`stdin`/`stdout`). Ideal for local IDE integration (Zed, Claude Desktop, Cursor) or SSH tunneling.
- **SSE HTTP Transport:** Listens on HTTP to accept remote network connections over Server-Sent Events (SSE). Ideal for cross-VPS connections (e.g., Hermes on VPS A calling MCP Server on VPS B).

### Command-line Flags:

- `--transport`: `stdio` (default) or `sse`.
- `--host`: Bind address for SSE mode. Defaults to env `MCP_HOST`, then `0.0.0.0` (use `127.0.0.1` for local-only, e.g. behind a reverse proxy). The flag overrides the env var.
- `--port`: HTTP port for SSE mode. Defaults to env `MCP_PORT`, then `8080`. The flag overrides the env var.
- `--version`: Print version, commit, build date, and platform, then exit.
- `--check-update`: Query GitHub for a newer release and exit (`0` current, `2` available, `1` error).
- `--update`: Install the newest GitHub release and exit. Restart to apply.

```sh
# Run locally using Stdio (default)
./main

# Run as SSE HTTP server listening on port 8080
./main --transport=sse --port=8080
```

---

## Deployment & Remote Client Configuration

### Option A: VPS Deployment via Systemd (SSE Mode)

Deploy the MCP server as a background service on Ubuntu/Debian VPS.

#### 1. Build the binary

**Option 1A: Build on VPS** (requires Go 1.26+)
```sh
sudo apt update && sudo apt install -y git
# Install Go from https://go.dev/dl/ (apt often has older versions)
git clone https://github.com/chipdien/vietelite_mcp.git
cd vietelite_mcp/go-server
go build -o main .
```

**Option 1B: Cross-compile on your Mac and upload**
```sh
cd go-server
GOOS=linux GOARCH=amd64 go build -o main .   # Use GOARCH=arm64 for ARM VPS
scp main user@<VPS_IP>:/tmp/
```

#### 2. Place files in `/opt/vietelite-mcp`

```sh
sudo mkdir -p /opt/vietelite-mcp/logs
sudo cp main /opt/vietelite-mcp/            # from build directory or /tmp/
sudo cp .env.example /opt/vietelite-mcp/.env
sudo chown -R www-data:www-data /opt/vietelite-mcp
sudo chmod 600 /opt/vietelite-mcp/.env
```

Write permissions on `logs/` are required — the server writes to `logs/mcp.log` (relative to `WorkingDirectory`).

#### 3. Configure `/opt/vietelite-mcp/.env`

```sh
sudo nano /opt/vietelite-mcp/.env
```

```ini
MYSQL_HOST=s5.pvmedia.vn
MYSQL_PORT=3306
MYSQL_USER=agent_mcp
MYSQL_PASSWORD=<your_mysql_password>
MYSQL_IDENTITY_DB=identity_service
MYSQL_CLASS_DB=class_service
MYSQL_FINANCE_DB=finance_service
MYSQL_CRM_DB=crm_service
MCP_AUTH_TOKEN=<generate_with_openssl_rand_-hex_24>
MCP_HOST=0.0.0.0              # Use 127.0.0.1 if behind nginx/Caddy
MCP_PORT=8080
```

- **Never commit `.env` to git** — it is already in `.gitignore`.
- Generate a secure token: `openssl rand -hex 24`
- If `MYSQL_HOST` is a remote server, ensure that server's firewall and MySQL user grants allow connections from this VPS IP.

#### 4. Create systemd service

```sh
sudo nano /etc/systemd/system/vietelite-mcp.service
```

```ini
[Unit]
Description=VietElite Go MCP Server (SSE)
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/vietelite-mcp
EnvironmentFile=/opt/vietelite-mcp/.env
ExecStart=/opt/vietelite-mcp/main --transport=sse
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Host and port are read from `.env` (`MCP_HOST`, `MCP_PORT`), so no need to pass flags.

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now vietelite-mcp
sudo systemctl status vietelite-mcp
```

View logs:
```sh
journalctl -u vietelite-mcp -f
# or
tail -f /opt/vietelite-mcp/logs/mcp.log
```

#### 5. Verify the server

```sh
# Without token, should return 401 Unauthorized
curl -i http://127.0.0.1:8080/sse

# With valid token, should see SSE stream "event: endpoint"
curl -N -H "Authorization: Bearer <your_token>" http://127.0.0.1:8080/sse
```

#### 6. Open access for MCPHub

**Same VPS or SSH tunnel:** Set `MCP_HOST=127.0.0.1` in `.env`, MCPHub points to `http://127.0.0.1:8080/sse`.

**MCPHub on a different server:** Allow only MCPHub's IP:
```sh
sudo ufw allow from <MCPHUB_IP> to any port 8080 proto tcp
```

**Behind reverse proxy with HTTPS** (e.g., `https://mcp-db.vietelite.link`):

Set `MCP_HOST=127.0.0.1` in `.env`, then configure Caddy:
```
mcp-db.vietelite.link {
    reverse_proxy 127.0.0.1:8080 {
        flush_interval -1
    }
}
```

For nginx, add `proxy_buffering off;` to preserve SSE streaming.

#### 7. Update to a new version

**Using the built-in updater** (no Go toolchain needed on the VPS):

```sh
sudo -u www-data /opt/vietelite-mcp/main --check-update   # is there a newer release?
sudo -u www-data /opt/vietelite-mcp/main --update         # download, verify, install
sudo systemctl restart vietelite-mcp
```

To have the server pull updates on its own, set `MCP_AUTO_UPDATE=1` in
`/opt/vietelite-mcp/.env`. It checks before serving requests and logs the
result under `AUTO_UPDATE`; restart is still required to run the new build.

**Building from source** (when you need an unreleased commit):

```sh
cd ~/vietelite_mcp
git pull
cd go-server
go build -o main .
sudo systemctl stop vietelite-mcp
sudo cp main /opt/vietelite-mcp/
sudo systemctl start vietelite-mcp
```

---

### MCPHub / Remote Client Configuration

Add the server to your MCP client config (MCPHub, Hermes, Claude Desktop, Cursor):

```json
{
  "mcpServers": {
    "vietelite-db": {
      "url": "http://<VPS_IP>:8080/sse",
      "headers": {
        "Authorization": "Bearer <MCP_AUTH_TOKEN>"
      }
    }
  }
}
```

If using a domain behind reverse proxy:
```json
{
  "mcpServers": {
    "vietelite-db": {
      "url": "https://mcp-db.vietelite.link/sse",
      "headers": {
        "Authorization": "Bearer <MCP_AUTH_TOKEN>"
      }
    }
  }
}
```

---

### Option B: SSH Stdio Tunnel (Zero Network Exposure)

Connect from a remote client to the MCP server via SSH without exposing HTTP ports:

```json
{
  "mcpServers": {
    "vietelite-db": {
      "command": "ssh",
      "args": [
        "-i", "/path/to/id_rsa",
        "user@<VPS_IP>",
        "/opt/vietelite-mcp/main"
      ]
    }
  }
}
```

No auth token is needed for stdio transport — SSH provides authentication and encryption.

---

### Option C: Local IDE Integration (Zed / Claude Desktop / Cursor)

For local development, run the server in stdio mode (no HTTP, no auth needed):

#### Zed

`~/.config/zed/settings.json`:

```json
{
  "context_servers": {
    "vietelite-go-mcp": {
      "command": {
        "path": "/path/to/go-server/main",
        "args": []
      }
    }
  }
}
```

#### Claude Desktop

`claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "vietelite-go-mcp": {
      "command": "/path/to/go-server/main",
      "args": []
    }
  }
}
```

---

## Known Limitations

- **Tool annotations:** All tools currently report `readOnlyHint: false, destructiveHint: true`. This is semantically incorrect — the tools only read data. MCP clients may prompt unnecessarily before calling. A future update will add `WithReadOnlyHintAnnotation(true)` when registering tools.
- **Reverse proxy base URL:** When deployed behind a reverse proxy with a custom domain, the `/message` endpoint URL returned by SSE may use the wrong host. If you encounter this, file an issue or add `MCP_BASE_URL` support.

---

## Security Features

The server applies **defence in depth** so that no LLM tool call can modify data:

- **SELECT-only statements.** `execute_read_query` accepts only statements beginning with
  `SELECT`, `SHOW`, `DESCRIBE`/`DESC`, or `EXPLAIN`. Anything else is rejected with
  `only read-only queries ... are allowed`.
- **Multi-statement rejection.** The query is split on `;` *after* comments and string
  literals are stripped. Stacked statements error with `multiple statements are not allowed`.
- **Keyword blacklist.** A case-insensitive, whole-word scan rejects DML/DDL and
  exfiltration keywords: `INSERT`, `UPDATE`, `DELETE`, `DROP`, `ALTER`, `TRUNCATE`,
  `REPLACE`, `CREATE`, `RENAME`, `GRANT`, `REVOKE`, `SET`, `CALL`, `EXEC`, `EXECUTE`,
  `LOCK`, `UNLOCK`, plus `INTO OUTFILE`, `INTO DUMPFILE`, `LOAD_FILE`, and `FOR UPDATE`.
- **Comment / literal stripping.** SQL comments and quoted literal bodies are removed
  before validation.
- **Automatic LIMIT capping.** `execute_read_query` rewrites or appends a trailing
  `LIMIT` (default 50, max 200) on `SELECT` queries only.
- **Read-only via privileges.** Connection users should be granted **only `SELECT` and metadata privileges** on the target databases.


## New Tools (Capability Gap Addressing)

### Payment & Enrollment

| Tool | Description |
|------|-------------|
| `get_student_payment_history` | Full payment history: invoices with allocated receipts (which receipt paid which invoice, when). |
| `get_student_enrollment_history` | Class enrollment history: current classes + action log (joined/left/transferred). |
| `get_class_debt_summary` | Tuition debt for a class: total billed/paid/outstanding + list of unpaid students. |

### Gap Tracking

| Tool | Description |
|------|-------------|
| `log_capability_gap` | Explicitly record when no suitable tool exists. Agent reports unmet needs. |
| `summarize_capability_gaps` | Query recorded gaps by frequency to prioritize new tool development. |

## Resources

| URI | Description |
|-----|-------------|
| `schema://identity_service` | Identity service database schema (markdown) |
| `schema://class_service` | Class service database schema (markdown) |
| `schema://finance_service` | Finance service database schema (markdown) |
| `schema://crm_service` | CRM service database schema (markdown) |
| `schema://summary` | Summary of all 4 services |
| `gaps://report` | Capability gaps report (markdown table, updated live) |
| `guide://tools-by-role/{role}` | Tool recommendations by role (7 roles) |

Gap tracking runs automatically: every `execute_read_query` call is logged to `data/gaps.db`.
