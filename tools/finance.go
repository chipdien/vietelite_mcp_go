package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"vietelite-mcp/go-server/db"
	"vietelite-mcp/go-server/logger"
)

// RegisterFinanceTools registers tools related to invoices, debt, and receipts.
func RegisterFinanceTools(s *server.MCPServer) {
	s.AddTool(
		mcp.NewTool("get_invoice_detail",
			mcp.WithDescription("Retrieves complete breakdown of an invoice: billing period, student, line items (classes, courses, fees), and allocated payment receipts."),
			mcp.WithNumber("invoice_id", mcp.Required(), mcp.Description("Invoice ID")),
		),
		handleGetInvoiceDetail,
	)

	s.AddTool(
		mcp.NewTool("list_unpaid_invoices",
			mcp.WithDescription("Lists unpaid or partially paid tuition invoices, filterable by center, due date range, and minimum overdue days."),
			mcp.WithNumber("center_id", mcp.Description("Filter by center ID")),
			mcp.WithString("from", mcp.Description("Filter invoices due on or after YYYY-MM-DD")),
			mcp.WithString("to", mcp.Description("Filter invoices due on or before YYYY-MM-DD")),
			mcp.WithNumber("min_days_overdue", mcp.Description("Filter invoices overdue by at least this many days (default 0)")),
			mcp.WithNumber("limit", mcp.Description("Max invoices returned (default 50, max 200)")),
		),
		handleListUnpaidInvoices,
	)

	s.AddTool(
		mcp.NewTool("find_unallocated_receipts",
			mcp.WithDescription("Finds receipts with unallocated balance ('tiền treo') that have not been fully applied towards invoices."),
			mcp.WithNumber("center_id", mcp.Description("Filter by center ID")),
			mcp.WithString("from", mcp.Description("Filter receipts on or after YYYY-MM-DD")),
			mcp.WithString("to", mcp.Description("Filter receipts on or before YYYY-MM-DD")),
			mcp.WithNumber("limit", mcp.Description("Max receipts returned (default 50, max 200)")),
		),
		handleFindUnallocatedReceipts,
	)
}

func handleGetInvoiceDetail(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	invoiceID := r.GetInt("invoice_id", 0)
	if invoiceID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"invoice_id\""))
		logger.LogToolCall("get_invoice_detail", "", time.Since(start), fmt.Errorf("invalid invoice_id"))
		return res, nil
	}

	pool, err := db.GetPool("finance")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_invoice_detail", fmt.Sprintf("invoice=%d", invoiceID), time.Since(start), err)
		return res, nil
	}

	// 1. Invoice Header
	headerQuery := `
		SELECT 
			i.id, i.student_id, i.billing_period, i.subtotal,
			i.discount_percentage, i.discount_value, i.vat,
			i.total_amount, i.paid_amount, i.adjustment_amount,
			(i.total_amount - i.paid_amount) AS outstanding,
			i.paid_status, i.transaction_status, i.due_date, i.paid_date,
			i.content, i.note, i.days_overdue, i.debt_label,
			i.is_locked, i.invoice_type, i.created_at, i.updated_at
		FROM fin_invoices i
		WHERE i.id = ?
	`
	headerRows, err := pool.QueryContext(ctx, headerQuery, invoiceID)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_invoice_detail", fmt.Sprintf("invoice=%d", invoiceID), time.Since(start), err)
		return res, nil
	}
	defer headerRows.Close()

	headerList, err := rowsToMaps(headerRows)
	if err != nil || len(headerList) == 0 {
		if err == nil {
			err = fmt.Errorf("invoice not found with ID %d", invoiceID)
		}
		res := errorResult(err)
		logger.LogToolCall("get_invoice_detail", fmt.Sprintf("invoice=%d", invoiceID), time.Since(start), err)
		return res, nil
	}
	invoiceInfo := headerList[0]

	// 2. Invoice Details (Line items)
	detailQuery := `
		SELECT 
			d.id, d.model, d.model_id, d.center_id, d.center_name,
			d.class_id, d.class_code, d.class_name,
			d.course_id, d.course_name, d.teacher_id, d.teacher_name,
			d.unit_price, d.quantity, d.amount, d.promotion_amount,
			d.start_date, d.end_date, d.note
		FROM fin_invoice_detail d
		WHERE d.invoice_id = ? AND d.deleted_at IS NULL
		ORDER BY d.id ASC
	`
	detailRows, err := pool.QueryContext(ctx, detailQuery, invoiceID)
	var lineItems []map[string]any
	if err == nil {
		lineItems, _ = rowsToMaps(detailRows)
		detailRows.Close()
	}

	// 3. Allocations (Receipts applied towards this invoice)
	allocQuery := `
		SELECT 
			a.id AS allocation_id,
			a.receipt_id,
			r.receipt_number_str,
			r.method AS payment_method,
			r.date AS payment_date,
			a.amount_applied,
			a.allocation_date,
			a.created_at
		FROM fin_receipt_allocations a
		JOIN fin_receipts r ON a.receipt_id = r.id
		WHERE a.invoice_id = ?
		ORDER BY a.allocation_date DESC
	`
	allocRows, err := pool.QueryContext(ctx, allocQuery, invoiceID)
	var allocations []map[string]any
	if err == nil {
		allocations, _ = rowsToMaps(allocRows)
		allocRows.Close()
	}

	// 4. Enrich student details
	var studentInfo any = nil
	if sIDVal, ok := invoiceInfo["student_id"]; ok && sIDVal != nil {
		var sID int
		if v, ok := sIDVal.(int64); ok {
			sID = int(v)
		} else if v, ok := sIDVal.(int); ok {
			sID = v
		}
		if sID > 0 {
			profiles := fetchStudentProfiles(ctx, []int{sID})
			if p, ok := profiles[sID]; ok {
				studentInfo = p
			}
		}
	}

	logger.LogToolCall("get_invoice_detail", fmt.Sprintf("invoice=%d items=%d allocs=%d", invoiceID, len(lineItems), len(allocations)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"invoiceId":   invoiceID,
		"invoice":     invoiceInfo,
		"student":     studentInfo,
		"lineItems":   lineItems,
		"allocations": allocations,
	}), nil
}

func handleListUnpaidInvoices(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	centerID := r.GetInt("center_id", 0)
	fromStr := r.GetString("from", "")
	toStr := r.GetString("to", "")
	minDaysOverdue := r.GetInt("min_days_overdue", 0)
	limit := clampLimit(r.GetInt("limit", defaultLimit))

	pool, err := db.GetPool("finance")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("list_unpaid_invoices", "", time.Since(start), err)
		return res, nil
	}

	whereClause := "i.paid_status != 'paid' AND (i.total_amount - i.paid_amount) > 0"
	var params []any
	joinClause := ""

	if centerID > 0 {
		joinClause = "JOIN fin_invoice_detail d ON d.invoice_id = i.id"
		whereClause += " AND d.center_id = ?"
		params = append(params, centerID)
	}

	if fromStr != "" {
		whereClause += " AND i.due_date >= ?"
		params = append(params, fromStr)
	}
	if toStr != "" {
		whereClause += " AND i.due_date <= ?"
		params = append(params, toStr)
	}
	if minDaysOverdue > 0 {
		whereClause += " AND i.days_overdue >= ?"
		params = append(params, minDaysOverdue)
	}

	// 1. Summary totals
	summaryQuery := fmt.Sprintf(`
		SELECT 
			COUNT(DISTINCT i.id) AS total_unpaid_count,
			COALESCE(SUM(i.total_amount), 0) AS total_billed,
			COALESCE(SUM(i.paid_amount), 0) AS total_paid,
			COALESCE(SUM(i.total_amount - i.paid_amount), 0) AS total_outstanding
		FROM fin_invoices i
		%s
		WHERE %s
	`, joinClause, whereClause)

	var totalUnpaidCount int
	var totalBilled, totalPaid, totalOutstanding float64
	_ = pool.QueryRowContext(ctx, summaryQuery, params...).Scan(&totalUnpaidCount, &totalBilled, &totalPaid, &totalOutstanding)

	// 2. Invoice list
	listQuery := fmt.Sprintf(`
		SELECT DISTINCT
			i.id AS invoice_id,
			i.student_id,
			i.billing_period,
			i.total_amount,
			i.paid_amount,
			(i.total_amount - i.paid_amount) AS outstanding,
			i.due_date,
			i.days_overdue,
			i.debt_label,
			i.paid_status,
			i.created_at
		FROM fin_invoices i
		%s
		WHERE %s
		ORDER BY i.days_overdue DESC, i.due_date ASC
		LIMIT ?
	`, joinClause, whereClause)

	listParams := append(params, limit)
	rows, err := pool.QueryContext(ctx, listQuery, listParams...)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("list_unpaid_invoices", "", time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	invoices, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("list_unpaid_invoices", "", time.Since(start), err)
		return res, nil
	}

	// Enrich student info
	var studentIDs []int
	for _, inv := range invoices {
		if sIDVal, ok := inv["student_id"]; ok {
			var sID int
			if v, ok := sIDVal.(int64); ok {
				sID = int(v)
			} else if v, ok := sIDVal.(int); ok {
				sID = v
			}
			studentIDs = append(studentIDs, sID)
		}
	}
	profiles := fetchStudentProfiles(ctx, studentIDs)
	for _, inv := range invoices {
		if sIDVal, ok := inv["student_id"]; ok {
			var sID int
			if v, ok := sIDVal.(int64); ok {
				sID = int(v)
			} else if v, ok := sIDVal.(int); ok {
				sID = v
			}
			if prof, ok := profiles[sID]; ok {
				inv["studentName"] = prof["studentName"]
				inv["studentPhone"] = prof["studentPhone"]
				inv["parentName"] = prof["parentName"]
				inv["parentPhone"] = prof["parentPhone"]
			}
		}
	}

	logger.LogToolCall("list_unpaid_invoices", fmt.Sprintf("unpaid=%d outstanding=%.2f", totalUnpaidCount, totalOutstanding), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"summary": map[string]any{
			"totalUnpaidCount": totalUnpaidCount,
			"totalBilled":      totalBilled,
			"totalPaid":        totalPaid,
			"totalOutstanding": totalOutstanding,
		},
		"invoices": invoices,
	}), nil
}

func handleFindUnallocatedReceipts(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	centerID := r.GetInt("center_id", 0)
	fromStr := r.GetString("from", "")
	toStr := r.GetString("to", "")
	limit := clampLimit(r.GetInt("limit", defaultLimit))

	pool, err := db.GetPool("finance")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("find_unallocated_receipts", "", time.Since(start), err)
		return res, nil
	}

	whereClause := "1=1"
	var params []any

	if centerID > 0 {
		whereClause += " AND r.center_id = ?"
		params = append(params, centerID)
	}
	if fromStr != "" {
		whereClause += " AND r.date >= ?"
		params = append(params, fromStr)
	}
	if toStr != "" {
		whereClause += " AND r.date <= ?"
		params = append(params, toStr)
	}

	query := fmt.Sprintf(`
		SELECT 
			r.id AS receipt_id,
			r.receipt_number_str,
			r.student_id,
			r.center_id,
			r.name,
			r.method,
			r.date AS receipt_date,
			r.amount AS total_amount,
			COALESCE(SUM(a.amount_applied), 0) AS total_allocated,
			(r.amount - COALESCE(SUM(a.amount_applied), 0)) AS unallocated_amount,
			r.status
		FROM fin_receipts r
		LEFT JOIN fin_receipt_allocations a ON r.id = a.receipt_id
		WHERE %s
		GROUP BY r.id
		HAVING unallocated_amount > 0.01
		ORDER BY unallocated_amount DESC, r.date DESC
		LIMIT ?
	`, whereClause)
	params = append(params, limit)

	rows, err := pool.QueryContext(ctx, query, params...)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("find_unallocated_receipts", "", time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	receipts, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("find_unallocated_receipts", "", time.Since(start), err)
		return res, nil
	}

	// Calculate total unallocated
	var totalUnallocated float64
	var studentIDs []int
	for _, rec := range receipts {
		if unallocVal, ok := rec["unallocated_amount"]; ok {
			if v, ok := unallocVal.(float64); ok {
				totalUnallocated += v
			}
		}
		if sIDVal, ok := rec["student_id"]; ok && sIDVal != nil {
			var sID int
			if v, ok := sIDVal.(int64); ok {
				sID = int(v)
			} else if v, ok := sIDVal.(int); ok {
				sID = v
			}
			if sID > 0 {
				studentIDs = append(studentIDs, sID)
			}
		}
	}

	// Enrich student info
	profiles := fetchStudentProfiles(ctx, studentIDs)
	for _, rec := range receipts {
		if sIDVal, ok := rec["student_id"]; ok && sIDVal != nil {
			var sID int
			if v, ok := sIDVal.(int64); ok {
				sID = int(v)
			} else if v, ok := sIDVal.(int); ok {
				sID = v
			}
			if prof, ok := profiles[sID]; ok {
				rec["studentName"] = prof["studentName"]
				rec["parentName"] = prof["parentName"]
				rec["parentPhone"] = prof["parentPhone"]
			}
		}
	}

	logger.LogToolCall("find_unallocated_receipts", fmt.Sprintf("count=%d unallocated=%.2f", len(receipts), totalUnallocated), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"summary": map[string]any{
			"unallocatedReceiptCount": len(receipts),
			"totalUnallocatedAmount":  totalUnallocated,
		},
		"receipts": receipts,
	}), nil
}
