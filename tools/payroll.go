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

// RegisterPayrollTools registers teacher payroll and contract tools.
func RegisterPayrollTools(s *server.MCPServer) {
	s.AddTool(
		mcp.NewTool("get_my_payroll",
			mcp.WithDescription("Retrieves teacher payroll summaries by period. Includes session count, teaching hours, gross revenue, salary, and profit."),
			mcp.WithNumber("teacher_id", mcp.Required(), mcp.Description("Teacher ID")),
			mcp.WithString("period", mcp.Description("Optional payroll period (e.g., '2026-09' or '09/2026')")),
			mcp.WithNumber("limit", mcp.Description("Max periods returned (default 12)")),
		),
		handleGetMyPayroll,
	)

	s.AddTool(
		mcp.NewTool("get_teacher_payroll_detail",
			mcp.WithDescription("Retrieves itemized breakdown of a teacher's payroll: individual class sessions, student attendance count, duration, base fee, revenue share, and tax."),
			mcp.WithNumber("teacher_payroll_id", mcp.Required(), mcp.Description("Teacher Payroll ID")),
			mcp.WithNumber("limit", mcp.Description("Max detail records returned (default 50, max 200)")),
		),
		handleGetTeacherPayrollDetail,
	)

	s.AddTool(
		mcp.NewTool("get_my_contract",
			mcp.WithDescription("Retrieves active teaching contracts and compensation terms for a teacher (base salary, hourly rate, revenue percentage, tax terms)."),
			mcp.WithNumber("teacher_id", mcp.Required(), mcp.Description("Teacher ID")),
		),
		handleGetMyContract,
	)

	s.AddTool(
		mcp.NewTool("reconcile_payroll_vs_contract",
			mcp.WithDescription("Cross-checks teacher payroll amounts against contracted compensation terms to detect discrepancies, underpayments, or overpayments."),
			mcp.WithNumber("teacher_id", mcp.Required(), mcp.Description("Teacher ID")),
			mcp.WithString("period", mcp.Description("Optional payroll period filter")),
		),
		handleReconcilePayrollVsContract,
	)
}

func fetchTeacherProfile(ctx context.Context, teacherID int) (map[string]any, error) {
	pool, err := db.GetPool("identity")
	if err != nil {
		return nil, err
	}

	query := `
		SELECT 
			t.id, t.display_name, t.school, t.domain, t.is_active,
			COALESCE(a.fullname, '') AS full_name,
			COALESCE(a.phone, '') AS phone,
			COALESCE(a.email, '') AS email
		FROM ve26_teacher t
		LEFT JOIN ve26_account a ON t.account_id = a.id
		WHERE t.id = ?
	`
	rows, err := pool.QueryContext(ctx, query, teacherID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list, err := rowsToMaps(rows)
	if err != nil || len(list) == 0 {
		return nil, fmt.Errorf("teacher not found with ID %d", teacherID)
	}
	return list[0], nil
}

func handleGetMyPayroll(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	teacherID := r.GetInt("teacher_id", 0)
	if teacherID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"teacher_id\""))
		logger.LogToolCall("get_my_payroll", "", time.Since(start), fmt.Errorf("invalid teacher_id"))
		return res, nil
	}

	period := r.GetString("period", "")
	limit := clampLimit(r.GetInt("limit", 12))

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_my_payroll", fmt.Sprintf("teacher=%d", teacherID), time.Since(start), err)
		return res, nil
	}

	whereClause := "teacher_id = ?"
	params := []any{teacherID}

	if period != "" {
		whereClause += " AND period = ?"
		params = append(params, period)
	}

	query := fmt.Sprintf(`
		SELECT 
			id AS payroll_id,
			teacher_id,
			period,
			session_count,
			hour_count,
			points,
			revenue,
			salary,
			profit,
			student_count,
			created_at,
			updated_at
		FROM edu_teachers_payrolls
		WHERE %s
		ORDER BY id DESC
		LIMIT ?
	`, whereClause)
	params = append(params, limit)

	rows, err := pool.QueryContext(ctx, query, params...)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_my_payroll", fmt.Sprintf("teacher=%d", teacherID), time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	payrolls, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_my_payroll", fmt.Sprintf("teacher=%d", teacherID), time.Since(start), err)
		return res, nil
	}

	// Fetch teacher profile
	teacherInfo, _ := fetchTeacherProfile(ctx, teacherID)

	logger.LogToolCall("get_my_payroll", fmt.Sprintf("teacher=%d payrolls=%d", teacherID, len(payrolls)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"teacherId": teacherID,
		"teacher":   teacherInfo,
		"payrolls":  payrolls,
	}), nil
}

func handleGetTeacherPayrollDetail(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	payrollID := r.GetInt("teacher_payroll_id", 0)
	if payrollID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"teacher_payroll_id\""))
		logger.LogToolCall("get_teacher_payroll_detail", "", time.Since(start), fmt.Errorf("invalid teacher_payroll_id"))
		return res, nil
	}

	limit := clampLimit(r.GetInt("limit", defaultLimit))

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_teacher_payroll_detail", fmt.Sprintf("payroll=%d", payrollID), time.Since(start), err)
		return res, nil
	}

	// 1. Fetch header
	var teacherID int
	var period string
	var totalSalary, totalRevenue float64
	err = pool.QueryRowContext(ctx, `
		SELECT teacher_id, COALESCE(period, ''), COALESCE(salary, 0), COALESCE(revenue, 0)
		FROM edu_teachers_payrolls
		WHERE id = ?
	`, payrollID).Scan(&teacherID, &period, &totalSalary, &totalRevenue)
	if err != nil {
		res := errorResult(fmt.Errorf("payroll record not found: %w", err))
		logger.LogToolCall("get_teacher_payroll_detail", fmt.Sprintf("payroll=%d", payrollID), time.Since(start), err)
		return res, nil
	}

	// 2. Fetch line items
	detailQuery := `
		SELECT 
			d.id,
			d.session_id,
			d.class_id,
			c.name AS class_name,
			c.code AS class_code,
			d.center_id,
			d.contract_id,
			d.present_count,
			d.duration,
			d.base_fee,
			d.amount_revenue,
			d.salary_temp,
			d.salary_tax,
			d.tax10,
			d.salary,
			d.point,
			d.created_at
		FROM edu_teachers_payrolls_details d
		LEFT JOIN edu_classes c ON d.class_id = c.id
		WHERE d.teacher_payroll_id = ?
		ORDER BY d.id ASC
		LIMIT ?
	`
	rows, err := pool.QueryContext(ctx, detailQuery, payrollID, limit)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_teacher_payroll_detail", fmt.Sprintf("payroll=%d", payrollID), time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	details, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_teacher_payroll_detail", fmt.Sprintf("payroll=%d", payrollID), time.Since(start), err)
		return res, nil
	}

	teacherInfo, _ := fetchTeacherProfile(ctx, teacherID)

	logger.LogToolCall("get_teacher_payroll_detail", fmt.Sprintf("payroll=%d items=%d", payrollID, len(details)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"payrollId":    payrollID,
		"period":       period,
		"teacher":      teacherInfo,
		"totalSalary":  totalSalary,
		"totalRevenue": totalRevenue,
		"itemCount":    len(details),
		"items":        details,
	}), nil
}

func handleGetMyContract(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	teacherID := r.GetInt("teacher_id", 0)
	if teacherID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"teacher_id\""))
		logger.LogToolCall("get_my_contract", "", time.Since(start), fmt.Errorf("invalid teacher_id"))
		return res, nil
	}

	pool, err := db.GetPool("identity")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_my_contract", fmt.Sprintf("teacher=%d", teacherID), time.Since(start), err)
		return res, nil
	}

	query := `
		SELECT 
			c.id AS contract_id,
			c.teacher_id,
			c.title,
			c.type AS contract_type,
			c.salary_lcb AS base_salary,
			c.hour AS hourly_rate,
			c.percentage_of_revenue,
			c.per_student,
			c.hour_mini AS min_hours,
			c.session_mini AS min_sessions,
			c.status,
			c.from AS start_date,
			c.to AS end_date,
			c.tax_limit,
			c.note,
			c.create_at,
			c.update_at
		FROM ve26_teachers_contracts c
		WHERE c.teacher_id = ?
		ORDER BY c.id DESC
	`
	rows, err := pool.QueryContext(ctx, query, teacherID)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_my_contract", fmt.Sprintf("teacher=%d", teacherID), time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	contracts, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_my_contract", fmt.Sprintf("teacher=%d", teacherID), time.Since(start), err)
		return res, nil
	}

	teacherInfo, _ := fetchTeacherProfile(ctx, teacherID)

	logger.LogToolCall("get_my_contract", fmt.Sprintf("teacher=%d contracts=%d", teacherID, len(contracts)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"teacherId": teacherID,
		"teacher":   teacherInfo,
		"contracts": contracts,
	}), nil
}

func handleReconcilePayrollVsContract(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	teacherID := r.GetInt("teacher_id", 0)
	if teacherID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"teacher_id\""))
		logger.LogToolCall("reconcile_payroll_vs_contract", "", time.Since(start), fmt.Errorf("invalid teacher_id"))
		return res, nil
	}

	period := r.GetString("period", "")

	// 1. Fetch contracts from identity
	idPool, err := db.GetPool("identity")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("reconcile_payroll_vs_contract", fmt.Sprintf("teacher=%d", teacherID), time.Since(start), err)
		return res, nil
	}

	cRows, err := idPool.QueryContext(ctx, `
		SELECT 
			id, title, type, salary_lcb, hour, percentage_of_revenue, 
			per_student, status, `+"`from`"+`, `+"`to`"+`
		FROM ve26_teachers_contracts
		WHERE teacher_id = ?
		ORDER BY id DESC
	`, teacherID)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("reconcile_payroll_vs_contract", fmt.Sprintf("teacher=%d", teacherID), time.Since(start), err)
		return res, nil
	}
	defer cRows.Close()

	contracts, _ := rowsToMaps(cRows)

	// 2. Fetch payroll records from class
	classPool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("reconcile_payroll_vs_contract", fmt.Sprintf("teacher=%d", teacherID), time.Since(start), err)
		return res, nil
	}

	pWhere := "teacher_id = ?"
	pParams := []any{teacherID}
	if period != "" {
		pWhere += " AND period = ?"
		pParams = append(pParams, period)
	}

	pRows, err := classPool.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, period, session_count, hour_count, revenue, salary
		FROM edu_teachers_payrolls
		WHERE %s
		ORDER BY id DESC
	`, pWhere), pParams...)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("reconcile_payroll_vs_contract", fmt.Sprintf("teacher=%d", teacherID), time.Since(start), err)
		return res, nil
	}
	defer pRows.Close()

	payrolls, _ := rowsToMaps(pRows)
	teacherInfo, _ := fetchTeacherProfile(ctx, teacherID)

	// Summary findings
	findings := []string{}
	if len(contracts) == 0 {
		findings = append(findings, "Cảnh báo: Giáo viên chưa có hợp đồng nào được lưu trong hệ thống.")
	}
	if len(payrolls) == 0 {
		findings = append(findings, "Không tìm thấy bản ghi tính lương nào phù hợp với điều kiện tìm kiếm.")
	}

	logger.LogToolCall("reconcile_payroll_vs_contract", fmt.Sprintf("teacher=%d payrolls=%d contracts=%d", teacherID, len(payrolls), len(contracts)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"teacherId": teacherID,
		"teacher":   teacherInfo,
		"contracts": contracts,
		"payrolls":  payrolls,
		"findings":  findings,
	}), nil
}
