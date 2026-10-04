package db

import (
	"strings"
	"testing"
)

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

// ---------------------------------------------------------------------------
// Issue 1: comment/string blindness
// ---------------------------------------------------------------------------

func TestValidateSQL_SemicolonInsideStringLiteralAllowed(t *testing.T) {
	// A semicolon inside a string literal is not stacked-statement execution.
	cases := []string{
		`SELECT * FROM t WHERE name = 'a;b'`,
		`SELECT * FROM t WHERE name = "a;b"`,
		`SELECT * FROM t WHERE note = 'DROP TABLE x;--'`,
	}
	for _, sql := range cases {
		if err := ValidateSQL(sql); err != nil {
			t.Errorf("Expected valid SQL %q, got error: %v", sql, err)
		}
	}
}

func TestValidateSQL_KeywordInsideStringLiteralAllowed(t *testing.T) {
	cases := []string{
		`SELECT * FROM t WHERE status = 'DELETED'`,
		`SELECT * FROM t WHERE action = 'UPDATE'`,
		`SELECT 'INSERT INTO x' AS label FROM t`,
	}
	for _, sql := range cases {
		if err := ValidateSQL(sql); err != nil {
			t.Errorf("Expected valid SQL %q, got error: %v", sql, err)
		}
	}
}

func TestValidateSQL_KeywordInsideCommentAllowed(t *testing.T) {
	cases := []string{
		"SELECT 1 /* note */",
		"SELECT * FROM t -- DROP TABLE users\nWHERE id = 1",
		"SELECT 1 # DELETE FROM users",
		"SELECT /* UPDATE x */ * FROM t WHERE id = 1",
	}
	for _, sql := range cases {
		if err := ValidateSQL(sql); err != nil {
			t.Errorf("Expected valid SQL %q, got error: %v", sql, err)
		}
	}
}

func TestValidateSQL_CommentHidesStackedStatementStillRejected(t *testing.T) {
	// Stripping must not open a hole: real stacked statements still rejected.
	cases := []string{
		"SELECT 1; DROP TABLE users",
		"SELECT 1 /* c */; DELETE FROM users",
		"SELECT 1; DROP TABLE users /* trailing */",
	}
	for _, sql := range cases {
		if err := ValidateSQL(sql); err == nil {
			t.Errorf("Expected rejection for %q", sql)
		}
	}
}

// ---------------------------------------------------------------------------
// Issue 2: LIMIT rewriting
// ---------------------------------------------------------------------------

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

func TestEnforceLimit_DoesNotTouchStringLiteral(t *testing.T) {
	in := `SELECT * FROM t WHERE c = 'LIMIT 500'`
	out := EnforceLimit(in, 200)
	if !strings.Contains(out, "'LIMIT 500'") {
		t.Errorf("Literal 'LIMIT 500' was mutated: %s", out)
	}
	// No trailing top-level LIMIT existed, so one must be injected after the clause.
	if !strings.HasSuffix(out, "'LIMIT 500' LIMIT 200") {
		t.Errorf("Expected literal preserved and trailing LIMIT 200 appended, got: %s", out)
	}
}

func TestEnforceLimit_CapsOnlyTrailingLimitNotLiteral(t *testing.T) {
	// A real trailing LIMIT must be capped; a LIMIT-looking literal earlier in
	// the query must not be rewritten.
	in := `SELECT * FROM t WHERE c = 'LIMIT 500' ORDER BY id LIMIT 900`
	out := EnforceLimit(in, 200)
	if !strings.Contains(out, "'LIMIT 500'") {
		t.Errorf("Literal 'LIMIT 500' was mutated: %s", out)
	}
	if !strings.HasSuffix(out, "LIMIT 200") {
		t.Errorf("Trailing LIMIT 900 must be capped to 200, got: %s", out)
	}
}

func TestEnforceLimit_DoesNotTouchComment(t *testing.T) {
	in := "SELECT * FROM t /* LIMIT 500 */ WHERE id = 1"
	out := EnforceLimit(in, 200)
	if !strings.Contains(out, "/* LIMIT 500 */") {
		t.Errorf("Comment must not be modified, got: %s", out)
	}
	if !strings.HasSuffix(out, "LIMIT 200") {
		t.Errorf("Expected trailing LIMIT 200 appended, got: %s", out)
	}
}

func TestEnforceLimit_SubqueryLimitPreservedOuterInjected(t *testing.T) {
	in := "SELECT * FROM (SELECT * FROM a LIMIT 999) x"
	out := EnforceLimit(in, 200)
	// Inner subquery LIMIT belongs to the subquery; outer query must be capped.
	if strings.Contains(out, "LIMIT 999) x LIMIT") {
		t.Logf("inner preserved, outer appended: %s", out)
	}
	if !strings.HasSuffix(out, "LIMIT 200") {
		t.Errorf("Outer query must get LIMIT 200 appended, got: %s", out)
	}
	if !strings.Contains(out, "LIMIT 999") {
		t.Errorf("Inner subquery LIMIT 999 must be preserved, got: %s", out)
	}
}

func TestEnforceLimit_CapsTrailingTopLevelLimitWithOffset(t *testing.T) {
	out := EnforceLimit("SELECT * FROM users LIMIT 500 OFFSET 10", 200)
	if !strings.Contains(out, "LIMIT 200") {
		t.Errorf("Expected trailing LIMIT capped, got: %s", out)
	}
	if !strings.Contains(out, "OFFSET 10") {
		t.Errorf("OFFSET must be preserved, got: %s", out)
	}
}

func TestEnforceLimit_NoLimitOnNonSelectStatements(t *testing.T) {
	// Appending a LIMIT to SHOW/DESCRIBE/EXPLAIN can produce invalid SQL.
	cases := []string{
		"SHOW TABLES",
		"SHOW FULL TABLES",
		"DESCRIBE users",
		"DESC users",
		"EXPLAIN SELECT * FROM users",
	}
	for _, sql := range cases {
		out := EnforceLimit(sql, 200)
		if out != sql {
			t.Errorf("EnforceLimit must not rewrite %q, got: %q", sql, out)
		}
	}
}

func TestEnforceLimit_OverflowLimitCapped(t *testing.T) {
	// strconv.Atoi errors on values beyond int64; must still be capped.
	out := EnforceLimit("SELECT * FROM users LIMIT 99999999999999999999", 200)
	if out != "SELECT * FROM users LIMIT 200" {
		t.Errorf("Overflow LIMIT must be capped to 200, got: %s", out)
	}
}

func TestEnforceLimit_OverflowLimitCappedInMaxInt64(t *testing.T) {
	out := EnforceLimit("SELECT * FROM users LIMIT 9223372036854775807", 200)
	if out != "SELECT * FROM users LIMIT 200" {
		t.Errorf("MaxInt64 LIMIT must be capped to 200, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Issue 3 + 4: full blacklist, exfiltration vectors, determinism
// ---------------------------------------------------------------------------

func TestValidateSQL_NewBlacklistKeywords(t *testing.T) {
	cases := []string{
		"SELECT 1 REPLACE INTO x VALUES (1)",
		"SELECT * FROM a RENAME TO b",
		"SELECT 1 SET x = 1",
		"SELECT 1 CALL proc()",
		"SELECT 1 EXECUTE stmt",
		"SELECT 1 LOCK TABLES a WRITE",
		"SELECT 1 UNLOCK TABLES",
	}
	for _, sql := range cases {
		if err := ValidateSQL(sql); err == nil {
			t.Errorf("Expected rejection for %q", sql)
		}
	}
}

func TestValidateSQL_ExfiltrationVectorsRejected(t *testing.T) {
	cases := []string{
		`SELECT * FROM t INTO OUTFILE '/tmp/x'`,
		`SELECT * FROM t INTO DUMPFILE '/tmp/x'`,
		`SELECT LOAD_FILE('/etc/passwd')`,
		`SELECT * FROM t WHERE id = 1 FOR UPDATE`,
	}
	for _, sql := range cases {
		if err := ValidateSQL(sql); err == nil {
			t.Errorf("Expected rejection for %q", sql)
		}
	}
}

func TestValidateSQL_ErrorMessagesDeterministic(t *testing.T) {
	// Multiple prohibited keywords present: the reported message must be stable
	// across runs (no map-iteration nondeterminism).
	sql := "SELECT 1 DELETE FROM x UPDATE y DROP z"
	want := ValidateSQL(sql)
	if want == nil {
		t.Fatal("expected an error")
	}
	for i := 0; i < 200; i++ {
		got := ValidateSQL(sql)
		if got == nil {
			t.Fatal("expected an error")
		}
		if got.Error() != want.Error() {
			t.Fatalf("nondeterministic error message:\n first: %s\n later: %s", want, got)
		}
	}
}
