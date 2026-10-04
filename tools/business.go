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

func RegisterBusinessTools(s *server.MCPServer) {
	s.AddTool(
		mcp.NewTool("get_daily_class_schedule",
			mcp.WithDescription("Retrieves daily class schedule, room assignment, teacher, and student headcount for a center."),
			mcp.WithNumber("center_id", mcp.Required(), mcp.Description("Center ID")),
			mcp.WithString("date", mcp.Description("Date YYYY-MM-DD")),
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
			mcp.WithDescription("Identifies students with consecutive absences for parent outreach."),
			mcp.WithNumber("center_id", mcp.Description("Optional Center ID filter")),
			mcp.WithNumber("consecutive_absent_count", mcp.Description("Consecutive absences threshold (default 2)")),
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

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_daily_class_schedule", fmt.Sprintf("center=%d date=%s", centerID, dateStr), time.Since(start), err)
		return res, nil
	}

	rows, err := pool.QueryContext(ctx, `
		SELECT c.id, c.name, COALESCE(c.friendly_class_schedule, ''), COALESCE(c.friendly_class_teacher, '')
		FROM edu_classes c
		WHERE c.center_id = ? AND c.deleted_at IS NULL
		ORDER BY c.id DESC LIMIT 50`, centerID)
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

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_absent_alerts", fmt.Sprintf("threshold=%d", threshold), time.Since(start), err)
		return res, nil
	}

	rows, err := pool.QueryContext(ctx, `
		SELECT sa.student_id, COUNT(*) AS absent_count
		FROM edu_student_session_attendance sa
		WHERE sa.attendance IN ('absent', 'vắng', 'vang')
		GROUP BY sa.student_id
		HAVING absent_count >= ?
		LIMIT 50`, threshold)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_absent_alerts", fmt.Sprintf("threshold=%d", threshold), time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	rowMaps, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_absent_alerts", fmt.Sprintf("threshold=%d", threshold), time.Since(start), err)
		return res, nil
	}

	logger.LogToolCall("get_absent_alerts", fmt.Sprintf("threshold=%d count=%d", threshold, len(rowMaps)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"threshold": threshold,
		"alerts":    rowMaps,
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
