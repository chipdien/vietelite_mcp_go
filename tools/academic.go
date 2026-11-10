package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"vietelite_mcp_go/db"
	"vietelite_mcp_go/logger"
)

// RegisterAcademicTools registers class and academic operational tools.
func RegisterAcademicTools(s *server.MCPServer) {
	s.AddTool(
		mcp.NewTool("get_class_detail",
			mcp.WithDescription("Retrieves comprehensive details of a class: schedule configs, room, teacher, course, fee, active headcount, and staff."),
			mcp.WithNumber("class_id", mcp.Required(), mcp.Description("Class ID")),
		),
		handleGetClassDetail,
	)

	s.AddTool(
		mcp.NewTool("get_session_detail",
			mcp.WithDescription("Retrieves all details of a single class session: teacher, time, room, lesson materials/documents, attendance breakdown, and homework completion stats."),
			mcp.WithNumber("session_id", mcp.Required(), mcp.Description("Session ID")),
		),
		handleGetSessionDetail,
	)

	s.AddTool(
		mcp.NewTool("list_students_at_risk",
			mcp.WithDescription("Identifies students at high academic or attendance risk in a class based on absences, missing homework, and low test scores."),
			mcp.WithNumber("class_id", mcp.Required(), mcp.Description("Class ID")),
			mcp.WithNumber("threshold_score", mcp.Description("Minimum acceptable score threshold (default 5.0)")),
			mcp.WithNumber("min_absences", mcp.Description("Minimum absences threshold to flag as at-risk (default 2)")),
			mcp.WithNumber("limit", mcp.Description("Max at-risk students returned (default 50, max 200)")),
		),
		handleListStudentsAtRisk,
	)

	s.AddTool(
		mcp.NewTool("list_ungraded_homework",
			mcp.WithDescription("Lists homework submissions in a class that are awaiting grading or score entry."),
			mcp.WithNumber("class_id", mcp.Required(), mcp.Description("Class ID")),
			mcp.WithString("from", mcp.Description("Filter sessions from date YYYY-MM-DD")),
			mcp.WithString("to", mcp.Description("Filter sessions to date YYYY-MM-DD")),
			mcp.WithNumber("limit", mcp.Description("Max submissions returned (default 50, max 200)")),
		),
		handleListUngradedHomework,
	)

	s.AddTool(
		mcp.NewTool("get_homework_submissions",
			mcp.WithDescription("Retrieves all student homework submissions, completion flags, scores, and teacher comments for a specific session."),
			mcp.WithNumber("session_id", mcp.Required(), mcp.Description("Session ID")),
		),
		handleGetHomeworkSubmissions,
	)
}

func handleGetClassDetail(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	classID := r.GetInt("class_id", 0)
	if classID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"class_id\""))
		logger.LogToolCall("get_class_detail", "", time.Since(start), fmt.Errorf("invalid class_id"))
		return res, nil
	}

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_class_detail", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}

	// 1. Core class query
	classQuery := `
		SELECT 
			c.id, c.name, c.code, c.active, c.open_date, c.note, c.fee, c.cost,
			c.center_id, c.course_id, c.grade, c.ss_number, c.class_type,
			c.friendly_class_schedule, c.friendly_class_teacher,
			co.name AS course_name, co.grade AS course_grade, co.level AS course_level,
			sy.name AS school_year_name
		FROM edu_classes c
		LEFT JOIN edu_courses co ON c.course_id = co.id
		LEFT JOIN edu_school_years sy ON c.year_id = sy.id
		WHERE c.id = ? AND (c.deleted_at IS NULL)
	`
	classRows, err := pool.QueryContext(ctx, classQuery, classID)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_class_detail", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}
	defer classRows.Close()

	classDataList, err := rowsToMaps(classRows)
	if err != nil || len(classDataList) == 0 {
		if err == nil {
			err = fmt.Errorf("class not found with ID %d", classID)
		}
		res := errorResult(err)
		logger.LogToolCall("get_class_detail", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}
	classInfo := classDataList[0]

	// 2. Schedule configurations
	configQuery := `
		SELECT id, teacher_id, date, start_time, end_time, room_id, ss_number, note, status
		FROM edu_class_configs
		WHERE class_id = ? AND (status = 1 OR status IS NULL)
		ORDER BY date ASC, start_time ASC
	`
	configRows, err := pool.QueryContext(ctx, configQuery, classID)
	var configs []map[string]any
	if err == nil {
		configs, _ = rowsToMaps(configRows)
		configRows.Close()
	}

	// 3. Active student headcount
	var activeStudents int
	_ = pool.QueryRowContext(ctx, `
		SELECT COUNT(*) 
		FROM edu_student_class 
		WHERE class_id = ? AND (status = 'active' OR status IS NULL) AND leave_date IS NULL
	`, classID).Scan(&activeStudents)

	// 4. Staffs
	staffRows, err := pool.QueryContext(ctx, `SELECT id, account_id FROM edu_class_staffs WHERE class_id = ?`, classID)
	var staffs []map[string]any
	if err == nil {
		staffs, _ = rowsToMaps(staffRows)
		staffRows.Close()
	}

	// 5. Optional center details from identity
	var centerInfo any = nil
	if centerIDVal, ok := classInfo["center_id"]; ok && centerIDVal != nil {
		if idPool, idErr := db.GetPool("identity"); idErr == nil {
			cRow, cErr := idPool.QueryContext(ctx, `SELECT id, name, code, phone, address FROM ve26_centers WHERE id = ?`, centerIDVal)
			if cErr == nil {
				cList, _ := rowsToMaps(cRow)
				cRow.Close()
				if len(cList) > 0 {
					centerInfo = cList[0]
				}
			}
		}
	}

	logger.LogToolCall("get_class_detail", fmt.Sprintf("class=%d active=%d", classID, activeStudents), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"classId":            classID,
		"classInfo":          classInfo,
		"scheduleConfigs":    configs,
		"activeStudentCount": activeStudents,
		"staffs":             staffs,
		"center":             centerInfo,
	}), nil
}

func handleGetSessionDetail(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	sessionID := r.GetInt("session_id", 0)
	if sessionID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"session_id\""))
		logger.LogToolCall("get_session_detail", "", time.Since(start), fmt.Errorf("invalid session_id"))
		return res, nil
	}

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_session_detail", fmt.Sprintf("session=%d", sessionID), time.Since(start), err)
		return res, nil
	}

	// 1. Core session info
	sessionQuery := `
		SELECT 
			s.id, s.class_id, s.center_id, s.room_id, s.teacher_id,
			s.date, s.from, s.to, s.status, s.type, s.note, s.ss_number,
			s.student_present, s.is_locked, s.session_type,
			c.name AS class_name, c.code AS class_code
		FROM edu_sessions s
		JOIN edu_classes c ON s.class_id = c.id
		WHERE s.id = ? AND s.is_deleted = 0
	`
	sessionRows, err := pool.QueryContext(ctx, sessionQuery, sessionID)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_session_detail", fmt.Sprintf("session=%d", sessionID), time.Since(start), err)
		return res, nil
	}
	defer sessionRows.Close()

	sessionDataList, err := rowsToMaps(sessionRows)
	if err != nil || len(sessionDataList) == 0 {
		if err == nil {
			err = fmt.Errorf("session not found with ID %d", sessionID)
		}
		res := errorResult(err)
		logger.LogToolCall("get_session_detail", fmt.Sprintf("session=%d", sessionID), time.Since(start), err)
		return res, nil
	}
	sessionInfo := sessionDataList[0]

	// 2. Documents
	docRows, err := pool.QueryContext(ctx, `
		SELECT id, name, type, file_url, note, created_at
		FROM edu_session_documents
		WHERE edu_session_id = ?
		ORDER BY id ASC
	`, sessionID)
	var documents []map[string]any
	if err == nil {
		documents, _ = rowsToMaps(docRows)
		docRows.Close()
	}

	// 3. Attendance breakdown
	attRows, err := pool.QueryContext(ctx, `
		SELECT attendance, COUNT(*) AS count
		FROM edu_student_session_attendance
		WHERE session_id = ? AND deleted_at IS NULL
		GROUP BY attendance
	`, sessionID)
	attStats := make(map[string]int)
	if err == nil {
		for attRows.Next() {
			var att sqlNullString
			var count int
			if err := attRows.Scan(&att.String, &count); err == nil {
				key := att.String
				if key == "" {
					key = "unrecorded"
				}
				attStats[key] = count
			}
		}
		attRows.Close()
	}

	// 4. Homework completion stats
	var hwTotal, hwComplete, hwIncomplete, hwGraded int
	_ = pool.QueryRowContext(ctx, `
		SELECT 
			COUNT(*),
			SUM(CASE WHEN btvn_complete = '1' OR btvn_complete = 'true' THEN 1 ELSE 0 END),
			SUM(CASE WHEN btvn_complete = '0' OR btvn_complete = 'false' THEN 1 ELSE 0 END),
			SUM(CASE WHEN btvn_score IS NOT NULL AND btvn_score != '' THEN 1 ELSE 0 END)
		FROM edu_student_academic_result
		WHERE session_id = ? AND deleted_at IS NULL
	`, sessionID).Scan(&hwTotal, &hwComplete, &hwIncomplete, &hwGraded)

	logger.LogToolCall("get_session_detail", fmt.Sprintf("session=%d docs=%d", sessionID, len(documents)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"sessionId":       sessionID,
		"sessionInfo":     sessionInfo,
		"documents":       documents,
		"attendanceStats": attStats,
		"homeworkStats": map[string]int{
			"totalRecords":    hwTotal,
			"completedCount":  hwComplete,
			"incompleteCount": hwIncomplete,
			"gradedCount":     hwGraded,
		},
	}), nil
}

type sqlNullString struct {
	String string
}

func (s *sqlNullString) Scan(value any) error {
	if value == nil {
		s.String = ""
		return nil
	}
	switch v := value.(type) {
	case []byte:
		s.String = string(v)
	case string:
		s.String = v
	default:
		s.String = fmt.Sprintf("%v", v)
	}
	return nil
}

func handleListStudentsAtRisk(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	classID := r.GetInt("class_id", 0)
	if classID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"class_id\""))
		logger.LogToolCall("list_students_at_risk", "", time.Since(start), fmt.Errorf("invalid class_id"))
		return res, nil
	}

	thresholdScore := r.GetFloat("threshold_score", 5.0)
	minAbsences := r.GetInt("min_absences", 2)
	limit := clampLimit(r.GetInt("limit", defaultLimit))

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("list_students_at_risk", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}

	// 1. Fetch active students in class
	activeRows, err := pool.QueryContext(ctx, `
		SELECT student_id 
		FROM edu_student_class 
		WHERE class_id = ? AND (status = 'active' OR status IS NULL) AND leave_date IS NULL
	`, classID)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("list_students_at_risk", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}
	defer activeRows.Close()

	var studentIDs []int
	for activeRows.Next() {
		var sID int
		if err := activeRows.Scan(&sID); err == nil {
			studentIDs = append(studentIDs, sID)
		}
	}
	if len(studentIDs) == 0 {
		logger.LogToolCall("list_students_at_risk", fmt.Sprintf("class=%d no active students", classID), time.Since(start), nil)
		return mcp.NewToolResultStructuredOnly(map[string]any{
			"classId":        classID,
			"atRiskStudents": []any{},
			"totalAtRisk":    0,
		}), nil
	}

	// 2. Query attendance stats
	attMap := make(map[int]struct {
		totalSessions int
		absences      int
	})
	attRows, err := pool.QueryContext(ctx, `
		SELECT 
			a.student_id, 
			COUNT(*) AS total_sessions,
			SUM(CASE WHEN a.attendance IN ('absent', 'vang', 'nghi', 'vang_co_phep', 'vang_khong_phep') OR a.attendance LIKE '%vang%' OR a.attendance LIKE '%nghi%' THEN 1 ELSE 0 END) AS absences
		FROM edu_student_session_attendance a
		JOIN edu_sessions s ON a.session_id = s.id
		WHERE s.class_id = ? AND a.deleted_at IS NULL AND s.is_deleted = 0
		GROUP BY a.student_id
	`, classID)
	if err == nil {
		for attRows.Next() {
			var sID, total, abs int
			if err := attRows.Scan(&sID, &total, &abs); err == nil {
				attMap[sID] = struct {
					totalSessions int
					absences      int
				}{totalSessions: total, absences: abs}
			}
		}
		attRows.Close()
	}

	// 3. Query homework stats
	hwMap := make(map[int]struct {
		totalHW      int
		incompleteHW int
		avgHWScore   float64
	})
	hwRows, err := pool.QueryContext(ctx, `
		SELECT 
			r.student_id, 
			COUNT(*) AS total_homework,
			SUM(CASE WHEN r.btvn_complete = '0' OR r.btvn_complete = 'false' THEN 1 ELSE 0 END) AS incomplete_homework,
			COALESCE(AVG(CASE WHEN r.btvn_score REGEXP '^[0-9]+(\\.[0-9]+)?$' THEN CAST(r.btvn_score AS DECIMAL(5,2)) ELSE NULL END), 0) AS avg_homework_score
		FROM edu_student_academic_result r
		JOIN edu_sessions s ON r.session_id = s.id
		WHERE s.class_id = ? AND r.deleted_at IS NULL AND s.is_deleted = 0
		GROUP BY r.student_id
	`, classID)
	if err == nil {
		for hwRows.Next() {
			var sID, total, incomplete int
			var avgScore float64
			if err := hwRows.Scan(&sID, &total, &incomplete, &avgScore); err == nil {
				hwMap[sID] = struct {
					totalHW      int
					incompleteHW int
					avgHWScore   float64
				}{totalHW: total, incompleteHW: incomplete, avgHWScore: avgScore}
			}
		}
		hwRows.Close()
	}

	// 4. Query test scores
	testMap := make(map[int]struct {
		testsTaken   int
		avgTestScore float64
	})
	testRows, err := pool.QueryContext(ctx, `
		SELECT 
			tr.student_id, 
			COUNT(*) AS tests_taken, 
			COALESCE(AVG(tr.score), 0) AS avg_test_score
		FROM edu_test_results tr
		JOIN edu_tests t ON tr.test_id = t.id
		WHERE t.class_id = ?
		GROUP BY tr.student_id
	`, classID)
	if err == nil {
		for testRows.Next() {
			var sID, taken int
			var avgScore float64
			if err := testRows.Scan(&sID, &taken, &avgScore); err == nil {
				testMap[sID] = struct {
					testsTaken   int
					avgTestScore float64
				}{testsTaken: taken, avgTestScore: avgScore}
			}
		}
		testRows.Close()
	}

	// Enrich student info from identity
	profiles := fetchStudentProfiles(ctx, studentIDs)

	type atRiskItem struct {
		StudentID          int      `json:"studentId"`
		StudentName        string   `json:"studentName"`
		School             string   `json:"school"`
		StudentPhone       string   `json:"studentPhone"`
		ParentName         string   `json:"parentName"`
		ParentPhone        string   `json:"parentPhone"`
		Absences           int      `json:"absences"`
		TotalSessions      int      `json:"totalSessions"`
		IncompleteHomework int      `json:"incompleteHomework"`
		TotalHomework      int      `json:"totalHomework"`
		AvgHomeworkScore   float64  `json:"avgHomeworkScore"`
		AvgTestScore       float64  `json:"avgTestScore"`
		TestsTaken         int      `json:"testsTaken"`
		RiskScore          int      `json:"riskScore"`
		Reasons            []string `json:"reasons"`
	}

	var atRiskList []atRiskItem

	for _, sID := range studentIDs {
		att := attMap[sID]
		hw := hwMap[sID]
		test := testMap[sID]
		prof := profiles[sID]

		name := fmt.Sprintf("Student #%d", sID)
		school, sPhone, pName, pPhone := "", "", "", ""
		if prof != nil {
			if n, ok := prof["studentName"].(string); ok && n != "" {
				name = n
			}
			if s, ok := prof["school"].(string); ok {
				school = s
			}
			if p, ok := prof["studentPhone"].(string); ok {
				sPhone = p
			}
			if pn, ok := prof["parentName"].(string); ok {
				pName = pn
			}
			if pp, ok := prof["parentPhone"].(string); ok {
				pPhone = pp
			}
		}

		var reasons []string
		riskScore := 0

		// Check absence factor
		if att.absences >= minAbsences {
			reasons = append(reasons, fmt.Sprintf("Vắng học %d/%d buổi", att.absences, att.totalSessions))
			riskScore += 2
		}

		// Check homework factor
		if hw.incompleteHW >= 2 || (hw.totalHW > 0 && float64(hw.incompleteHW)/float64(hw.totalHW) >= 0.4) {
			reasons = append(reasons, fmt.Sprintf("Chưa hoàn thành %d/%d bài tập về nhà", hw.incompleteHW, hw.totalHW))
			riskScore++
		}
		if hw.avgHWScore > 0 && hw.avgHWScore < thresholdScore {
			reasons = append(reasons, fmt.Sprintf("Điểm BTVN trung bình thấp (%.1f < %.1f)", hw.avgHWScore, thresholdScore))
			riskScore++
		}

		// Check test score factor
		if test.testsTaken > 0 && test.avgTestScore < thresholdScore {
			reasons = append(reasons, fmt.Sprintf("Điểm kiểm tra trung bình thấp (%.1f < %.1f)", test.avgTestScore, thresholdScore))
			riskScore += 2
		}

		if len(reasons) > 0 {
			atRiskList = append(atRiskList, atRiskItem{
				StudentID:          sID,
				StudentName:        name,
				School:             school,
				StudentPhone:       sPhone,
				ParentName:         pName,
				ParentPhone:        pPhone,
				Absences:           att.absences,
				TotalSessions:      att.totalSessions,
				IncompleteHomework: hw.incompleteHW,
				TotalHomework:      hw.totalHW,
				AvgHomeworkScore:   hw.avgHWScore,
				AvgTestScore:       test.avgTestScore,
				TestsTaken:         test.testsTaken,
				RiskScore:          riskScore,
				Reasons:            reasons,
			})
		}
	}

	// Sort by risk score descending
	sort.Slice(atRiskList, func(i, j int) bool {
		return atRiskList[i].RiskScore > atRiskList[j].RiskScore
	})

	if len(atRiskList) > limit {
		atRiskList = atRiskList[:limit]
	}

	logger.LogToolCall("list_students_at_risk", fmt.Sprintf("class=%d atRisk=%d", classID, len(atRiskList)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"classId":        classID,
		"thresholdScore": thresholdScore,
		"minAbsences":    minAbsences,
		"totalAtRisk":    len(atRiskList),
		"atRiskStudents": atRiskList,
	}), nil
}

func handleListUngradedHomework(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	classID := r.GetInt("class_id", 0)
	if classID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"class_id\""))
		logger.LogToolCall("list_ungraded_homework", "", time.Since(start), fmt.Errorf("invalid class_id"))
		return res, nil
	}

	fromStr := r.GetString("from", "")
	toStr := r.GetString("to", "")
	limit := clampLimit(r.GetInt("limit", defaultLimit))

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("list_ungraded_homework", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}

	whereClause := "s.class_id = ? AND (r.btvn_score IS NULL OR r.btvn_score = '') AND (r.btvn_complete = '1' OR r.btvn_complete = 'true' OR r.btvn_complete IS NOT NULL) AND r.deleted_at IS NULL AND s.is_deleted = 0"
	params := []any{classID}

	if fromStr != "" {
		whereClause += " AND s.date >= ?"
		params = append(params, fromStr)
	}
	if toStr != "" {
		whereClause += " AND s.date <= ?"
		params = append(params, toStr)
	}

	query := fmt.Sprintf(`
		SELECT 
			r.id AS result_id,
			s.id AS session_id,
			s.date AS session_date,
			s.from AS session_start,
			c.name AS class_name,
			c.code AS class_code,
			r.student_id,
			r.btvn_complete,
			r.btvn_comment,
			r.created_at,
			r.updated_at
		FROM edu_student_academic_result r
		JOIN edu_sessions s ON r.session_id = s.id
		JOIN edu_classes c ON s.class_id = c.id
		WHERE %s
		ORDER BY s.date DESC, r.student_id ASC
		LIMIT ?
	`, whereClause)
	params = append(params, limit)

	rows, err := pool.QueryContext(ctx, query, params...)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("list_ungraded_homework", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	items, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("list_ungraded_homework", fmt.Sprintf("class=%d", classID), time.Since(start), err)
		return res, nil
	}

	// Collect student IDs for enrichment
	var studentIDs []int
	for _, item := range items {
		if sIDVal, ok := item["student_id"]; ok {
			if sID, ok := sIDVal.(int64); ok {
				studentIDs = append(studentIDs, int(sID))
			} else if sIDInt, ok := sIDVal.(int); ok {
				studentIDs = append(studentIDs, sIDInt)
			}
		}
	}
	profiles := fetchStudentProfiles(ctx, studentIDs)
	for _, item := range items {
		if sIDVal, ok := item["student_id"]; ok {
			var sID int
			if v, ok := sIDVal.(int64); ok {
				sID = int(v)
			} else if v, ok := sIDVal.(int); ok {
				sID = v
			}
			if prof, ok := profiles[sID]; ok {
				item["studentName"] = prof["studentName"]
				item["school"] = prof["school"]
			}
		}
	}

	logger.LogToolCall("list_ungraded_homework", fmt.Sprintf("class=%d pending=%d", classID, len(items)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"classId":             classID,
		"pendingCount":        len(items),
		"ungradedSubmissions": items,
	}), nil
}

func handleGetHomeworkSubmissions(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	start := time.Now()
	sessionID := r.GetInt("session_id", 0)
	if sessionID <= 0 {
		res := errorResult(fmt.Errorf("missing or invalid required parameter \"session_id\""))
		logger.LogToolCall("get_homework_submissions", "", time.Since(start), fmt.Errorf("invalid session_id"))
		return res, nil
	}

	pool, err := db.GetPool("class")
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_homework_submissions", fmt.Sprintf("session=%d", sessionID), time.Since(start), err)
		return res, nil
	}

	query := `
		SELECT 
			r.id AS result_id,
			r.student_id,
			r.btvn_max,
			r.btvn_score,
			r.btvn_complete,
			r.btvn_complete_percent,
			r.btvn_score_100,
			r.btvn_comment,
			r.comment AS session_comment,
			r.created_at,
			r.updated_at,
			COALESCE(a.attendance, '') AS attendance
		FROM edu_student_academic_result r
		LEFT JOIN edu_student_session_attendance a 
			ON r.session_id = a.session_id AND r.student_id = a.student_id AND a.deleted_at IS NULL
		WHERE r.session_id = ? AND r.deleted_at IS NULL
		ORDER BY r.student_id ASC
	`
	rows, err := pool.QueryContext(ctx, query, sessionID)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_homework_submissions", fmt.Sprintf("session=%d", sessionID), time.Since(start), err)
		return res, nil
	}
	defer rows.Close()

	submissions, err := rowsToMaps(rows)
	if err != nil {
		res := errorResult(err)
		logger.LogToolCall("get_homework_submissions", fmt.Sprintf("session=%d", sessionID), time.Since(start), err)
		return res, nil
	}

	// Enrich student info
	var studentIDs []int
	completedCount := 0
	gradedCount := 0
	for _, sub := range submissions {
		if sIDVal, ok := sub["student_id"]; ok {
			var sID int
			if v, ok := sIDVal.(int64); ok {
				sID = int(v)
			} else if v, ok := sIDVal.(int); ok {
				sID = v
			}
			studentIDs = append(studentIDs, sID)
		}
		if comp, ok := sub["btvn_complete"].(string); ok && (comp == "1" || comp == "true") {
			completedCount++
		}
		if sc, ok := sub["btvn_score"].(string); ok && strings.TrimSpace(sc) != "" {
			gradedCount++
		}
	}

	profiles := fetchStudentProfiles(ctx, studentIDs)
	for _, sub := range submissions {
		if sIDVal, ok := sub["student_id"]; ok {
			var sID int
			if v, ok := sIDVal.(int64); ok {
				sID = int(v)
			} else if v, ok := sIDVal.(int); ok {
				sID = v
			}
			if prof, ok := profiles[sID]; ok {
				sub["studentName"] = prof["studentName"]
				sub["school"] = prof["school"]
			}
		}
	}

	logger.LogToolCall("get_homework_submissions", fmt.Sprintf("session=%d count=%d", sessionID, len(submissions)), time.Since(start), nil)
	return mcp.NewToolResultStructuredOnly(map[string]any{
		"sessionId": sessionID,
		"summary": map[string]any{
			"totalSubmissions": len(submissions),
			"completedCount":   completedCount,
			"gradedCount":      gradedCount,
		},
		"submissions": submissions,
	}), nil
}
