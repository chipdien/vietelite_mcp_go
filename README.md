# VietElite Go MCP Server

A **read-only Model Context Protocol (MCP) server written in Go** for the VietElite MySQL
microservices databases. It exposes schema inspection, safe read-only SQL execution, and
domain-specific lookup tools to any MCP-compatible LLM client (Zed, Claude Desktop, etc.)
over **Stdio transport**.

The server is designed to be safe by default: every query is validated before it reaches
the database, connections are opened as `READ ONLY`, and result sets are row-capped so an
LLM can never accidentally mutate data or exhaust its context window.

## Databases Exposed

| Env var | Default database | Contents |
| --- | --- | --- |
| `MYSQL_IDENTITY_DB` | `identity_service` | User accounts, student profiles, parents, OAuth tokens |
| `MYSQL_CLASS_DB` | `class_service` | Classes, courses, enrollments, schedules, attendance, test scores |
| `MYSQL_FINANCE_DB` | `finance_service` | Invoices, tuition items, receipts, wallets, transactions |
| `MYSQL_CRM_DB` | `crm_service` | Leads, sales activities, customer care logs, campaigns |

## MCP Tools

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

## Prerequisites

- **Go 1.22+** (see `go.mod`; the module builds with newer toolchains such as 1.26.x).
- **Network access to the VietElite MySQL databases** — host, port, and read-only
  credentials. If the databases run locally, a local MySQL instance on port `3306` is
  enough.
- The MySQL user only needs `SELECT` / metadata privileges; nothing in this server writes.

## Configuration

1. Copy the example environment file and edit it with your credentials:

   ```sh
   cd go-server
   cp .env.example .env
   ```

2. Set the following variables in `go-server/.env`:

   ```ini
   MYSQL_HOST=127.0.0.1
   MYSQL_PORT=3306
   MYSQL_USER=root
   MYSQL_PASSWORD=your-password-here
   MYSQL_IDENTITY_DB=identity_service
   MYSQL_CLASS_DB=class_service
   MYSQL_FINANCE_DB=finance_service
   MYSQL_CRM_DB=crm_service
   ```

   The values above are the built-in defaults; any variable you omit falls back to the
   default shown. `.env` is git-ignored — never commit real credentials.

   > **Note:** If you would rather not use a `.env` file, export the same `MYSQL_*`
   > variables in the environment of the process that launches the server (see the client
   > `env` blocks below). A `.env` file in the working directory takes precedence when
   > present.

## Build

From the repository root:

```sh
cd go-server && go build -o main .
```

This produces the `go-server/main` binary (git-ignored). Run the test suite with:

```sh
cd go-server && go test ./...
```

## Client Integration

The server speaks **Stdio**, so the client launches the binary and talks JSON-RPC over
stdin/stdout. Point each client's command at the absolute path of the built `main` binary.

### Zed

Add an entry under `context_servers` in your Zed settings
(`~/.config/zed/settings.json`, or **Zed → Settings → Open Settings**):

```json
{
  "context_servers": {
    "vietelite-go-mcp": {
      "command": {
        "path": "/Volumes/PHANANH/wwwroot/vietelite-mcp/go-server/main",
        "args": [],
        "env": {
          "MYSQL_HOST": "127.0.0.1",
          "MYSQL_PORT": "3306",
          "MYSQL_USER": "root",
          "MYSQL_PASSWORD": "your-password-here",
          "MYSQL_IDENTITY_DB": "identity_service",
          "MYSQL_CLASS_DB": "class_service",
          "MYSQL_FINANCE_DB": "finance_service",
          "MYSQL_CRM_DB": "crm_service"
        }
      }
    }
  }
}
```

If the `env` block is omitted, the server falls back to its own `.env` file.

### Claude Desktop

Add the same server to `claude_desktop_config.json`
(macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "vietelite-go-mcp": {
      "command": "/Volumes/PHANANH/wwwroot/vietelite-mcp/go-server/main",
      "args": [],
      "env": {
        "MYSQL_HOST": "127.0.0.1",
        "MYSQL_PORT": "3306",
        "MYSQL_USER": "root",
        "MYSQL_PASSWORD": "your-password-here",
        "MYSQL_IDENTITY_DB": "identity_service",
        "MYSQL_CLASS_DB": "class_service",
        "MYSQL_FINANCE_DB": "finance_service",
        "MYSQL_CRM_DB": "crm_service"
      }
    }
  }
}
```

Restart the client after editing its config. Both clients use the identical binary and
environment — only the surrounding JSON key (`context_servers` vs `mcpServers`) differs.

## Security Features

The server applies **defence in depth** so that no LLM tool call can modify data:

- **SELECT-only statements.** `execute_read_query` accepts only statements beginning with
  `SELECT`, `SHOW`, `DESCRIBE`/`DESC`, or `EXPLAIN`. Anything else is rejected with
  `only read-only queries ... are allowed`.
- **Multi-statement rejection.** The query is split on `;` *after* comments and string
  literals are stripped, so `SELECT 1; DROP TABLE users` is refused while a semicolon
  inside a literal (`name = 'a;b'`) is treated as data. Stacked statements error with
  `multiple statements are not allowed`.
- **Keyword blacklist.** A case-insensitive, whole-word scan rejects DML/DDL and
  exfiltration keywords: `INSERT`, `UPDATE`, `DELETE`, `DROP`, `ALTER`, `TRUNCATE`,
  `REPLACE`, `CREATE`, `RENAME`, `GRANT`, `REVOKE`, `SET`, `CALL`, `EXEC`, `EXECUTE`,
  `LOCK`, `UNLOCK`, plus `INTO OUTFILE`, `INTO DUMPFILE`, `LOAD_FILE`, and `FOR UPDATE`.
  Word boundaries mean identifiers like `updated_at` or `created_at` are **not** false
  positives.
- **Comment / literal stripping.** SQL comments and quoted literal bodies are removed
  before validation, so a keyword can't be hidden in a comment or a delimiter smuggled
  inside a string.
- **Automatic LIMIT capping.** `execute_read_query` rewrites or appends a trailing
  `LIMIT` (default 50, max 200) on `SELECT` queries only. An existing `LIMIT` above the
  cap is lowered; limits inside subqueries, literals, or comments are left untouched; and
  non-`SELECT` statements (`SHOW`/`DESCRIBE`/`EXPLAIN`) are never given a `LIMIT`.
- **Read-only via privileges, not flags.** The Go server relies on the SQL Guard above
  rather than a `SET SESSION ... READ ONLY` flag (the DSN in `db/connection.go` does not
  set one). The defence-in-depth is therefore: give the connection user **only `SELECT`
  and metadata privileges** on the four databases. Even if a guard check were bypassed,
  MySQL rejects writes at the account level. Do not grant `INSERT`/`UPDATE`/`DELETE`
  rights to this account.