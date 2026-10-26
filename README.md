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

### Business Helper Tools (Class Management)

| Tool | Parameters | Description |
| --- | --- | --- |
| `get_daily_class_schedule` | `center_id`, `date`? | Retrieves daily class schedule, room assignment, teacher, and student headcount for a center. |
| `get_class_roster` | `class_id`, `session_date`? | Retrieves student roster and attendance status for a specific class session. |
| `get_student_attendance_history` | `student_id`, `class_id`?, `limit`? | Retrieves attendance history, absence reasons, and make-up session records for a student. |
| `get_absent_alerts` | `center_id`?, `consecutive_absent_count`? | Identifies students with consecutive absences for parent outreach. |
| `get_class_academic_summary` | `class_id`, `student_id`?, `test_id`? | Aggregates test scores, homework completion, and teacher session comments for a class or student. |
| `check_student_class_eligibility` | `student_id`, `class_id`? | Checks student enrollment status and overdue tuition balance before class entry. |

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
go build -o main .
```

Run tests with:

```sh
go test ./... -v
```

## Transport Modes & Command-Line Flags

The server supports two transport modes:

- **Stdio Transport (Default):** Communicates over standard input/output (`stdin`/`stdout`). Ideal for local IDE integration (Zed, Claude Desktop, Cursor) or SSH tunneling.
- **SSE HTTP Transport:** Listens on HTTP to accept remote network connections over Server-Sent Events (SSE). Ideal for cross-VPS connections (e.g., Hermes on VPS A calling MCP Server on VPS B).

### Command-line Flags:

- `--transport`: `stdio` (default) or `sse`.
- `--port`: HTTP port for SSE mode. Defaults to env `MCP_PORT`, then `8080`. The flag overrides the env var.

```sh
# Run locally using Stdio (default)
./main

# Run as SSE HTTP server listening on port 8080
./main --transport=sse --port=8080
```

---

## Deployment & Remote Client Configuration

### Option A: VPS Deployment via Systemd (SSE Mode)

To run the MCP server as a background service on **VPS B**:

1. **Build binary and place files on VPS B:**

   ```sh
   mkdir -p /opt/vietelite-mcp
   cp main .env /opt/vietelite-mcp/
   ```

2. **Create Systemd Service (`/etc/systemd/system/vietelite-mcp.service`):**

   ```ini
   [Unit]
   Description=VietElite Go MCP Server (SSE)
   After=network.target mysql.service

   [Service]
   Type=simple
   User=www-data
   WorkingDirectory=/opt/vietelite-mcp
   ExecStart=/opt/vietelite-mcp/main --transport=sse --port=8080
   Restart=always
   RestartSec=5
   EnvironmentFile=/opt/vietelite-mcp/.env

   [Install]
   WantedBy=multi-user.target
   ```

3. **Enable and start service:**

   ```sh
   sudo systemctl daemon-reload
   sudo systemctl enable vietelite-mcp
   sudo systemctl start vietelite-mcp
   sudo systemctl status vietelite-mcp
   ```

4. **Connect Remote Client (Hermes / Claude / Cursor on VPS A):**

   Add the server URL to your client config (e.g., `mcp_config.json`):

   ```json
   {
     "mcpServers": {
       "vietelite-db": {
         "url": "http://<VPS_B_IP>:8080/sse",
         "headers": {
           "Authorization": "Bearer <MCP_AUTH_TOKEN>"
         }
       }
     }
   }
   ```

---

### Option B: SSH Stdio Tunnel (Zero Network Exposure)

If you do not wish to expose HTTP ports to the Internet, connect from **VPS A** (Hermes) to **VPS B** via SSH tunnel over Stdio transport:

Config on **VPS A**:

```json
{
  "mcpServers": {
    "vietelite-db": {
      "command": "ssh",
      "args": [
        "-i", "/path/to/id_rsa",
        "user@<VPS_B_IP>",
        "/opt/vietelite-mcp/main"
      ]
    }
  }
}
```

---

### Option C: Local IDE Integration (Zed / Claude Desktop / Cursor)

#### Zed

Add an entry in `~/.config/zed/settings.json`:

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

Add to `claude_desktop_config.json`:

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