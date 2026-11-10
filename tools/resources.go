package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"vietelite_mcp_go/db"
	"vietelite_mcp_go/logger"
	"vietelite_mcp_go/version"
)

// referenceCache holds in-memory cached markdown representations of lookup tables.
type referenceCache struct {
	sync.RWMutex
	items map[string]cacheEntry
}

type cacheEntry struct {
	content   string
	updatedAt time.Time
}

var refCache = &referenceCache{
	items: make(map[string]cacheEntry),
}

const refCacheTTL = 1 * time.Hour

func (c *referenceCache) get(uri string) (string, bool) {
	c.RLock()
	defer c.RUnlock()
	entry, ok := c.items[uri]
	if !ok || time.Since(entry.updatedAt) > refCacheTTL {
		return "", false
	}
	return entry.content, true
}

func (c *referenceCache) set(uri, content string) {
	c.Lock()
	defer c.Unlock()
	c.items[uri] = cacheEntry{
		content:   content,
		updatedAt: time.Now(),
	}
}

// textResource creates a single TextResourceContents response.
func textResource(uri, text string) []mcp.ResourceContents {
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      uri,
			MIMEType: "text/markdown",
			Text:     text,
		},
	}
}

// RegisterResources registers all MCP resources:
// 1. schema://* (static database schema markdown files)
// 2. reference://* (cached lookup data from MySQL databases)
// 3. guide://tools-by-role/{role} (role-based workflow guides)
// 4. gaps://report (dynamically recorded capability gaps)
// 5. server://capabilities (server metadata, limits, safety guarantees)
func RegisterResources(s *server.MCPServer) {
	registerSchemaResources(s)
	registerReferenceResources(s)
	registerGuideResources(s)
	registerGapsReportResource(s)
	registerCapabilitiesResource(s)
}

func registerSchemaResources(s *server.MCPServer) {
	schemaFiles := map[string]string{
		"schema://identity_service": "docs/db-schema/identity_service.md",
		"schema://class_service":    "docs/db-schema/class_service.md",
		"schema://finance_service":  "docs/db-schema/finance_service.md",
		"schema://crm_service":      "docs/db-schema/crm_service.md",
		"schema://summary":          "docs/db-schema/SUMMARY.md",
	}

	for uri, filePath := range schemaFiles {
		localPath := filePath
		resourceURI := uri
		s.AddResource(
			mcp.NewResource(resourceURI, fmt.Sprintf("Database schema: %s", resourceURI),
				mcp.WithResourceDescription(fmt.Sprintf("Read-only schema documentation from %s", localPath)),
				mcp.WithMIMEType("text/markdown"),
			),
			func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				content, err := os.ReadFile(localPath)
				if err != nil {
					logger.Error("RESOURCE_READ", "Failed to read %s: %v", localPath, err)
					return nil, fmt.Errorf("failed to read schema file %s: %w", localPath, err)
				}
				return textResource(resourceURI, string(content)), nil
			},
		)
	}
}

type refDef struct {
	uri         string
	name        string
	description string
	service     string
	query       string
}

func registerReferenceResources(s *server.MCPServer) {
	refs := []refDef{
		{
			uri:         "reference://centers",
			name:        "Reference: Centers (Cơ sở VietElite)",
			description: "Danh sách cơ sở học (ve26_centers): ID, mã, tên, điện thoại, địa chỉ, hotline tuyển sinh",
			service:     "identity",
			query:       "SELECT id, code, name, phone, address, phone_tuyensinh FROM ve26_centers ORDER BY id ASC",
		},
		{
			uri:         "reference://courses",
			name:        "Reference: Courses (Khóa học)",
			description: "Danh mục khóa học (edu_courses): ID, tên khóa, khối lớp (grade), cấp độ (level), domain_id",
			service:     "class",
			query:       "SELECT id, name, grade, level, domain_id, show_able FROM edu_courses ORDER BY grade ASC, id ASC",
		},
		{
			uri:         "reference://expense-categories",
			name:        "Reference: Expense Categories (Danh mục khoản chi)",
			description: "Danh mục phân loại khoản chi đang hoạt động (fin_expense_categories)",
			service:     "finance",
			query:       "SELECT id, code, name, parent_id, is_active FROM fin_expense_categories WHERE is_active = 1 ORDER BY id ASC",
		},
		{
			uri:         "reference://pipeline-stages",
			name:        "Reference: CRM Pipeline Stages (Giai đoạn cơ hội)",
			description: "Các giai đoạn bán hàng/tuyển sinh CRM (crm_pipeline_stages): ID, pipeline_id, tên, thứ tự, SLA",
			service:     "crm",
			query:       "SELECT id, pipeline_id, name, code, `order`, sla_hours, system_action FROM crm_pipeline_stages ORDER BY pipeline_id ASC, `order` ASC",
		},
		{
			uri:         "reference://accounts-chart",
			name:        "Reference: Chart of Accounts (Cây tài khoản)",
			description: "Cây tài khoản kế toán đang hoạt động (fin_accounts): ID, mã tài khoản, tên, loại",
			service:     "finance",
			query:       "SELECT id, account_code, name, type, parent_id, is_active FROM fin_accounts WHERE is_active = 1 ORDER BY account_code ASC",
		},
	}

	for _, ref := range refs {
		def := ref
		s.AddResource(
			mcp.NewResource(def.uri, def.name,
				mcp.WithResourceDescription(def.description),
				mcp.WithMIMEType("text/markdown"),
			),
			func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				markdown, err := getOrFetchReference(def.uri, def.service, def.query, def.name, def.description)
				if err != nil {
					logger.Error("RESOURCE_READ", "Failed to fetch reference %s: %v", def.uri, err)
					return nil, fmt.Errorf("failed to fetch reference data: %w", err)
				}
				return textResource(def.uri, markdown), nil
			},
		)
	}
}

func getOrFetchReference(uri, service, query, title, description string) (string, error) {
	if cached, ok := refCache.get(uri); ok {
		return cached, nil
	}

	markdown, err := fetchTableAsMarkdown(service, query, title, description)
	if err != nil {
		refCache.RLock()
		stale, ok := refCache.items[uri]
		refCache.RUnlock()
		if ok && stale.content != "" {
			logger.Warn("RESOURCE_CACHE", "Serving stale cache for %s due to query error: %v", uri, err)
			return stale.content, nil
		}
		return "", err
	}

	refCache.set(uri, markdown)
	return markdown, nil
}

func fetchTableAsMarkdown(service, query, title, description string) (string, error) {
	pool, err := db.GetPool(service)
	if err != nil {
		return "", fmt.Errorf("database pool for '%s' unavailable: %w", service, err)
	}

	rows, err := pool.Query(query)
	if err != nil {
		return "", fmt.Errorf("query execution failed on '%s': %w", service, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("failed to read table columns: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))
	if description != "" {
		sb.WriteString(fmt.Sprintf("> %s\n\n", description))
	}

	// Table Header
	sb.WriteString("| ")
	for _, col := range cols {
		sb.WriteString(col)
		sb.WriteString(" | ")
	}
	sb.WriteString("\n| ")
	for range cols {
		sb.WriteString("--- | ")
	}
	sb.WriteString("\n")

	// Scan destination setup
	values := make([]any, len(cols))
	scanArgs := make([]any, len(cols))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	rowCount := 0
	for rows.Next() {
		if err := rows.Scan(scanArgs...); err != nil {
			return "", fmt.Errorf("row scan error: %w", err)
		}
		rowCount++
		sb.WriteString("| ")
		for _, val := range values {
			sb.WriteString(formatCell(val))
			sb.WriteString(" | ")
		}
		sb.WriteString("\n")
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("rows iteration error: %w", err)
	}

	sb.WriteString(fmt.Sprintf("\n*Total records: %d | Cached at: %s*\n", rowCount, time.Now().Format("2006-01-02 15:04:05 MST")))
	return sb.String(), nil
}

func formatCell(v any) string {
	if v == nil {
		return "-"
	}
	switch val := v.(type) {
	case []byte:
		s := strings.TrimSpace(string(val))
		if s == "" {
			return "-"
		}
		return cleanCell(s)
	case time.Time:
		return val.Format("2006-01-02 15:04:05")
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", val))
		if s == "" {
			return "-"
		}
		return cleanCell(s)
	}
}

func cleanCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

func registerGuideResources(s *server.MCPServer) {
	roles := []string{
		"class-operations",
		"teaching-assistant",
		"teacher",
		"center-manager",
		"coo",
		"accountant",
		"ceo",
	}

	for i, role := range roles {
		localPath := fmt.Sprintf("docs/tools-by-role-%02d-%s.md", i+1, role)
		uri := fmt.Sprintf("guide://tools-by-role/%s", role)
		resourceURI := uri
		filePath := localPath
		roleName := role

		s.AddResource(
			mcp.NewResource(resourceURI, fmt.Sprintf("Tools Guide: %s", roleName),
				mcp.WithResourceDescription(fmt.Sprintf("Recommended tools and workflows for %s role", roleName)),
				mcp.WithMIMEType("text/markdown"),
			),
			func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				content, err := os.ReadFile(filePath)
				if err != nil {
					logger.Error("RESOURCE_READ", "Guide not found: %s", filePath)
					return textResource(resourceURI, fmt.Sprintf("# Guide Not Found\n\nFile `%s` does not exist.", filePath)), nil
				}
				return textResource(resourceURI, string(content)), nil
			},
		)
	}
}

func registerGapsReportResource(s *server.MCPServer) {
	const uri = "gaps://report"
	s.AddResource(
		mcp.NewResource(uri, "Capability Gaps Report",
			mcp.WithResourceDescription("Summarizes recorded capability gaps from SQL queries to prioritize future tool development."),
			mcp.WithMIMEType("text/markdown"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			gaps, err := db.QueryGaps(1, 100)
			if err != nil {
				logger.Error("RESOURCE_READ", "Failed to query gaps: %v", err)
				return nil, fmt.Errorf("failed to query gaps: %w", err)
			}
			markdown := formatGapsAsMarkdown(gaps)
			return textResource(uri, markdown), nil
		},
	)
}

func formatGapsAsMarkdown(gaps []map[string]any) string {
	var sb strings.Builder
	sb.WriteString("# Capability Gaps Report\n\n")
	sb.WriteString("This report lists tool gaps recorded from `execute_read_query` usage and explicit `log_capability_gap` calls, ordered by occurrence frequency.\n\n")

	if len(gaps) == 0 {
		sb.WriteString("**No gaps recorded yet.**\n")
		return sb.String()
	}

	sb.WriteString("| # | Occurrences | Source | Database | Question | Query (first 60 chars) | Suggested Tool |\n")
	sb.WriteString("|---|-------------|--------|----------|----------|------------------------|----------------|\n")

	for i, gap := range gaps {
		count := gap["occurrence_count"]
		source := gap["source"]
		database := gap["database"]
		question := gap["question"]
		queryText := gap["query_text"]
		suggestedTool := gap["suggested_tool_name"]

		querySnippet := fmt.Sprintf("%v", queryText)
		if len(querySnippet) > 60 {
			querySnippet = querySnippet[:57] + "..."
		}

		sb.WriteString(fmt.Sprintf("| %d | %v | %v | %v | %v | `%s` | %v |\n",
			i+1, count, source, database, question, querySnippet, suggestedTool))
	}

	sb.WriteString("\n---\n\n")
	sb.WriteString("**Recommendation:** Focus on gaps with `occurrence_count >= 3` first. Use `execute_read_query` with `database` and `query_text` from the table above to understand the pattern, then build a dedicated tool.\n")

	return sb.String()
}

func registerCapabilitiesResource(s *server.MCPServer) {
	const uri = "server://capabilities"
	s.AddResource(
		mcp.NewResource(uri, "Server Capabilities & Metadata",
			mcp.WithResourceDescription("Server version, transport modes, safety limits, connected databases, and catalog of resources"),
			mcp.WithMIMEType("text/markdown"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			markdown := formatServerCapabilities()
			return textResource(uri, markdown), nil
		},
	)
}

func formatServerCapabilities() string {
	var sb strings.Builder
	sb.WriteString("# VietElite DB MCP Server Capabilities\n\n")
	sb.WriteString(fmt.Sprintf("- **Server Name:** VietElite DB MCP Server\n"))
	sb.WriteString(fmt.Sprintf("- **Version:** `%s`\n", version.Version))
	sb.WriteString(fmt.Sprintf("- **Commit:** `%s`\n", version.Commit))
	sb.WriteString(fmt.Sprintf("- **Build Date:** `%s`\n", version.BuildDate))
	sb.WriteString("- **Supported Transports:** `stdio` (standard JSON-RPC), `sse` (HTTP Server-Sent Events with Bearer Auth)\n")
	sb.WriteString("- **Query Limit Enforcement:** Default Limit: `50` | Maximum Limit: `200`\n")
	sb.WriteString("- **Multi-layer Read-Only Safety:**\n")
	sb.WriteString("  - MySQL session level: `SET SESSION TRANSACTION READ ONLY` executed on pool connection\n")
	sb.WriteString("  - AST SQL Guard: strict single-statement validation, strips comments/quotes, blocks data manipulation keywords\n\n")

	sb.WriteString("## Connected Database Services\n\n")
	sb.WriteString("| Service Key | Target Database | Scope & Business Entity |\n")
	sb.WriteString("| --- | --- | --- |\n")
	sb.WriteString("| `identity` | `identity_service` | User accounts, student profiles, parent relationships, center mappings, staff, roles |\n")
	sb.WriteString("| `class` | `class_service` | Classes, courses, enrollments, schedules, attendance, test scores, homework |\n")
	sb.WriteString("| `finance` | `finance_service` | Invoices, tuition receipts, student wallets, transactions, chart of accounts, expense categories |\n")
	sb.WriteString("| `crm` | `crm_service` | Leads, pipelines, stages, opportunities, interactions, appointments, campaigns |\n\n")

	sb.WriteString("## Available MCP Resources\n\n")
	sb.WriteString("| Scheme | URI | Description |\n")
	sb.WriteString("| --- | --- | --- |\n")
	sb.WriteString("| `schema://` | `schema://identity_service` | Full schema definition for identity_service |\n")
	sb.WriteString("| `schema://` | `schema://class_service` | Full schema definition for class_service |\n")
	sb.WriteString("| `schema://` | `schema://finance_service` | Full schema definition for finance_service |\n")
	sb.WriteString("| `schema://` | `schema://crm_service` | Full schema definition for crm_service |\n")
	sb.WriteString("| `schema://` | `schema://summary` | High-level 4-service relational architecture map |\n")
	sb.WriteString("| `reference://` | `reference://centers` | Master centers list (`ve26_centers`) |\n")
	sb.WriteString("| `reference://` | `reference://courses` | Course catalog (`edu_courses`) |\n")
	sb.WriteString("| `reference://` | `reference://expense-categories` | Active expense categories (`fin_expense_categories`) |\n")
	sb.WriteString("| `reference://` | `reference://pipeline-stages` | CRM stages (`crm_pipeline_stages`) |\n")
	sb.WriteString("| `reference://` | `reference://accounts-chart` | Chart of accounts (`fin_accounts`) |\n")
	sb.WriteString("| `guide://` | `guide://tools-by-role/{role}` | Tool and workflow guides for 7 staff roles |\n")
	sb.WriteString("| `gaps://` | `gaps://report` | Dynamic capability gaps tracker report |\n")
	sb.WriteString("| `server://` | `server://capabilities` | This runtime capabilities metadata document |\n\n")

	sb.WriteString("## Tool Groups\n\n")
	sb.WriteString("- **Database Inspection Tools:** `list_databases`, `list_tables`, `describe_table`, `execute_read_query`\n")
	sb.WriteString("- **Business Helpers (Operations & Academic):** `get_daily_class_schedule`, `get_class_roster`, `get_class_detail`, `get_session_detail`, `get_student_attendance_history`, `get_absent_alerts`, `list_students_at_risk`, `list_ungraded_homework`, `get_homework_submissions`, `get_class_academic_summary`, `check_student_class_eligibility`\n")
	sb.WriteString("- **Finance & Tuition Tools:** `get_invoice_detail`, `list_unpaid_invoices`, `find_unallocated_receipts`, `get_class_debt_summary`, `get_student_payment_history`\n")
	sb.WriteString("- **Teacher Payroll & Contracts:** `get_my_payroll`, `get_teacher_payroll_detail`, `get_my_contract`, `reconcile_payroll_vs_contract`\n")
	sb.WriteString("- **Accounting & Bank Reconciliation:** `reconcile_invoice_vs_receipts`, `reconcile_bank_vs_receipts`\n")
	sb.WriteString("- **Center & Network Analytics:** `get_center_dashboard`, `compare_centers_performance`, `get_network_overview`\n")
	sb.WriteString("- **Capability Gap Management:** `log_capability_gap`, `summarize_capability_gaps`\n")

	return sb.String()
}
