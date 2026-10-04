// Package tools registers and implements the MCP tools exposed by the VietElite
// database server. The generic tools here are the four database-inspection
// primitives shared by all higher-level domain tools.
package tools

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"vietelite-mcp/go-server/db"
)

// serviceNames is the canonical set of database services exposed by the server,
// in display order. It mirrors the service identifiers accepted by the db
// package's connection pool manager.
var serviceNames = []string{"identity", "class", "finance", "crm"}

var validServices = map[string]bool{
	"identity": true,
	"class":    true,
	"finance":  true,
	"crm":      true,
}

// Recommended execution limits for ad-hoc read queries.
const (
	defaultLimit = 50
	maxLimit     = 200
)

// DBInfo describes a single database service in list_databases output.
type dbInfo struct {
	Service    string `json:"service"`
	Database   string `json:"database"`
	Status     string `json:"status"` // "connected" or "disconnected"
	TableCount int    `json:"tableCount"`
}

type listDatabasesResult struct {
	Databases []dbInfo `json:"databases"`
}

// tableInfo is a single row from information_schema.TABLES.
type tableInfo struct {
	Name    string `json:"name"`
	Rows    int64  `json:"rows"` // estimated row count reported by MySQL
	Comment string `json:"comment"`
}

type listTablesResult struct {
	Service    string      `json:"service"`
	Database   string      `json:"database"`
	TableCount int         `json:"tableCount"`
	Tables     []tableInfo `json:"tables"`
}

// columnInfo describes a column from information_schema.COLUMNS.
type columnInfo struct {
	Name         string `json:"name"`
	DataType     string `json:"dataType"`
	ColumnType   string `json:"columnType"`
	Nullable     bool   `json:"nullable"`
	Key          string `json:"key"` // PRI / UNI / MUL or ""
	DefaultValue any    `json:"default,omitempty"`
	Extra        string `json:"extra"`
	Comment      string `json:"comment"`
}

// foreignKeyInfo describes a single foreign-key constraint targeting another
// table, from information_schema.KEY_COLUMN_USAGE.
type foreignKeyInfo struct {
	Column           string `json:"column"`
	ConstraintName   string `json:"constraintName"`
	ReferencedTable  string `json:"referencedTable"`
	ReferencedColumn string `json:"referencedColumn"`
}

type describeTableResult struct {
	Service     string           `json:"service"`
	Database    string           `json:"database"`
	Table       string           `json:"table"`
	Columns     []columnInfo     `json:"columns"`
	ForeignKeys []foreignKeyInfo `json:"foreignKeys"`
}

type executeReadQueryResult struct {
	Service  string           `json:"service"`
	Database string           `json:"database"`
	Query    string           `json:"query"`
	RowCount int              `json:"rowCount"`
	Rows     []map[string]any `json:"rows"`
}

// RegisterGenericTools installs the four generic DB-inspection tools on the MCP
// server: list_databases, list_tables, describe_table, and execute_read_query.
func RegisterGenericTools(s *server.MCPServer) {
	dbDesc := "Database service name (identity, class, finance, crm)"

	s.AddTool(
		mcp.NewTool("list_databases",
			mcp.WithDescription("Returns list of 4 databases, connection status, and table counts."),
		),
		handleListDatabases,
	)

	s.AddTool(
		mcp.NewTool("list_tables",
			mcp.WithDescription("Lists the tables in a database service, with estimated row counts and comments."),
			mcp.WithString("database", mcp.Required(), mcp.Description(dbDesc)),
		),
		handleListTables,
	)

	s.AddTool(
		mcp.NewTool("describe_table",
			mcp.WithDescription("Describes the columns and foreign keys of a table in a database service."),
			mcp.WithString("database", mcp.Required(), mcp.Description(dbDesc)),
			mcp.WithString("table", mcp.Required(), mcp.Description("Name of the table to describe")),
		),
		handleDescribeTable,
	)

	s.AddTool(
		mcp.NewTool("execute_read_query",
			mcp.WithDescription("Executes a read-only SQL query against a database service with LIMIT enforcement."),
			mcp.WithString("database", mcp.Required(), mcp.Description(dbDesc)),
			mcp.WithString("query", mcp.Required(), mcp.Description("Read-only SQL query (SELECT, SHOW, DESCRIBE, EXPLAIN)")),
			mcp.WithNumber("limit", mcp.Description("Max rows returned (default 50, max 200)")),
		),
		handleExecuteReadQuery,
	)
}

// validateDatabase rejects any service name that is not one of the four known
// databases. It is the single point of truth for tool argument validation.
func validateDatabase(service string) error {
	if !validServices[service] {
		return fmt.Errorf("invalid database %q: must be one of %s", service, strings.Join(serviceNames, ", "))
	}
	return nil
}

// clampLimit bounds the requested row limit to [1, maxLimit], falling back to
// defaultLimit for unset or non-positive values.
func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

// errorResult wraps a failure as an MCP result with IsError set so the client
// sees a tool-level error rather than an unexpected server-side exception.
func errorResult(err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(err.Error())
}

func handleListDatabases(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	databases := make([]dbInfo, 0, len(serviceNames))
	for _, service := range serviceNames {
		databases = append(databases, inspectDatabase(ctx, service))
	}
	return mcp.NewToolResultStructuredOnly(listDatabasesResult{Databases: databases}), nil
}

// inspectDatabase checks whether a service's pool is reachable and, if so,
// reports its connected database name and table count. Connection failures are
// reported gracefully as status "disconnected" rather than as a hard error.
func inspectDatabase(ctx context.Context, service string) dbInfo {
	info := dbInfo{Service: service, Status: "disconnected"}

	pool, err := db.GetPool(service)
	if err != nil {
		return info
	}

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	rows, err := pool.QueryContext(pingCtx, "SELECT 1")
	if err != nil {
		return info
	}
	_ = rows.Close()

	info.Status = "connected"
	_ = pool.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&info.Database)
	info.TableCount = countTables(ctx, pool, info.Database)
	return info
}

// countTables returns how many user tables exist in a schema, or 0 if the count
// cannot be determined (for example when the database is unreachable).
func countTables(ctx context.Context, pool *sql.DB, schemaName string) int {
	var count int
	err := pool.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = ?", schemaName,
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

func handleListTables(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	service, err := r.RequireString("database")
	if err != nil {
		return errorResult(err), nil
	}
	if err := validateDatabase(service); err != nil {
		return errorResult(err), nil
	}

	pool, err := db.GetPool(service)
	if err != nil {
		return errorResult(err), nil
	}

	result := listTablesResult{Service: service}
	_ = pool.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&result.Database)

	rows, err := pool.QueryContext(ctx, `
		SELECT TABLE_NAME, TABLE_ROWS, TABLE_COMMENT
		  FROM information_schema.TABLES
		 WHERE TABLE_SCHEMA = ?
		 ORDER BY TABLE_NAME`, result.Database)
	if err != nil {
		return errorResult(fmt.Errorf("failed to list tables: %w", err)), nil
	}
	defer rows.Close()

	tables := make([]tableInfo, 0, 16)
	for rows.Next() {
		var t tableInfo
		var rowCount sql.NullInt64
		if err := rows.Scan(&t.Name, &rowCount, &t.Comment); err != nil {
			return errorResult(fmt.Errorf("failed to read table row: %w", err)), nil
		}
		if rowCount.Valid {
			t.Rows = rowCount.Int64
		}
		tables = append(tables, t)
	}
	if err := rows.Err(); err != nil {
		return errorResult(fmt.Errorf("failed to iterate tables: %w", err)), nil
	}

	result.TableCount = len(tables)
	result.Tables = tables
	return mcp.NewToolResultStructuredOnly(result), nil
}

func handleDescribeTable(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	service, err := r.RequireString("database")
	if err != nil {
		return errorResult(err), nil
	}
	if err := validateDatabase(service); err != nil {
		return errorResult(err), nil
	}

	table, err := r.RequireString("table")
	if err != nil {
		return errorResult(err), nil
	}
	if strings.TrimSpace(table) == "" {
		return errorResult(errors.New("argument \"table\" must not be empty")), nil
	}

	pool, err := db.GetPool(service)
	if err != nil {
		return errorResult(err), nil
	}

	result := describeTableResult{Service: service, Table: table}
	_ = pool.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&result.Database)

	columns, err := queryColumns(ctx, pool, result.Database, table)
	if err != nil {
		return errorResult(err), nil
	}
	foreignKeys, err := queryForeignKeys(ctx, pool, result.Database, table)
	if err != nil {
		return errorResult(err), nil
	}

	result.Columns = columns
	result.ForeignKeys = foreignKeys
	return mcp.NewToolResultStructuredOnly(result), nil
}

func queryColumns(ctx context.Context, pool *sql.DB, schemaName, table string) ([]columnInfo, error) {
	rows, err := pool.QueryContext(ctx, `
		SELECT COLUMN_NAME, DATA_TYPE, COLUMN_TYPE, IS_NULLABLE,
		       COALESCE(COLUMN_KEY, ''), COLUMN_DEFAULT,
		       COALESCE(EXTRA, ''), COALESCE(COLUMN_COMMENT, '')
		  FROM information_schema.COLUMNS
		 WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		 ORDER BY ORDINAL_POSITION`, schemaName, table)
	if err != nil {
		return nil, fmt.Errorf("failed to describe table %q: %w", table, err)
	}
	defer rows.Close()

	columns := make([]columnInfo, 0, 16)
	for rows.Next() {
		var c columnInfo
		var nullable string
		var defaultValue sql.NullString
		if err := rows.Scan(&c.Name, &c.DataType, &c.ColumnType, &nullable,
			&c.Key, &defaultValue, &c.Extra, &c.Comment); err != nil {
			return nil, fmt.Errorf("failed to read column of %q: %w", table, err)
		}
		c.Nullable = nullable == "YES"
		if defaultValue.Valid {
			c.DefaultValue = defaultValue.String
		}
		columns = append(columns, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate columns of %q: %w", table, err)
	}
	return columns, nil
}

func queryForeignKeys(ctx context.Context, pool *sql.DB, schemaName, table string) ([]foreignKeyInfo, error) {
	rows, err := pool.QueryContext(ctx, `
		SELECT COLUMN_NAME, CONSTRAINT_NAME, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
		  FROM information_schema.KEY_COLUMN_USAGE
		 WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND REFERENCED_TABLE_NAME IS NOT NULL
		 ORDER BY ORDINAL_POSITION`, schemaName, table)
	if err != nil {
		return nil, fmt.Errorf("failed to read foreign keys of %q: %w", table, err)
	}
	defer rows.Close()

	foreignKeys := make([]foreignKeyInfo, 0, 4)
	for rows.Next() {
		var f foreignKeyInfo
		if err := rows.Scan(&f.Column, &f.ConstraintName, &f.ReferencedTable, &f.ReferencedColumn); err != nil {
			return nil, fmt.Errorf("failed to read foreign key of %q: %w", table, err)
		}
		foreignKeys = append(foreignKeys, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate foreign keys of %q: %w", table, err)
	}
	return foreignKeys, nil
}

func handleExecuteReadQuery(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	service, err := r.RequireString("database")
	if err != nil {
		return errorResult(err), nil
	}
	if err := validateDatabase(service); err != nil {
		return errorResult(err), nil
	}

	query, err := r.RequireString("query")
	if err != nil {
		return errorResult(err), nil
	}

	limit := clampLimit(r.GetInt("limit", defaultLimit))

	if err := db.ValidateSQL(query); err != nil {
		return errorResult(err), nil
	}
	finalSQL := db.EnforceLimit(query, limit)

	pool, err := db.GetPool(service)
	if err != nil {
		return errorResult(err), nil
	}

	var database string
	_ = pool.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database)

	rows, err := pool.QueryContext(ctx, finalSQL)
	if err != nil {
		return errorResult(fmt.Errorf("query failed: %w", err)), nil
	}
	defer rows.Close()

	rowMaps, err := rowsToMaps(rows)
	if err != nil {
		return errorResult(fmt.Errorf("failed to read query results: %w", err)), nil
	}

	result := executeReadQueryResult{
		Service:  service,
		Database: database,
		Query:    finalSQL,
		RowCount: len(rowMaps),
		Rows:     rowMaps,
	}
	return mcp.NewToolResultStructuredOnly(result), nil
}

// rowsToMaps converts an SQL row set into a slice of column-name-keyed maps,
// normalizing driver value types so the result serializes cleanly to JSON.
func rowsToMaps(rows *sql.Rows) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to read column names: %w", err)
	}

	maps := make([]map[string]any, 0, 16)
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(map[string]any, len(columns))
		for i, col := range columns {
			row[col] = normalizeSQLValue(values[i])
		}
		maps = append(maps, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate rows: %w", err)
	}
	return maps, nil
}

// normalizeSQLValue converts driver-specific value types into their JSON-safe
// forms. The MySQL driver returns binary blobs and strings as []byte, which
// would otherwise be base64-encoded by encoding/json; converting to string keeps
// the value human-readable.
func normalizeSQLValue(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}
