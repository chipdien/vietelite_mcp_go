package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestRegisterResources(t *testing.T) {
	s := server.NewMCPServer(
		"Test MCP Server",
		"1.0.0",
		server.WithResourceCapabilities(false, true),
	)

	// Register all resources and ensure no panics
	RegisterResources(s)
}

func TestReferenceCache(t *testing.T) {
	cache := &referenceCache{
		items: make(map[string]cacheEntry),
	}

	uri := "reference://test"
	if _, ok := cache.get(uri); ok {
		t.Fatalf("expected cache miss for uninitialized URI")
	}

	content := "# Test Content"
	cache.set(uri, content)

	cached, ok := cache.get(uri)
	if !ok || cached != content {
		t.Fatalf("expected cache hit with %q, got ok=%v, content=%q", content, ok, cached)
	}

	// Test expired entry
	cache.items[uri] = cacheEntry{
		content:   content,
		updatedAt: time.Now().Add(-2 * time.Hour),
	}
	if _, ok := cache.get(uri); ok {
		t.Fatalf("expected cache miss for expired entry")
	}
}

func TestFormatCell(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want string
	}{
		{"nil value", nil, "-"},
		{"empty string", "", "-"},
		{"string with pipes and newlines", "Hello | World\nNew line\r", "Hello \\| World New line"},
		{"byte slice", []byte("Byte data | test"), "Byte data \\| test"},
		{"integer", 42, "42"},
		{"timestamp", time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC), "2026-11-01 10:00:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCell(tt.val)
			if got != tt.want {
				t.Errorf("formatCell(%v) = %q, want %q", tt.val, got, tt.want)
			}
		})
	}
}

func TestFormatServerCapabilities(t *testing.T) {
	cap := formatServerCapabilities()
	requiredSnippets := []string{
		"VietElite DB MCP Server Capabilities",
		"reference://centers",
		"reference://courses",
		"reference://expense-categories",
		"reference://pipeline-stages",
		"reference://accounts-chart",
		"schema://summary",
		"guide://tools-by-role/{role}",
		"gaps://report",
		"server://capabilities",
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(cap, snippet) {
			t.Errorf("formatServerCapabilities missing expected snippet: %q", snippet)
		}
	}
}

func TestFormatGapsAsMarkdown(t *testing.T) {
	// Empty gaps
	emptyReport := formatGapsAsMarkdown(nil)
	if !strings.Contains(emptyReport, "No gaps recorded yet") {
		t.Errorf("expected 'No gaps recorded yet' in empty gaps report")
	}

	// Populated gaps
	gaps := []map[string]any{
		{
			"id":                  1,
			"occurrence_count":    5,
			"source":              "agent",
			"database":            "finance",
			"question":            "Get unpaid invoices",
			"query_text":          "SELECT * FROM fin_invoices WHERE paid_status = 0",
			"suggested_tool_name": "get_unpaid_invoices",
		},
	}
	report := formatGapsAsMarkdown(gaps)
	if !strings.Contains(report, "get_unpaid_invoices") || !strings.Contains(report, "finance") {
		t.Errorf("expected gap data to be formatted into table, got: %s", report)
	}
}

func TestCapabilitiesResourceHandler(t *testing.T) {
	s := server.NewMCPServer("Test", "1.0.0")
	RegisterResources(s)

	req := mcp.ReadResourceRequest{
		Params: mcp.ReadResourceParams{
			URI: "server://capabilities",
		},
	}

	// Verify textResource helper
	res := textResource("server://capabilities", formatServerCapabilities())
	if len(res) != 1 {
		t.Fatalf("expected 1 resource content, got %d", len(res))
	}
	textContent, ok := mcp.AsTextResourceContents(res[0])
	if !ok {
		t.Fatalf("expected TextResourceContents type")
	}
	if textContent.URI != req.Params.URI {
		t.Errorf("expected URI %s, got %s", req.Params.URI, textContent.URI)
	}
	if !strings.Contains(textContent.Text, "Server Capabilities") {
		t.Errorf("expected server capabilities content")
	}
}
