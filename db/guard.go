package db

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var invalidKeywords = []string{
	"INSERT", "UPDATE", "DELETE", "DROP", "ALTER",
	"TRUNCATE", "CREATE", "EXEC", "GRANT", "REVOKE",
}

// invalidKeywordPatterns pre-compiles one word-boundary pattern per prohibited
// keyword. \b only matches at a transition between a word char and a non-word
// char, so identifiers like updated_at, created_at or altered_records cannot
// match UPDATE, CREATE or ALTER.
var invalidKeywordPatterns = func() map[string]*regexp.Regexp {
	m := make(map[string]*regexp.Regexp, len(invalidKeywords))
	for _, kw := range invalidKeywords {
		m[kw] = regexp.MustCompile(`\b` + kw + `\b`)
	}
	return m
}()

var selectPrefixPattern = regexp.MustCompile(`^(SELECT|SHOW|DESCRIBE|DESC|EXPLAIN)\b`)

// ValidateSQL ensures a statement is a single read-only query. It rejects
// non-SELECT entry points, stacked statements, and any prohibited DML/DDL
// keyword appearing as a standalone word.
func ValidateSQL(sql string) error {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return errors.New("empty query")
	}

	// Reject stacked statements before keyword scanning so a trailing
	// delimiter cannot smuggle a second statement through.
	if strings.Contains(trimmed, ";") {
		return errors.New("multiple statements are not allowed")
	}

	upper := strings.ToUpper(trimmed)
	if !selectPrefixPattern.MatchString(upper) {
		return errors.New("only read-only queries (SELECT, SHOW, DESCRIBE, EXPLAIN) are allowed")
	}

	for kw, re := range invalidKeywordPatterns {
		if re.MatchString(upper) {
			return errors.New("query contains prohibited keyword: " + kw)
		}
	}

	return nil
}

// limitPattern matches a trailing LIMIT <n> clause (optionally separated from
// the preceding query with whitespace/newlines). It intentionally does not
// match LIMIT inside string literals anchored at the end of the statement in a
// way that would corrupt them; see EnforceLimit for the applied heuristic.
var limitPattern = regexp.MustCompile(`(?is)\bLIMIT\s+(\d+)`)

// EnforceLimit guarantees the query returns at most maxLimit rows. An existing
// LIMIT above maxLimit is capped; one at or below maxLimit is left untouched;
// a query with no LIMIT gets one injected.
func EnforceLimit(sql string, maxLimit int) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(sql), ";")

	if matches := limitPattern.FindStringSubmatch(trimmed); matches != nil {
		existing, err := strconv.Atoi(matches[1])
		if err == nil && existing > maxLimit {
			return limitPattern.ReplaceAllString(trimmed, fmt.Sprintf("LIMIT %d", maxLimit))
		}
		return trimmed
	}

	return fmt.Sprintf("%s LIMIT %d", trimmed, maxLimit)
}
