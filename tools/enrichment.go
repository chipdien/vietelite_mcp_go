package tools

import (
	"context"
	"fmt"
	"strings"

	"vietelite-mcp/go-server/db"
	"vietelite-mcp/go-server/logger"
)

// fetchStudentProfiles takes a slice of student IDs and fetches their basic profile from identity DB.
// Returns a map[int]map[string]any keyed by student_id. Gracefully returns empty on error.
func fetchStudentProfiles(ctx context.Context, studentIDs []int) map[int]map[string]any {
	result := make(map[int]map[string]any)
	if len(studentIDs) == 0 {
		return result
	}

	// Deduplicate IDs
	uniqueIDs := make(map[int]bool)
	var idList []int
	for _, id := range studentIDs {
		if id > 0 && !uniqueIDs[id] {
			uniqueIDs[id] = true
			idList = append(idList, id)
		}
	}
	if len(idList) == 0 {
		return result
	}

	pool, err := db.GetPool("identity")
	if err != nil {
		logger.Warn("ENRICHMENT", "identity pool unavailable: %v", err)
		return result
	}

	placeholders := make([]string, len(idList))
	args := make([]any, len(idList))
	for i, id := range idList {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT 
			s.id, s.name, s.school, s.grade,
			COALESCE(a.phone, '') AS student_phone,
			COALESCE(p.alt_fullname, '') AS parent_name,
			COALESCE(p.alt_phone, '') AS parent_phone
		FROM ve26_student s
		LEFT JOIN ve26_account a ON s.account_id = a.id
		LEFT JOIN ve26_parent p ON s.parent_id = p.id
		WHERE s.id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := pool.QueryContext(ctx, query, args...)
	if err != nil {
		logger.Warn("ENRICHMENT", "failed to query student profiles: %v", err)
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id                                    int
			name, school, grade                   string
			studentPhone, parentName, parentPhone string
		)
		if err := rows.Scan(&id, &name, &school, &grade, &studentPhone, &parentName, &parentPhone); err == nil {
			result[id] = map[string]any{
				"studentId":    id,
				"studentName":  name,
				"school":       school,
				"grade":        grade,
				"studentPhone": studentPhone,
				"parentName":   parentName,
				"parentPhone":  parentPhone,
			}
		}
	}
	return result
}
