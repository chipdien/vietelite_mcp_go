package tools

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"vietelite_mcp_go/config"
	"vietelite_mcp_go/db"
	"vietelite_mcp_go/logger"
)

// RegisterAggregateTools registers center, multi-center, and network-level analytics tools.
func RegisterAggregateTools(s *server.MCPServer) {
	s.AddTool(
		mcp.NewTool("get_center_dashboard",
			mcp.WithDescription("Consolidated 360° operational and financial performance dashboard for a specific learning center."),
			mcp.WithNumber("center_id", mcp.Required(), mcp.Description("Center ID")),
			mcp.WithString("from", mcp.Description("Start date filter YYYY-MM-DD")),
			mcp.WithString("to", mcp.Description("End date filter YYYY-MM-DD")),
		),
		handleGetCenterDashboard,
	)

	s.AddTool(
		mcp.NewTool("compare_centers_performance",
			mcp.WithDescription("Cross-compares active centers on students, active classes, tuition revenue, collection rate, and outstanding debt."),
			mcp.WithString("from", mcp.Description("Start date filter YYYY-MM-DD")),
			mcp.WithString("to", mcp.Description("End date filter YYYY-MM-DD")),
		),
		handleCompareCentersPerformance,
	)

	s.AddTool(
		mcp.NewTool("get_network_overview",
			mcp.WithDescription("Macro health overview across the entire VietElite network: total centers, classes, students, sessions, and financial health."),
			mcp.WithString("from", mcp.Description("Start date filter YYYY-MM-DD")),
			mcp.WithString("to", mcp.Description("End date filter YYYY-MM-DD")),
		),
		handleGetNetworkOverview,
	)
}

// CenterMetrics represents operational and financial KPIs for a single center.
type CenterMetrics struct {
	CenterID          int     `json:"centerId"`
	CenterName        string  `json:"centerName"`
	CenterCode        string  `json:"centerCode"`
	Address           string  `json:"address"`
	Phone             string  `json:"phone"`
	ActiveClasses     int     `json:"activeClasses"`
	ActiveStudents    int     `json:"activeStudents"`
	TotalSessions     int     `json:"totalSessions"`
	TotalBilled       float64 `json:"totalBilled"`
	TotalCollected    float64 `json:"totalCollected"`
	TotalOutstanding  float64 `json:"totalOutstanding"`
	CollectionRatePct float64 `json:"collectionRatePct"`
}

// computeCenterMetrics computes KPIs for a single center across identity, class, and finance DBs.
func computeCenterMetrics(ctx context.Context, centerID int, from, to string) (*CenterMetrics, error) {
	idPool, err := db.GetPool("identity")
	if err != nil {
		return nil, err
	}

	// 1. Center info
	var name, code, address, phone string
	err = idPool.QueryRowContext(ctx, `
		SELECT name, code, COALESCE(address, ''), COALESCE(phone, '')
		FROM ve26_centers
		WHERE id = ?
	`, centerID).Scan(&name, &code, &address, &phone)
	if err != nil {
		return nil, fmt.Errorf("center with ID %d not found: %w", centerID, err)
	}

	metrics := &CenterMetrics{
		CenterID:   centerID,
		CenterName: name,
		CenterCode: code,
		Address:    address,
		Phone:      phone,
	}

	// 2. Class DB: active classes, active students, sessions (scoped to current school year)
	classPool, err := db.GetPool("class")
	if err == nil {
		// Active classes — scoped to current school year
		_ = classPool.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM edu_classes
			WHERE center_id = ? AND active = 1 AND deleted_at IS NULL AND year_id = ?
		`, centerID, config.DefaultSchoolYearID).Scan(&metrics.ActiveClasses)

		// Active students — scoped to current school year
		_ = classPool.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT sc.student_id)
			FROM edu_student_class sc
			JOIN edu_classes c ON sc.class_id = c.id
			WHERE c.center_id = ? AND (sc.status = 'active' OR sc.status IS NULL) AND sc.leave_date IS NULL AND c.deleted_at IS NULL AND c.year_id = ?
		`, centerID, config.DefaultSchoolYearID).Scan(&metrics.ActiveStudents)

		// Sessions
		sessionWhere := "center_id = ? AND is_deleted = 0"
		var sessionParams []any = []any{centerID}
		if from != "" {
			sessionWhere += " AND date >= ?"
			sessionParams = append(sessionParams, from)
		}
		if to != "" {
			sessionWhere += " AND date <= ?"
			sessionParams = append(sessionParams, to)
		}
		_ = classPool.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT COUNT(*) FROM edu_sessions WHERE %s
		`, sessionWhere), sessionParams...).Scan(&metrics.TotalSessions)
	}

	// 3. Finance DB: billed, collected, debt
	finPool, err := db.GetPool("finance")
	if err == nil {
		// Billed and Outstanding from invoices
		invWhere := "d.center_id = ?"
		var invParams []any = []any{centerID}
		if from != "" {
			invWhere += " AND i.due_date >= ?"
			invParams = append(invParams, from)
		}
		if to != "" {
			invWhere += " AND i.due_date <= ?"
			invParams = append(invParams, to)
		}

		_ = finPool.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT
				COALESCE(SUM(d.amount), 0),
				COALESCE(SUM(CASE WHEN i.paid_status != 'paid' THEN (d.amount - COALESCE(d.promotion_amount, 0)) ELSE 0 END), 0)
			FROM fin_invoice_detail d
			JOIN fin_invoices i ON d.invoice_id = i.id
			WHERE %s AND d.deleted_at IS NULL
		`, invWhere), invParams...).Scan(&metrics.TotalBilled, &metrics.TotalOutstanding)

		// Receipts collected
		recWhere := "center_id = ?"
		var recParams []any = []any{centerID}
		if from != "" {
			recWhere += " AND date >= ?"
			recParams = append(recParams, from)
		}
		if to != "" {
			recWhere += " AND date <= ?"
			recParams = append(recParams, to)
		}

		_ = finPool.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT COALESCE(SUM(amount), 0)
			FROM fin_receipts
			WHERE %s
		`, recWhere), recParams...).Scan(&metrics.TotalCollected)

		if metrics.TotalBilled > 0 {
			metrics.CollectionRatePct = math.Round((metrics.TotalCollected/metrics.TotalBilled)*10000) / 100
		}
	}

	return metrics, nil
}

func handleGetCenterDashboard(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	centerID := r.GetInt("center_id", 0)
	if centerID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"center_id\""))
		logger.LogToolCall("get_center_dashboard", "", time.Since(start), fmt.Errorf("invalid center_id"))
		return res, nil
	}

	fromStr := r.GetString("from", "")
	toStr := r.GetString("to", "")

	metrics, err := computeCenterMetrics(ctx, centerID, fromStr, toStr)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_center_dashboard", fmt.Sprintf("center=%d", centerID), time.Since(start), err)
		return res, nil
	}

	logger.LogToolCall("get_center_dashboard", fmt.Sprintf("center=%d billed=%.2f collected=%.2f", centerID, metrics.TotalBilled, metrics.TotalCollected), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(metrics), nil
}

func handleCompareCentersPerformance(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	fromStr := r.GetString("from", "")
	toStr := r.GetString("to", "")

	idPool, err := db.GetPool("identity")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("compare_centers_performance", "", time.Since(start), err)
		return res, nil
	}

	// Fetch all centers
	rows, err := idPool.QueryContext(ctx, `SELECT id FROM ve26_centers ORDER BY id ASC`)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("compare_centers_performance", "", time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	var centerIDs []int
	for rows.Next() {
		var cID int
		if err := rows.Scan(&cID); err == nil {
			centerIDs = append(centerIDs, cID)
		}
	}

	var results []*CenterMetrics
	for _, cID := range centerIDs {
		if m, err := computeCenterMetrics(ctx, cID, fromStr, toStr); err == nil {
			results = append(results, m)
		}
	}

	logger.LogToolCall("compare_centers_performance", fmt.Sprintf("centersCount=%d", len(results)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"totalCenters": len(results),
		"centers":      results,
	}), nil
}

func handleGetNetworkOverview(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	fromStr := r.GetString("from", "")
	toStr := r.GetString("to", "")

	idPool, err := db.GetPool("identity")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_network_overview", "", time.Since(start), err)
		return res, nil
	}

	var totalCenters int
	_ = idPool.QueryRowContext(ctx, `SELECT COUNT(*) FROM ve26_centers`).Scan(&totalCenters)

	var totalTeachers int
	_ = idPool.QueryRowContext(ctx, `SELECT COUNT(*) FROM ve26_teacher WHERE is_active = 1`).Scan(&totalTeachers)

	var totalStudentsNetwork int
	_ = idPool.QueryRowContext(ctx, `SELECT COUNT(*) FROM ve26_student WHERE is_active = 1`).Scan(&totalStudentsNetwork)

	classPool, _ := db.GetPool("class")
	var totalActiveClasses, totalSessions int
	if classPool != nil {
		_ = classPool.QueryRowContext(ctx, `SELECT COUNT(*) FROM edu_classes WHERE active = 1 AND deleted_at IS NULL AND year_id = ?`, config.DefaultSchoolYearID).Scan(&totalActiveClasses)

		sWhere := "is_deleted = 0"
		var sParams []any
		if fromStr != "" {
			sWhere += " AND date >= ?"
			sParams = append(sParams, fromStr)
		}
		if toStr != "" {
			sWhere += " AND date <= ?"
			sParams = append(sParams, toStr)
		}
		_ = classPool.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM edu_sessions WHERE %s`, sWhere), sParams...).Scan(&totalSessions)
	}

	finPool, _ := db.GetPool("finance")
	var totalBilled, totalCollected, totalOutstanding float64
	if finPool != nil {
		invWhere := "1=1"
		var invParams []any
		if fromStr != "" {
			invWhere += " AND due_date >= ?"
			invParams = append(invParams, fromStr)
		}
		if toStr != "" {
			invWhere += " AND due_date <= ?"
			invParams = append(invParams, toStr)
		}

		_ = finPool.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT
				COALESCE(SUM(total_amount), 0),
				COALESCE(SUM(CASE WHEN paid_status != 'paid' THEN (total_amount - paid_amount) ELSE 0 END), 0)
			FROM fin_invoices
			WHERE %s
		`, invWhere), invParams...).Scan(&totalBilled, &totalOutstanding)

		recWhere := "1=1"
		var recParams []any
		if fromStr != "" {
			recWhere += " AND date >= ?"
			recParams = append(recParams, fromStr)
		}
		if toStr != "" {
			recWhere += " AND date <= ?"
			recParams = append(recParams, toStr)
		}
		_ = finPool.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT COALESCE(SUM(amount), 0) FROM fin_receipts WHERE %s
		`, recWhere), recParams...).Scan(&totalCollected)
	}

	var overallCollectionRate float64
	if totalBilled > 0 {
		overallCollectionRate = math.Round((totalCollected/totalBilled)*10000) / 100
	}

	logger.LogToolCall("get_network_overview", fmt.Sprintf("centers=%d classes=%d billed=%.2f", totalCenters, totalActiveClasses, totalBilled), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"networkOverview": map[string]any{
			"totalCenters":          totalCenters,
			"totalActiveClasses":    totalActiveClasses,
			"totalActiveTeachers":   totalTeachers,
			"totalActiveStudents":   totalStudentsNetwork,
			"totalSessions":         totalSessions,
			"totalBilled":           totalBilled,
			"totalCollected":        totalCollected,
			"totalOutstanding":      totalOutstanding,
			"collectionRatePercent": overallCollectionRate,
		},
	}), nil
}
