package tools

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"vietelite-mcp/go-server/db"
	"vietelite-mcp/go-server/logger"
)

// RegisterReconcileTools registers accounting audit and reconciliation tools.
func RegisterReconcileTools(s *server.MCPServer) {
	s.AddTool(
		mcp.NewTool("reconcile_invoice_vs_receipts",
			mcp.WithDescription("Audits invoice amounts against allocated receipts to detect status mismatches, overpayments, or unrecorded collections."),
			mcp.WithNumber("invoice_id", mcp.Description("Optional Invoice ID filter")),
			mcp.WithNumber("student_id", mcp.Description("Optional Student ID filter")),
			mcp.WithNumber("limit", mcp.Description("Max invoices audited (default 50, max 200)")),
		),
		handleReconcileInvoiceVsReceipts,
	)

	s.AddTool(
		mcp.NewTool("reconcile_bank_vs_receipts",
			mcp.WithDescription("Cross-checks bank transactions against system receipts to surface unmatched bank credits and unverified payment receipts."),
			mcp.WithNumber("center_id", mcp.Description("Optional Center ID filter")),
			mcp.WithString("from", mcp.Description("Filter transactions from date YYYY-MM-DD")),
			mcp.WithString("to", mcp.Description("Filter transactions to date YYYY-MM-DD")),
			mcp.WithNumber("limit", mcp.Description("Max unmatched items returned (default 50, max 200)")),
		),
		handleReconcileBankVsReceipts,
	)
}

func handleReconcileInvoiceVsReceipts(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	invoiceID := r.GetInt("invoice_id", 0)
	studentID := r.GetInt("student_id", 0)
	limit := clampLimit(r.GetInt("limit", defaultLimit))

	pool, err := db.GetPool("finance")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("reconcile_invoice_vs_receipts", "", time.Since(start), err)
		return res, nil
	}

	whereClause := "1=1"
	var params []any

	if invoiceID > 0 {
		whereClause += " AND i.id = ?"
		params = append(params, invoiceID)
	}
	if studentID > 0 {
		whereClause += " AND i.student_id = ?"
		params = append(params, studentID)
	}

	query := fmt.Sprintf(`
		SELECT 
			i.id AS invoice_id,
			i.student_id,
			i.billing_period,
			i.total_amount,
			i.paid_amount,
			i.paid_status,
			COALESCE(SUM(a.amount_applied), 0) AS actual_allocated_sum,
			COUNT(a.id) AS allocation_count
		FROM fin_invoices i
		LEFT JOIN fin_receipt_allocations a ON i.id = a.invoice_id
		WHERE %s
		GROUP BY i.id
		ORDER BY i.id DESC
		LIMIT ?
	`, whereClause)
	params = append(params, limit)

	rows, err := pool.QueryContext(ctx, query, params...)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("reconcile_invoice_vs_receipts", "", time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	var discrepancies []map[string]any
	var studentIDs []int
	totalAudited := 0
	totalDiscrepancies := 0

	for rows.Next() {
		totalAudited++
		var invID, sID, allocCount int
		var period, status string
		var totalAmount, recordedPaid, actualAllocated float64

		if err := rows.Scan(&invID, &sID, &period, &totalAmount, &recordedPaid, &status, &actualAllocated, &allocCount); err == nil {
			delta := math.Abs(recordedPaid - actualAllocated)
			var issue string

			if delta > 0.01 {
				issue = fmt.Sprintf("Chênh lệch ghi nhận: paid_amount (%.2f) != thực tế đã cấn trừ (%.2f)", recordedPaid, actualAllocated)
			} else if status == "paid" && actualAllocated < (totalAmount-0.01) {
				issue = fmt.Sprintf("Trạng thái ghi 'paid' nhưng mới cấn trừ %.2f / %.2f", actualAllocated, totalAmount)
			} else if actualAllocated > (totalAmount + 0.01) {
				issue = fmt.Sprintf("Thu vượt quá số tiền hóa đơn: đã cấn trừ %.2f / %.2f", actualAllocated, totalAmount)
			}

			if issue != "" {
				totalDiscrepancies++
				studentIDs = append(studentIDs, sID)
				discrepancies = append(discrepancies, map[string]any{
					"invoiceId":          invID,
					"studentId":          sID,
					"billingPeriod":      period,
					"totalAmount":        totalAmount,
					"recordedPaidAmount": recordedPaid,
					"actualAllocatedSum": actualAllocated,
					"recordedStatus":     status,
					"allocationCount":    allocCount,
					"issue":              issue,
				})
			}
		}
	}

	// Enrich student info
	profiles := fetchStudentProfiles(ctx, studentIDs)
	for _, item := range discrepancies {
		if sIDVal, ok := item["studentId"].(int); ok {
			if prof, ok := profiles[sIDVal]; ok {
				item["studentName"] = prof["studentName"]
				item["studentPhone"] = prof["studentPhone"]
				item["parentName"] = prof["parentName"]
			}
		}
	}

	logger.LogToolCall("reconcile_invoice_vs_receipts", fmt.Sprintf("audited=%d discrepancies=%d", totalAudited, totalDiscrepancies), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"summary": map[string]any{
			"totalInvoicesAudited": totalAudited,
			"discrepanciesFound":   totalDiscrepancies,
			"cleanCount":           totalAudited - totalDiscrepancies,
		},
		"discrepancies": discrepancies,
	}), nil
}

func handleReconcileBankVsReceipts(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	centerID := r.GetInt("center_id", 0)
	fromStr := r.GetString("from", "")
	toStr := r.GetString("to", "")
	limit := clampLimit(r.GetInt("limit", defaultLimit))

	pool, err := db.GetPool("finance")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("reconcile_bank_vs_receipts", "", time.Since(start), err)
		return res, nil
	}

	// 1. Unmatched bank transactions (money in, but no receipt attached)
	bankWhere := "bt.amount > 0 AND (bt.receipt_id IS NULL OR bt.receipt_id = 0) AND m.id IS NULL"
	var bankParams []any

	if fromStr != "" {
		bankWhere += " AND bt.transaction_date >= ?"
		bankParams = append(bankParams, fromStr)
	}
	if toStr != "" {
		bankWhere += " AND bt.transaction_date <= ?"
		bankParams = append(bankParams, toStr)
	}

	unmatchedBankQuery := fmt.Sprintf(`
		SELECT 
			bt.id AS bank_transaction_id,
			bt.tid,
			bt.bank_name,
			bt.amount,
			bt.transaction_date,
			bt.description,
			bt.corresponsive_name,
			bt.corresponsive_account,
			bt.status
		FROM fin_bank_transactions bt
		LEFT JOIN fin_receipt_bank_matches m ON bt.id = m.bank_transaction_id
		WHERE %s
		ORDER BY bt.transaction_date DESC
		LIMIT ?
	`, bankWhere)
	bankParams = append(bankParams, limit)

	bankRows, err := pool.QueryContext(ctx, unmatchedBankQuery, bankParams...)
	var unmatchedBankItems []map[string]any
	if err == nil {
		unmatchedBankItems, _ = rowsToMaps(bankRows)
		bankRows.Close()
	}

	// 2. Bank-transfer receipts that have no matching bank transaction record
	receiptWhere := "r.method IN ('bank_transfer', 'transfer', 'chuyen_khoan', 'bank', 'vietqr') AND bt.id IS NULL AND m.id IS NULL"
	var receiptParams []any

	if centerID > 0 {
		receiptWhere += " AND r.center_id = ?"
		receiptParams = append(receiptParams, centerID)
	}
	if fromStr != "" {
		receiptWhere += " AND r.date >= ?"
		receiptParams = append(receiptParams, fromStr)
	}
	if toStr != "" {
		receiptWhere += " AND r.date <= ?"
		receiptParams = append(receiptParams, toStr)
	}

	unmatchedReceiptQuery := fmt.Sprintf(`
		SELECT 
			r.id AS receipt_id,
			r.receipt_number_str,
			r.student_id,
			r.center_id,
			r.amount,
			r.date AS receipt_date,
			r.method,
			r.name,
			r.description
		FROM fin_receipts r
		LEFT JOIN fin_bank_transactions bt ON r.id = bt.receipt_id
		LEFT JOIN fin_receipt_bank_matches m ON r.id = m.receipt_id
		WHERE %s
		ORDER BY r.date DESC
		LIMIT ?
	`, receiptWhere)
	receiptParams = append(receiptParams, limit)

	receiptRows, err := pool.QueryContext(ctx, unmatchedReceiptQuery, receiptParams...)
	var unmatchedReceiptItems []map[string]any
	if err == nil {
		unmatchedReceiptItems, _ = rowsToMaps(receiptRows)
		receiptRows.Close()
	}

	logger.LogToolCall("reconcile_bank_vs_receipts", fmt.Sprintf("unmatchedBank=%d unmatchedReceipts=%d", len(unmatchedBankItems), len(unmatchedReceiptItems)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"summary": map[string]any{
			"unmatchedBankCreditCount":   len(unmatchedBankItems),
			"unverifiedBankReceiptCount": len(unmatchedReceiptItems),
		},
		"unmatchedBankCredits":   unmatchedBankItems,
		"unverifiedBankReceipts": unmatchedReceiptItems,
	}), nil
}
