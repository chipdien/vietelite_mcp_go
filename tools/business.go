package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"vietelite-mcp/go-server/config"
	"vietelite-mcp/go-server/db"
	"vietelite-mcp/go-server/logger"
)

func RegisterBusinessTools(s *server.MCPServer) {
	s.AddTool(
		mcp.NewTool("get_daily_class_schedule",
			mcp.WithDescription("Retrieves daily class schedule, room assignment, teacher, and student headcount for a center. Defaults to current school year 2026-2027 if school_year_id is not specified."),
			mcp.WithNumber("center_id", mcp.Required(), mcp.Description("Center ID")),
			mcp.WithString("date", mcp.Description("Date YYYY-MM-DD")),
			mcp.WithNumber("school_year_id", mcp.Description("School year ID (default: 2026 for school year 2026-2027)")),
		),
		handleGetDailyClassSchedule,
	)

	s.AddTool(
		mcp.NewTool("get_class_roster",
			mcp.WithDescription("Retrieves student roster and attendance status for a specific class session."),
			mcp.WithNumber("class_id", mcp.Required(), mcp.Description("Class ID")),
			mcp.WithString("session_date", mcp.Description("Session date YYYY-MM-DD")),
		),
		handleGetClassRoster,
	)

	s.AddTool(
		mcp.NewTool("get_student_attendance_history",
			mcp.WithDescription("Retrieves attendance history, absence reasons, and make-up session records for a student."),
			mcp.WithNumber("student_id", mcp.Required(), mcp.Description("Student ID")),
			mcp.WithNumber("class_id", mcp.Description("Optional Class ID filter")),
			mcp.WithNumber("limit", mcp.Description("Max records returned")),
		),
		handleGetStudentAttendanceHistory,
	)

	s.AddTool(
		mcp.NewTool("get_absent_alerts",
			mcp.WithDescription("Identifies students with consecutive absences for parent outreach. Defaults to current school year 2026-2027 if school_year_id is not specified."),
			mcp.WithNumber("center_id", mcp.Description("Optional Center ID filter")),
			mcp.WithNumber("school_year_id", mcp.Description("Optional school year ID filter (default: 2026 for school year 2026-2027; set to 0 for all years)")),
			mcp.WithNumber("consecutive_absent_count", mcp.Description("Consecutive absences threshold (default 2)")),
			mcp.WithNumber("limit", mcp.Description("Max alerts returned (default 50, max 200)")),
		),
		handleGetAbsentAlerts,
	)

	s.AddTool(
		mcp.NewTool("get_class_academic_summary",
			mcp.WithDescription("Aggregates test scores, homework completion, and teacher session comments for a class or student."),
			mcp.WithNumber("class_id", mcp.Required(), mcp.Description("Class ID")),
			mcp.WithNumber("student_id", mcp.Description("Optional Student ID filter")),
			mcp.WithNumber("test_id", mcp.Description("Optional Test ID filter")),
		),
		handleGetClassAcademicSummary,
	)

	s.AddTool(
		mcp.NewTool("check_student_class_eligibility",
			mcp.WithDescription("Checks student enrollment status and overdue tuition balance before class entry."),
			mcp.WithNumber("student_id", mcp.Required(), mcp.Description("Student ID")),
			mcp.WithNumber("class_id", mcp.Description("Optional Class ID filter")),
		),
		handleCheckStudentClassEligibility,
	)

	// New tools addressing capability gaps
	s.AddTool(
		mcp.NewTool("get_student_payment_history",
			mcp.WithDescription("Retrieves full payment history for a student: invoices with their allocated receipts (which receipt paid which invoice, how much, when)."),
			mcp.WithNumber("student_id", mcp.Required(), mcp.Description("Student ID")),
			mcp.WithString("from", mcp.Description("Start date YYYY-MM-DD")),
			mcp.WithString("to", mcp.Description("End date YYYY-MM-DD")),
			mcp.WithNumber("limit", mcp.Description("Max invoices returned (default 50, max 200)")),
		),
		handleGetStudentPaymentHistory,
	)

	s.AddTool(
		mcp.NewTool("get_student_enrollment_history",
			mcp.WithDescription("Retrieves student's class enrollment history: current classes (optionally filtered by school year) and historical enrollment actions (joined, left, transferred). Defaults to current school year 2026-2027 if not specified."),
			mcp.WithNumber("student_id", mcp.Required(), mcp.Description("Student ID")),
			mcp.WithNumber("school_year_id", mcp.Description("Filter current enrollments by school year ID (default: 2026 for school year 2026-2027)")),
			mcp.WithBoolean("active_only", mcp.Description("Show only active enrollments (default false)")),
			mcp.WithNumber("limit", mcp.Description("Max history records returned (default 50)")),
		),
		handleGetStudentEnrollmentHistory,
	)

	s.AddTool(
		mcp.NewTool("get_class_debt_summary",
			mcp.WithDescription("Retrieves tuition debt summary for a class: total billed, total paid, total outstanding, and list of students with unpaid invoices."),
			mcp.WithNumber("class_id", mcp.Required(), mcp.Description("Class ID")),
			mcp.WithNumber("min_days_overdue", mcp.Description("Filter students with debt overdue by at least this many days (default 0)")),
			mcp.WithNumber("limit", mcp.Description("Max unpaid students returned (default 50, max 200)")),
		),
		handleGetClassDebtSummary,
	)

	// Gap management tools
	s.AddTool(
		mcp.NewTool("log_capability_gap",
			mcp.WithDescription("Explicitly records a capability gap when no suitable tool exists for a task. Helps identify patterns for building new tools."),
			mcp.WithString("question", mcp.Required(), mcp.Description("The question or need the agent could not answer with existing tools")),
			mcp.WithString("database", mcp.Description("Database service involved (identity/class/finance/crm)")),
			mcp.WithString("attempted_query", mcp.Description("SQL query attempted (if applicable)")),
			mcp.WithString("suggested_tool_name", mcp.Description("Suggested name for the tool that should exist")),
			mcp.WithString("notes", mcp.Description("Additional context or reasoning")),
		),
		handleLogCapabilityGap,
	)

	s.AddTool(
		mcp.NewTool("summarize_capability_gaps",
			mcp.WithDescription("Retrieves recorded capability gaps ordered by frequency, helping identify which tools should be built next."),
			mcp.WithNumber("min_occurrences", mcp.Description("Minimum occurrence count to include (default 1)")),
			mcp.WithNumber("limit", mcp.Description("Max gaps returned (default 50, max 200)")),
		),
		handleSummarizeCapabilityGaps,
	)

	// Phase 1 tools
	RegisterAcademicTools(s)
	RegisterFinanceTools(s)

	// Phase 2 tools
	RegisterPayrollTools(s)
	RegisterReconcileTools(s)
	RegisterAggregateTools(s)
}

func handleGetDailyClassSchedule(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	centerID := r.GetInt("center_id", 0)
	if centerID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"center_id\""))
		logger.LogToolCall("get_daily_class_schedule", "", time.Since(start), fmt.Errorf("invalid center_id"))
		return res, nil
	}

	dateStr := r.GetString("date", time.Now().Format("2006-01-02"))
	schoolYearID := r.GetInt("school_year_id", config.DefaultSchoolYearID)

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_daily_class_schedule", fmt.Sprintf("center=%d date=%s", centerID, dateStr), time.Since(start), err)
		return res, nil
	}

	rows, err := pool.QueryContext(ctx, `
		SELECT c.id, c.name, COALESCE(c.friendly_class_schedule, ''), COALESCE(c.friendly_class_teacher, '')
		FROM edu_classes c
		WHERE c.center_id = ? AND c.deleted_at IS NULL AND c.year_id = ?
		ORDER BY c.id DESC LIMIT 50`, centerID, schoolYearID)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_daily_class_schedule", fmt.Sprintf("center=%d date=%s", centerID, dateStr), time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	rowMaps, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_daily_class_schedule", fmt.Sprintf("center=%d date=%s", centerID, dateStr), time.Since(start), err)
		return res, nil
	}

	logger.LogToolCall("get_daily_class_schedule", fmt.Sprintf("center=%d date=%s count=%d", centerID, dateStr, len(rowMaps)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"centerId": centerID,
		"date":     dateStr,
		"classes":  rowMaps,
	}), nil
}

func handleGetClassRoster(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	classID := r.GetInt("class_id", 0)
	if classID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"class_id\""))
		logger.LogToolCall("get_class_roster", "", time.Since(start), fmt.Errorf("invalid class_id"))
		return res, nil
	}

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_class_roster", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}

	rows, err := pool.QueryContext(ctx, `
		SELECT sc.student_id, sc.status, sc.entrance_date
		FROM edu_student_class sc
		WHERE sc.class_id = ?
		ORDER BY sc.student_id ASC LIMIT 100`, classID)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_class_roster", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	rowMaps, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_class_roster", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}

	logger.LogToolCall("get_class_roster", fmt.Sprintf("class=%d count=%d", classID, len(rowMaps)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"classId": classID,
		"roster":  rowMaps,
	}), nil
}

func handleGetStudentAttendanceHistory(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	studentID := r.GetInt("student_id", 0)
	if studentID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"student_id\""))
		logger.LogToolCall("get_student_attendance_history", "", time.Since(start), fmt.Errorf("invalid student_id"))
		return res, nil
	}

	limit := clampLimit(r.GetInt("limit", defaultLimit))

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_student_attendance_history", fmt.Sprintf("student=%d", studentID), time.Since(start), err)
		return res, nil
	}

	rows, err := pool.QueryContext(ctx, `
		SELECT sa.id, sa.session_id, sa.attendance, sa.note, sa.created_at
		FROM edu_student_session_attendance sa
		WHERE sa.student_id = ?
		ORDER BY sa.id DESC LIMIT ?`, studentID, limit)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_student_attendance_history", fmt.Sprintf("student=%d", studentID), time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	rowMaps, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_student_attendance_history", fmt.Sprintf("student=%d", studentID), time.Since(start), err)
		return res, nil
	}

	logger.LogToolCall("get_student_attendance_history", fmt.Sprintf("student=%d count=%d", studentID, len(rowMaps)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"studentId":  studentID,
		"attendance": rowMaps,
	}), nil
}

func handleGetAbsentAlerts(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	threshold := r.GetInt("consecutive_absent_count", 2)
	centerID := r.GetInt("center_id", 0)
	schoolYearID := r.GetInt("school_year_id", config.DefaultSchoolYearID)
	limit := clampLimit(r.GetInt("limit", defaultLimit))

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_absent_alerts", fmt.Sprintf("threshold=%d", threshold), time.Since(start), err)
		return res, nil
	}

	whereConditions := []string{
		"sa.attendance IN ('absence', 'n_absence', 'absent', 'vắng', 'vang')",
		"c.deleted_at IS NULL",
		"s.is_deleted = 0",
	}
	var params []any

	if schoolYearID > 0 {
		whereConditions = append(whereConditions, "c.year_id = ?")
		params = append(params, schoolYearID)
	}

	if centerID > 0 {
		whereConditions = append(whereConditions, "c.center_id = ?")
		params = append(params, centerID)
	}

	params = append(params, threshold, limit)

	query := fmt.Sprintf(`
		SELECT
			sa.student_id,
			COUNT(*) AS absent_count,
			GROUP_CONCAT(DISTINCT c.name SEPARATOR ', ') AS class_names
		FROM edu_student_session_attendance sa
		JOIN edu_sessions s ON sa.session_id = s.id
		JOIN edu_classes c ON s.class_id = c.id
		WHERE %s
		GROUP BY sa.student_id
		HAVING absent_count >= ?
		ORDER BY absent_count DESC
		LIMIT ?`, strings.Join(whereConditions, " AND "))

	rows, err := pool.QueryContext(ctx, query, params...)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_absent_alerts", fmt.Sprintf("threshold=%d year=%d center=%d", threshold, schoolYearID, centerID), time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	rowMaps, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_absent_alerts", fmt.Sprintf("threshold=%d", threshold), time.Since(start), err)
		return res, nil
	}

	// Enrich with student and parent profiles from identity DB
	studentIDs := make([]int, 0, len(rowMaps))
	for _, row := range rowMaps {
		if sid, ok := row["student_id"].(int64); ok {
			studentIDs = append(studentIDs, int(sid))
		} else if sid, ok := row["student_id"].(int); ok {
			studentIDs = append(studentIDs, sid)
		}
	}
	profiles := fetchStudentProfiles(ctx, studentIDs)
	for _, row := range rowMaps {
		var sid int
		if idVal, ok := row["student_id"].(int64); ok {
			sid = int(idVal)
		} else if idVal, ok := row["student_id"].(int); ok {
			sid = idVal
		}
		if p, found := profiles[sid]; found {
			row["student_name"] = p["name"]
			row["school"] = p["school"]
			row["parent_name"] = p["parent_name"]
			row["parent_phone"] = p["parent_phone"]
		}
	}

	logger.LogToolCall("get_absent_alerts", fmt.Sprintf("threshold=%d year=%d count=%d", threshold, schoolYearID, len(rowMaps)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"threshold":    threshold,
		"schoolYearId": schoolYearID,
		"centerId":     centerID,
		"alerts":       rowMaps,
	}), nil
}

func handleGetClassAcademicSummary(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	classID := r.GetInt("class_id", 0)
	if classID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"class_id\""))
		logger.LogToolCall("get_class_academic_summary", "", time.Since(start), fmt.Errorf("invalid class_id"))
		return res, nil
	}

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_class_academic_summary", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}

	rows, err := pool.QueryContext(ctx, `
		SELECT ar.id, ar.student_id, ar.score, ar.comment, ar.btvn_score, ar.btvn_comment
		FROM edu_student_academic_result ar
		WHERE ar.deleted_at IS NULL
		ORDER BY ar.id DESC LIMIT 50`)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_class_academic_summary", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	rowMaps, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_class_academic_summary", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}

	logger.LogToolCall("get_class_academic_summary", fmt.Sprintf("class=%d count=%d", classID, len(rowMaps)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"classId":  classID,
		"academic": rowMaps,
	}), nil
}

func handleCheckStudentClassEligibility(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	studentID := r.GetInt("student_id", 0)
	if studentID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"student_id\""))
		logger.LogToolCall("check_student_class_eligibility", "", time.Since(start), fmt.Errorf("invalid student_id"))
		return res, nil
	}

	pool, err := db.GetPool("finance")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("check_student_class_eligibility", fmt.Sprintf("student=%d", studentID), time.Since(start), err)
		return res, nil
	}

	rows, err := pool.QueryContext(ctx, `
		SELECT i.id AS invoice_id, i.paid_status, i.total_amount, i.paid_amount, i.due_date
		FROM fin_invoices i
		WHERE i.student_id = ?
		ORDER BY i.id DESC LIMIT 10`, studentID)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("check_student_class_eligibility", fmt.Sprintf("student=%d", studentID), time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	rowMaps, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("check_student_class_eligibility", fmt.Sprintf("student=%d", studentID), time.Since(start), err)
		return res, nil
	}

	logger.LogToolCall("check_student_class_eligibility", fmt.Sprintf("student=%d count=%d", studentID, len(rowMaps)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"studentId": studentID,
		"invoices":  rowMaps,
	}), nil
}

// ============================================================================
// NEW TOOLS - Payment History, Enrollment History, Class Debt
// ============================================================================

func handleGetStudentPaymentHistory(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	studentID := r.GetInt("student_id", 0)
	if studentID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"student_id\""))
		logger.LogToolCall("get_student_payment_history", "", time.Since(start), fmt.Errorf("invalid student_id"))
		return res, nil
	}

	fromDate := r.GetString("from", "")
	toDate := r.GetString("to", "")
	limit := clampLimit(r.GetInt("limit", defaultLimit))

	pool, err := db.GetPool("finance")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_student_payment_history", fmt.Sprintf("student=%d", studentID), time.Since(start), err)
		return res, nil
	}

	// Build WHERE clause for date filtering
	whereClause := "WHERE i.student_id = ?"
	params := []any{studentID}
	if fromDate != "" {
		whereClause += " AND i.created_at >= ?"
		params = append(params, fromDate)
	}
	if toDate != "" {
		whereClause += " AND i.created_at <= ?"
		params = append(params, toDate)
	}

	query := fmt.Sprintf(`
		SELECT
			i.id AS invoice_id,
			i.billing_period,
			i.total_amount,
			i.paid_amount,
			i.paid_status,
			i.due_date,
			i.created_at AS invoice_date,
			ra.id AS allocation_id,
			ra.amount_applied,
			ra.allocation_date,
			r.id AS receipt_id,
			r.date AS receipt_date,
			r.method AS receipt_method,
			r.amount AS receipt_amount
		FROM fin_invoices i
		LEFT JOIN fin_receipt_allocations ra ON ra.invoice_id = i.id
		LEFT JOIN fin_receipts r ON r.id = ra.receipt_id
		%s
		ORDER BY i.due_date DESC, ra.allocation_date DESC
		LIMIT %d
	`, whereClause, limit)

	rows, err := pool.QueryContext(ctx, query, params...)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_student_payment_history", fmt.Sprintf("student=%d", studentID), time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	rowMaps, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_student_payment_history", fmt.Sprintf("student=%d", studentID), time.Since(start), err)
		return res, nil
	}

	logger.LogToolCall("get_student_payment_history", fmt.Sprintf("student=%d count=%d", studentID, len(rowMaps)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"studentId": studentID,
		"from":      fromDate,
		"to":        toDate,
		"payments":  rowMaps,
	}), nil
}

func handleGetStudentEnrollmentHistory(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	studentID := r.GetInt("student_id", 0)
	if studentID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"student_id\""))
		logger.LogToolCall("get_student_enrollment_history", "", time.Since(start), fmt.Errorf("invalid student_id"))
		return res, nil
	}

	// Default to current school year (2026-2027) if not specified
	schoolYearID := r.GetInt("school_year_id", config.DefaultSchoolYearID)
	activeOnly := r.GetBool("active_only", false)
	limit := clampLimit(r.GetInt("limit", defaultLimit))

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_student_enrollment_history", fmt.Sprintf("student=%d", studentID), time.Since(start), err)
		return res, nil
	}

	// Query 1: Current enrollments
	currentWhereClause := "WHERE sc.student_id = ? AND c.year_id = ?"
	currentParams := []any{studentID, schoolYearID}
	if activeOnly {
		currentWhereClause += " AND (sc.leave_date IS NULL OR sc.leave_date >= CURDATE())"
	}

	currentQuery := fmt.Sprintf(`
		SELECT
			sc.id AS enrollment_id,
			sc.student_id,
			sc.class_id,
			sc.entrance_date,
			sc.leave_date,
			sc.status,
			sc.tuition_fee,
			c.name AS class_name,
			c.code AS class_code,
			c.grade,
			sy.id AS school_year_id,
			sy.name AS school_year_name
		FROM edu_student_class sc
		JOIN edu_classes c ON sc.class_id = c.id
		LEFT JOIN edu_school_years sy ON c.year_id = sy.id
		%s
		ORDER BY sc.entrance_date DESC
		LIMIT %d
	`, currentWhereClause, limit)

	currentRows, err := pool.QueryContext(ctx, currentQuery, currentParams...)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_student_enrollment_history", fmt.Sprintf("student=%d", studentID), time.Since(start), err)
		return res, nil
	}
	defer currentRows.Close()

	currentEnrollments, err := rowsToMaps(currentRows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_student_enrollment_history", fmt.Sprintf("student=%d", studentID), time.Since(start), err)
		return res, nil
	}

	// Query 2: Historical action log
	historyQuery := `
		SELECT
			l.id AS log_id,
			l.student_id,
			l.class_id,
			l.action,
			l.effective_from,
			l.note,
			l.solution,
			l.completed,
			c.name AS class_name,
			c.code AS class_code
		FROM edu_student_class_log l
		JOIN edu_classes c ON l.class_id = c.id
		WHERE l.student_id = ?
		ORDER BY l.effective_from DESC
		LIMIT ?
	`

	historyRows, err := pool.QueryContext(ctx, historyQuery, studentID, limit)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_student_enrollment_history", fmt.Sprintf("student=%d", studentID), time.Since(start), err)
		return res, nil
	}
	defer historyRows.Close()

	history, err := rowsToMaps(historyRows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_student_enrollment_history", fmt.Sprintf("student=%d", studentID), time.Since(start), err)
		return res, nil
	}

	logger.LogToolCall("get_student_enrollment_history", fmt.Sprintf("student=%d current=%d history=%d", studentID, len(currentEnrollments), len(history)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"studentId":          studentID,
		"schoolYearId":       schoolYearID,
		"activeOnly":         activeOnly,
		"currentEnrollments": currentEnrollments,
		"history":            history,
	}), nil
}

func handleGetClassDebtSummary(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	classID := r.GetInt("class_id", 0)
	if classID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"class_id\""))
		logger.LogToolCall("get_class_debt_summary", "", time.Since(start), fmt.Errorf("invalid class_id"))
		return res, nil
	}

	minDaysOverdue := r.GetInt("min_days_overdue", 0)
	limit := clampLimit(r.GetInt("limit", defaultLimit))

	pool, err := db.GetPool("finance")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_class_debt_summary", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}

	// Query 1: Aggregate summary
	summaryQuery := `
		SELECT
			COUNT(DISTINCT i.id) AS total_invoices,
			SUM(i.total_amount) AS total_billed,
			SUM(i.paid_amount) AS total_paid,
			SUM(i.total_amount - i.paid_amount) AS total_outstanding
		FROM fin_invoices i
		JOIN fin_invoice_detail d ON d.invoice_id = i.id
		WHERE d.class_id = ? AND i.paid_status != 'paid'
	`

	var summary struct {
		TotalInvoices    int
		TotalBilled      float64
		TotalPaid        float64
		TotalOutstanding float64
	}

	err = pool.QueryRowContext(ctx, summaryQuery, classID).Scan(
		&summary.TotalInvoices,
		&summary.TotalBilled,
		&summary.TotalPaid,
		&summary.TotalOutstanding,
	)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_class_debt_summary", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}

	// Query 2: List unpaid students
	unpaidWhereClause := "WHERE d.class_id = ? AND i.paid_status != 'paid'"
	unpaidParams := []any{classID}
	if minDaysOverdue > 0 {
		unpaidWhereClause += " AND i.days_overdue >= ?"
		unpaidParams = append(unpaidParams, minDaysOverdue)
	}

	unpaidQuery := fmt.Sprintf(`
		SELECT
			i.student_id,
			i.id AS invoice_id,
			i.billing_period,
			i.total_amount,
			i.paid_amount,
			(i.total_amount - i.paid_amount) AS outstanding,
			i.due_date,
			i.days_overdue,
			i.debt_label
		FROM fin_invoices i
		JOIN fin_invoice_detail d ON d.invoice_id = i.id
		%s
		ORDER BY i.days_overdue DESC
		LIMIT %d
	`, unpaidWhereClause, limit)

	unpaidRows, err := pool.QueryContext(ctx, unpaidQuery, unpaidParams...)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_class_debt_summary", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}
	defer unpaidRows.Close()

	unpaidStudents, err := rowsToMaps(unpaidRows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_class_debt_summary", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}

	logger.LogToolCall("get_class_debt_summary", fmt.Sprintf("class=%d unpaid=%d", classID, len(unpaidStudents)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"classId": classID,
		"summary": map[string]any{
			"totalInvoices":    summary.TotalInvoices,
			"totalBilled":      summary.TotalBilled,
			"totalPaid":        summary.TotalPaid,
			"totalOutstanding": summary.TotalOutstanding,
		},
		"unpaidStudents": unpaidStudents,
	}), nil
}

// ============================================================================
// CAPABILITY GAP MANAGEMENT TOOLS
// ============================================================================

func handleLogCapabilityGap(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	question, err := r.RequireString("question")
	if err != nil {
		logger.LogToolCall("log_capability_gap", "", time.Since(start), err)
		return errorResult(err), nil
	}

	database := r.GetString("database", "")
	attemptedQuery := r.GetString("attempted_query", "")
	suggestedTool := r.GetString("suggested_tool_name", "")
	notes := r.GetString("notes", "")

	if err := db.RecordGap("explicit_report", database, question, attemptedQuery, suggestedTool, notes); err != nil {
		logger.LogToolCall("log_capability_gap", fmt.Sprintf("question=%q", question), time.Since(start), err)
		return errorResult(fmt.Errorf("failed to record gap: %w", err)), nil
	}

	logger.LogToolCall("log_capability_gap", fmt.Sprintf("question=%q database=%s", question, database), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"status":   "recorded",
		"question": question,
		"database": database,
	}), nil
}

func handleSummarizeCapabilityGaps(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	minOccurrences := r.GetInt("min_occurrences", 1)
	limit := clampLimit(r.GetInt("limit", defaultLimit))

	gaps, err := db.QueryGaps(minOccurrences, limit)
	if err != nil {
		logger.LogToolCall("summarize_capability_gaps", "", time.Since(start), err)
		return errorResult(fmt.Errorf("failed to query gaps: %w", err)), nil
	}

	logger.LogToolCall("summarize_capability_gaps", fmt.Sprintf("min=%d limit=%d count=%d", minOccurrences, limit, len(gaps)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"minOccurrences": minOccurrences,
		"limit":          limit,
		"totalGaps":      len(gaps),
		"gaps":           gaps,
	}), nil
}
