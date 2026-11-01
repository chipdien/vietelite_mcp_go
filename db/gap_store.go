package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	gapDB   *sql.DB
	gapOnce sync.Once
	gapErr  error
)

// InitGapStore initializes the SQLite database for capability gap tracking.
// Creates data/gaps.db if it doesn't exist and sets up the schema.
func InitGapStore() error {
	gapOnce.Do(func() {
		dataDir := "data"
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			gapErr = fmt.Errorf("failed to create data directory: %w", err)
			return
		}

		dbPath := filepath.Join(dataDir, "gaps.db")
		gapDB, gapErr = sql.Open("sqlite", dbPath)
		if gapErr != nil {
			gapErr = fmt.Errorf("failed to open gaps.db: %w", gapErr)
			return
		}

		schema := `
CREATE TABLE IF NOT EXISTS capability_gaps (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  source TEXT NOT NULL,
  database TEXT,
  question TEXT,
  query_text TEXT,
  suggested_tool_name TEXT,
  notes TEXT,
  occurrence_count INTEGER DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_gaps_source ON capability_gaps(source);
CREATE INDEX IF NOT EXISTS idx_gaps_created_at ON capability_gaps(created_at);
CREATE INDEX IF NOT EXISTS idx_gaps_database ON capability_gaps(database);
`
		if _, err := gapDB.Exec(schema); err != nil {
			gapErr = fmt.Errorf("failed to create schema: %w", err)
			return
		}
	})
	return gapErr
}

// RecordGap logs a capability gap (either auto from execute_read_query or explicit from user/agent).
func RecordGap(source, database, question, queryText, suggestedTool, notes string) error {
	if err := InitGapStore(); err != nil {
		return err
	}

	// Check for duplicate recent query (same query_text in last 5 minutes) and increment count
	if queryText != "" {
		var existingID int
		var count int
		err := gapDB.QueryRow(`
			SELECT id, occurrence_count FROM capability_gaps
			WHERE query_text = ? AND created_at > datetime('now', '-5 minutes')
			ORDER BY created_at DESC LIMIT 1
		`, queryText).Scan(&existingID, &count)

		if err == nil {
			// Found duplicate — increment count instead of inserting
			_, err = gapDB.Exec(`UPDATE capability_gaps SET occurrence_count = ?, created_at = CURRENT_TIMESTAMP WHERE id = ?`, count+1, existingID)
			return err
		}
	}

	// No recent duplicate — insert new
	_, err := gapDB.Exec(`
		INSERT INTO capability_gaps (source, database, question, query_text, suggested_tool_name, notes)
		VALUES (?, ?, ?, ?, ?, ?)
	`, source, database, question, queryText, suggestedTool, notes)
	return err
}

// QueryGaps retrieves capability gaps ordered by occurrence_count DESC, created_at DESC.
func QueryGaps(minOccurrences int, limit int) ([]map[string]any, error) {
	if err := InitGapStore(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	rows, err := gapDB.Query(`
		SELECT id, created_at, source, database, question, query_text, suggested_tool_name, notes, occurrence_count
		FROM capability_gaps
		WHERE occurrence_count >= ?
		ORDER BY occurrence_count DESC, created_at DESC
		LIMIT ?
	`, minOccurrences, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id int
		var createdAt time.Time
		var source, database, question, queryText, suggestedTool, notes sql.NullString
		var occurrenceCount int

		if err := rows.Scan(&id, &createdAt, &source, &database, &question, &queryText, &suggestedTool, &notes, &occurrenceCount); err != nil {
			return nil, err
		}

		results = append(results, map[string]any{
			"id":                  id,
			"created_at":          createdAt.Format(time.RFC3339),
			"source":              nullStringToString(source),
			"database":            nullStringToString(database),
			"question":            nullStringToString(question),
			"query_text":          nullStringToString(queryText),
			"suggested_tool_name": nullStringToString(suggestedTool),
			"notes":               nullStringToString(notes),
			"occurrence_count":    occurrenceCount,
		})
	}
	return results, rows.Err()
}

func nullStringToString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}
