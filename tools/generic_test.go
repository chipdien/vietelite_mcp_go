package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func requestWith(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: args},
	}
}

// assertErrorResult asserts that the result is a tool-level error whose message
// contains the supplied substring.
func assertErrorResult(t *testing.T, result *mcp.CallToolResult, wantSubstr string) {
	t.Helper()
	if result == nil {
		t.Fatal("expected a non-nil result, got nil")
	}
	if !result.IsError {
		t.Errorf("expected IsError=true, got success result")
	}
	text := resultText(t, result)
	if !strings.Contains(text, wantSubstr) {
		t.Errorf("error result message %q does not contain %q", text, wantSubstr)
	}
}

// resultText extracts the concatenated text content of a result, mirroring how a
// client would consume it.
func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestValidateDatabase(t *testing.T) {
	for _, svc := range []string{"identity", "class", "finance", "crm"} {
		if err := validateDatabase(svc); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", svc, err)
		}
	}
	for _, svc := range []string{"identity_service", "admin", "", "CRM", "c rem"} {
		if err := validateDatabase(svc); err == nil {
			t.Errorf("expected %q to be rejected", svc)
		}
	}
}

func TestClampLimit(t *testing.T) {
	tests := []struct {
		in   int
		want int
	}{
		{0, defaultLimit},
		{-5, defaultLimit},
		{50, 50},
		{200, 200},
		{5000, maxLimit},
	}
	for _, tc := range tests {
		if got := clampLimit(tc.in); got != tc.want {
			t.Errorf("clampLimit(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestHandleExecuteReadQuery_MissingDatabase(t *testing.T) {
	result, err := handleExecuteReadQuery(context.Background(), requestWith(map[string]any{
		"query": "SELECT * FROM users",
	}))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	assertErrorResult(t, result, "database")
}

func TestHandleExecuteReadQuery_InvalidDatabase(t *testing.T) {
	result, err := handleExecuteReadQuery(context.Background(), requestWith(map[string]any{
		"database": "admin",
		"query":    "SELECT * FROM users",
	}))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	assertErrorResult(t, result, "invalid database")
}

func TestHandleExecuteReadQuery_MissingQuery(t *testing.T) {
	result, err := handleExecuteReadQuery(context.Background(), requestWith(map[string]any{
		"database": "identity",
	}))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	assertErrorResult(t, result, "query")
}

func TestHandleExecuteReadQuery_WriteQueryRejected(t *testing.T) {
	// ValidateSQL runs before any DB access, so the write statement must be
	// rejected without touching a connection pool. The statement starts with
	// SELECT but smuggles a prohibited DML keyword, hitting the blacklist branch.
	result, err := handleExecuteReadQuery(context.Background(), requestWith(map[string]any{
		"database": "identity",
		"query":    "SELECT 1 DELETE FROM users",
	}))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	assertErrorResult(t, result, "prohibited")
}

func TestHandleExecuteReadQuery_UninitializedPool(t *testing.T) {
	// A syntactically valid read query on a valid service must fail gracefully
	// with a pool-not-initialized error rather than panicking.
	result, err := handleExecuteReadQuery(context.Background(), requestWith(map[string]any{
		"database": "finance",
		"query":    "SELECT * FROM invoices",
	}))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	assertErrorResult(t, result, "pool")
}

func TestHandleListTables_MissingDatabase(t *testing.T) {
	result, err := handleListTables(context.Background(), requestWith(nil))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	assertErrorResult(t, result, "database")
}

func TestHandleListTables_InvalidDatabase(t *testing.T) {
	result, err := handleListTables(context.Background(), requestWith(map[string]any{
		"database": "identity_service",
	}))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	assertErrorResult(t, result, "invalid database")
}

func TestHandleDescribeTable_MissingDatabase(t *testing.T) {
	result, err := handleDescribeTable(context.Background(), requestWith(map[string]any{
		"table": "users",
	}))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	assertErrorResult(t, result, "database")
}

func TestHandleDescribeTable_MissingTable(t *testing.T) {
	result, err := handleDescribeTable(context.Background(), requestWith(map[string]any{
		"database": "identity",
	}))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	assertErrorResult(t, result, "table")
}

func TestHandleDescribeTable_EmptyTable(t *testing.T) {
	result, err := handleDescribeTable(context.Background(), requestWith(map[string]any{
		"database": "identity",
		"table":    "  ",
	}))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	assertErrorResult(t, result, "must not be empty")
}

func TestHandleDescribeTable_InvalidDatabase(t *testing.T) {
	result, err := handleDescribeTable(context.Background(), requestWith(map[string]any{
		"database": "nope",
		"table":    "users",
	}))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	assertErrorResult(t, result, "invalid database")
}

func TestHandleListDatabases_WithoutPools(t *testing.T) {
	// With no pools initialized, list_databases must still succeed and report
	// every service as disconnected rather than failing.
	result, err := handleListDatabases(context.Background(), requestWith(nil))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatal("expected list_databases to succeed when pools are unavailable")
	}
	structured, ok := result.StructuredContent.(listDatabasesResult)
	if !ok {
		t.Fatalf("expected listDatabasesResult, got %T", result.StructuredContent)
	}
	if len(structured.Databases) != len(serviceNames) {
		t.Errorf("expected %d databases, got %d", len(serviceNames), len(structured.Databases))
	}
	for _, info := range structured.Databases {
		if info.Status != "disconnected" {
			t.Errorf("expected status disconnected for %q, got %q", info.Service, info.Status)
		}
		if info.TableCount != 0 {
			t.Errorf("expected tableCount 0 for %q, got %d", info.Service, info.TableCount)
		}
	}
}

func TestNormalizeSQLValue(t *testing.T) {
	if got := normalizeSQLValue([]byte("hello")); got != "hello" {
		t.Errorf("expected []byte converted to string, got %v", got)
	}
	if got := normalizeSQLValue(int64(42)); got != int64(42) {
		t.Errorf("expected int64 passed through unchanged, got %v", got)
	}
	if got := normalizeSQLValue(nil); got != nil {
		t.Errorf("expected nil passed through, got %v", got)
	}
}
