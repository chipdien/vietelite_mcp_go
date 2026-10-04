package db

import "testing"

func TestValidateSQL_ValidSelect(t *testing.T) {
	if err := ValidateSQL("SELECT * FROM users"); err != nil {
		t.Errorf("Expected valid SQL, got error: %v", err)
	}
}

func TestValidateSQL_InvalidDrop(t *testing.T) {
	if err := ValidateSQL("DROP TABLE users"); err == nil {
		t.Error("Expected error for DROP TABLE")
	}
}

func TestValidateSQL_InvalidMultiStatement(t *testing.T) {
	if err := ValidateSQL("SELECT 1; DELETE FROM users"); err == nil {
		t.Error("Expected error for multi-statement")
	}
}

func TestValidateSQL_LegitimateTimestampColumns(t *testing.T) {
	// Keyword blacklist must not trip on column/table names that merely
	// contain a prohibited keyword as a substring (updated_at, create_time, ...).
	cases := []string{
		"SELECT * FROM students WHERE updated_at > '2024-01-01'",
		"SELECT id, created_at FROM users ORDER BY created_at DESC",
		"SELECT * FROM altered_records",
		"SELECT COUNT(*) FROM students WHERE last_update IS NOT NULL",
	}
	for _, sql := range cases {
		if err := ValidateSQL(sql); err != nil {
			t.Errorf("Expected valid SQL %q, got error: %v", sql, err)
		}
	}
}

func TestEnforceLimit_InjectLimit(t *testing.T) {
	out := EnforceLimit("SELECT * FROM users", 50)
	if out != "SELECT * FROM users LIMIT 50" {
		t.Errorf("Limit injection failed: %s", out)
	}
}

func TestEnforceLimit_CapLimit(t *testing.T) {
	out := EnforceLimit("SELECT * FROM users LIMIT 500", 200)
	if out != "SELECT * FROM users LIMIT 200" {
		t.Errorf("Limit capping failed: %s", out)
	}
}

func TestEnforceLimit_KeepsSmallerLimit(t *testing.T) {
	out := EnforceLimit("SELECT * FROM users LIMIT 10", 200)
	if out != "SELECT * FROM users LIMIT 10" {
		t.Errorf("Should keep smaller limit, got: %s", out)
	}
}
