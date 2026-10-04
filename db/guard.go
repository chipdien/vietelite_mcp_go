package db

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// forbiddenKeywords is the full set of prohibited write/DML/DDL keywords plus
// the read-shaped exfiltration vectors. It is compiled once into a single
// package-level pattern so a match always reports the same (leftmost) keyword,
// keeping error messages deterministic — map iteration would not.
var forbiddenKeywords = []string{
	"INSERT", "UPDATE", "DELETE", "DROP", "ALTER",
	"TRUNCATE", "REPLACE", "CREATE", "RENAME", "GRANT",
	"REVOKE", "SET", "CALL", "EXEC", "EXECUTE",
	"LOCK", "UNLOCK",
	"INTO\\s+OUTFILE", "INTO\\s+DUMPFILE", "LOAD_FILE", "FOR\\s+UPDATE",
}

// forbiddenPattern matches any prohibited keyword as a whole word (or, for the
// multi-word exfiltration vectors, as the full sequence). \b only matches at a
// transition between a word char and a non-word char, so identifiers like
// updated_at, created_at or altered_records cannot match UPDATE, CREATE or
// ALTER.
var forbiddenPattern = regexp.MustCompile(`(?i)\b(` + strings.Join(forbiddenKeywords, "|") + `)\b`)

// selectPrefixPattern matches the read-only statement kinds the guard admits.
var selectPrefixPattern = regexp.MustCompile(`^(SELECT|SHOW|DESCRIBE|DESC|EXPLAIN)\b`)

// statementPrefixPattern extracts the leading keyword of a statement.
var statementPrefixPattern = regexp.MustCompile(`^[a-zA-Z]+`)

var whitespaceRunPattern = regexp.MustCompile(`\s+`)

// stripCommentsAndStrings removes SQL comments and blanks out the contents of
// string/identifier literals so their raw text cannot influence structural
// checks (semicolon splitting, keyword scanning). Comments collapse to a single
// space (or newline for line comments) to keep tokens separated; literal bodies
// collapse to `”` / `""` of equal length-ish form so downstream scanning sees
// no keyword or delimiter hidden inside them.
//
// It returns:
//   - cleaned: comments removed, literal bodies emptied (delimiters kept)
//   - stripped: literal bodies preserved verbatim, comments removed
//
// cleaned is used for structural validation (ValidateSQL); stripped is used by
// EnforceLimit so it can locate the trailing LIMIT while still skipping over
// literals and comments.
func stripCommentsAndStrings(sql string) (cleaned string, stripped string) {
	var cleanedBuilder, strippedBuilder strings.Builder
	cleanedBuilder.Grow(len(sql))
	strippedBuilder.Grow(len(sql))

	i := 0
	length := len(sql)
	inSingleQuote := false
	inDoubleQuote := false
	inBacktick := false
	inLineComment := false
	inBlockComment := false

	for i < length {
		ch := sql[i]
		var next byte
		if i+1 < length {
			next = sql[i+1]
		}

		if inLineComment {
			if ch == '\n' || ch == '\r' {
				inLineComment = false
				cleanedBuilder.WriteByte('\n')
				strippedBuilder.WriteByte('\n')
			}
			i++
			continue
		}

		if inBlockComment {
			if ch == '*' && next == '/' {
				inBlockComment = false
				i += 2
				cleanedBuilder.WriteByte(' ')
				strippedBuilder.WriteByte(' ')
				continue
			}
			i++
			continue
		}

		if inSingleQuote {
			if ch == '\\' {
				// MySQL backslash escape inside a literal: consume both bytes.
				i += 2
				continue
			}
			if ch == '\'' {
				if i+1 < length && sql[i+1] == '\'' {
					// Escaped '' pair; drop the body, keep the delimiters.
					i += 2
					continue
				}
				inSingleQuote = false
				cleanedBuilder.WriteString("''")
				strippedBuilder.WriteString("''")
			}
			i++
			continue
		}

		if inDoubleQuote {
			if ch == '\\' {
				i += 2
				continue
			}
			if ch == '"' {
				if i+1 < length && sql[i+1] == '"' {
					i += 2
					continue
				}
				inDoubleQuote = false
				cleanedBuilder.WriteString(`""`)
				strippedBuilder.WriteString(`""`)
			}
			i++
			continue
		}

		if inBacktick {
			if ch == '`' {
				inBacktick = false
			}
			// Backtick identifiers are structural (table/column names); keep them.
			cleanedBuilder.WriteByte(ch)
			strippedBuilder.WriteByte(ch)
			i++
			continue
		}

		switch {
		case ch == '-' && next == '-':
			inLineComment = true
			i += 2
		case ch == '#':
			inLineComment = true
			i++
		case ch == '/' && next == '*':
			inBlockComment = true
			i += 2
		case ch == '\'':
			inSingleQuote = true
			cleanedBuilder.WriteByte('\'')
			strippedBuilder.WriteByte('\'')
			i++
		case ch == '"':
			inDoubleQuote = true
			cleanedBuilder.WriteByte('"')
			strippedBuilder.WriteByte('"')
			i++
		case ch == '`':
			inBacktick = true
			cleanedBuilder.WriteByte('`')
			strippedBuilder.WriteByte('`')
			i++
		default:
			cleanedBuilder.WriteByte(ch)
			strippedBuilder.WriteByte(ch)
			i++
		}
	}

	return cleanedBuilder.String(), strippedBuilder.String()
}

// ValidateSQL ensures a statement is a single read-only query. It rejects
// non-SELECT entry points, stacked statements, and any prohibited DML/DDL
// keyword or exfiltration vector appearing as a standalone word.
//
// Comments and string literals are stripped before both the multi-statement
// check and the keyword scan, so `name = 'a;b'` and `SELECT 1 /* note */` are
// accepted while genuine stacked statements remain rejected.
func ValidateSQL(sql string) error {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return errors.New("empty query")
	}

	cleaned, _ := stripCommentsAndStrings(trimmed)

	// Split on ';' after stripping so delimiters inside literals/comments do
	// not fragment the statement, then reject genuine stacked statements.
	statements := make([]string, 0, 2)
	for _, part := range strings.Split(cleaned, ";") {
		if p := strings.TrimSpace(part); p != "" {
			statements = append(statements, p)
		}
	}

	if len(statements) == 0 {
		return errors.New("query contains no executable statements")
	}
	if len(statements) > 1 {
		return errors.New("multiple statements are not allowed")
	}

	single := statements[0]

	if !selectPrefixPattern.MatchString(strings.ToUpper(single)) {
		return errors.New("only read-only queries (SELECT, SHOW, DESCRIBE, EXPLAIN) are allowed")
	}

	if match := forbiddenPattern.FindString(single); match != "" {
		return errors.New("query contains prohibited keyword: " + normalizeKeyword(match))
	}

	return nil
}

// normalizeKeyword collapses internal whitespace and upper-cases a matched
// keyword so the reported message is stable regardless of source casing or
// spacing.
func normalizeKeyword(match string) string {
	return strings.ToUpper(whitespaceRunPattern.ReplaceAllString(match, " "))
}

// stripCommentsAndStringsWithOffsets is stripCommentsAndStrings plus a map from
// each byte index in the stripped output to the corresponding byte index in the
// original input. Literal bodies (and comment bodies) are removed from the
// stripped output, so downstream code that rewrites matched spans must translate
// indices back to the original string before splicing.
func stripCommentsAndStringsWithOffsets(sql string) (stripped string, offsets []int) {
	var b strings.Builder
	b.Grow(len(sql))
	offsets = make([]int, 0, len(sql))

	i := 0
	length := len(sql)
	inSingleQuote := false
	inDoubleQuote := false
	inBacktick := false
	inLineComment := false
	inBlockComment := false

	for i < length {
		ch := sql[i]
		var next byte
		if i+1 < length {
			next = sql[i+1]
		}

		if inLineComment {
			if ch == '\n' || ch == '\r' {
				inLineComment = false
				b.WriteByte('\n')
				offsets = append(offsets, i)
			}
			i++
			continue
		}

		if inBlockComment {
			if ch == '*' && next == '/' {
				inBlockComment = false
				i += 2
				b.WriteByte(' ')
				offsets = append(offsets, i-2)
				continue
			}
			i++
			continue
		}

		if inSingleQuote {
			if ch == '\\' {
				i += 2
				continue
			}
			if ch == '\'' {
				if i+1 < length && sql[i+1] == '\'' {
					i += 2
					continue
				}
				inSingleQuote = false
				b.WriteByte('\'')
				offsets = append(offsets, i)
				b.WriteByte('\'')
				offsets = append(offsets, i)
			}
			i++
			continue
		}

		if inDoubleQuote {
			if ch == '\\' {
				i += 2
				continue
			}
			if ch == '"' {
				if i+1 < length && sql[i+1] == '"' {
					i += 2
					continue
				}
				inDoubleQuote = false
				b.WriteByte('"')
				offsets = append(offsets, i)
				b.WriteByte('"')
				offsets = append(offsets, i)
			}
			i++
			continue
		}

		if inBacktick {
			if ch == '`' {
				inBacktick = false
			}
			b.WriteByte(ch)
			offsets = append(offsets, i)
			i++
			continue
		}

		switch {
		case ch == '-' && next == '-':
			inLineComment = true
			i += 2
		case ch == '#':
			inLineComment = true
			i++
		case ch == '/' && next == '*':
			inBlockComment = true
			i += 2
		case ch == '\'':
			inSingleQuote = true
			b.WriteByte('\'')
			offsets = append(offsets, i)
			i++
		case ch == '"':
			inDoubleQuote = true
			b.WriteByte('"')
			offsets = append(offsets, i)
			i++
		case ch == '`':
			inBacktick = true
			b.WriteByte('`')
			offsets = append(offsets, i)
			i++
		default:
			b.WriteByte(ch)
			offsets = append(offsets, i)
			i++
		}
	}

	return b.String(), offsets
}

// mapStrippedToOriginal translates a byte index in the stripped output to the
// corresponding byte index in the original input using the offsets map produced
// by stripCommentsAndStringsWithOffsets.
//
// halfOpenEnd may be true when translating an exclusive end index (the byte one
// past the matched span). In that case an index equal to len(offsets) maps to
// the end of the original string.
func mapStrippedToOriginal(offsets []int, strippedIndex int, origLen int, halfOpenEnd bool) int {
	if strippedIndex < 0 {
		return -1
	}
	if strippedIndex == len(offsets) {
		if halfOpenEnd {
			return origLen
		}
		return -1
	}
	if strippedIndex > len(offsets) {
		return -1
	}
	idx := offsets[strippedIndex]
	if idx < 0 || idx > origLen {
		return -1
	}
	return idx
}

// trailingLimitPattern matches a top-level trailing LIMIT <n> clause that is
// optionally followed by an OFFSET clause, anchored at end of statement.
var trailingLimitPattern = regexp.MustCompile(`(?is)\s+LIMIT\s+(\d+)(\s*(?:,|OFFSET)\s*\d+)?\s*$`)

// EnforceLimit guarantees the query returns at most maxLimit rows.
//
// Only the trailing top-level LIMIT of a SELECT is rewritten or injected:
//   - an existing LIMIT above maxLimit is capped, at or below maxLimit is kept;
//   - a query with no LIMIT gets `LIMIT maxLimit` appended;
//   - LIMITs inside string literals, comments, or subqueries are never touched;
//   - non-SELECT statements (SHOW/DESCRIBE/EXPLAIN) are returned unchanged,
//     since appending a LIMIT to them can produce invalid SQL.
func EnforceLimit(sql string, maxLimit int) string {
	trimmed := strings.TrimSpace(sql)
	// A single trailing semicolon is a statement delimiter, not rewriting scope.
	trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))

	if trimmed == "" {
		return trimmed
	}

	stripped, offsets := stripCommentsAndStringsWithOffsets(trimmed)

	// Only SELECT-shaped statements may receive a LIMIT.
	if !strings.HasPrefix(strings.ToUpper(stripped), "SELECT") {
		return trimmed
	}

	// Anchor: a LIMIT clause is only real if only whitespace/comments — i.e. an
	// empty stripped remainder — follows it. This keeps `'LIMIT 500'` (literal
	// body removed) and `/* LIMIT 500 */` (removed) from matching.
	if loc := trailingLimitPattern.FindStringSubmatchIndex(stripped); loc != nil {
		start, end := loc[0], loc[1]
		if strings.TrimSpace(stripped[end:]) != "" {
			return trimmed
		}
		if strings.TrimSpace(stripped[:start]) == "" {
			// The whole statement was the LIMIT clause; nothing to rewrite.
			return trimmed
		}

		group := stripped[loc[2]:loc[3]]
		value, err := strconv.Atoi(group)
		// An unparseable or overflowing LIMIT is treated as "too large".
		if err == nil && value <= maxLimit {
			return trimmed
		}

		// Map the digit span from the stripped string back to the original
		// string. Literal bodies are removed by stripping, so raw indices are
		// unreliable whenever a literal precedes the LIMIT.
		digitStart := mapStrippedToOriginal(offsets, loc[2], len(trimmed), false)
		digitEnd := mapStrippedToOriginal(offsets, loc[3], len(trimmed), true)
		if digitStart < 0 || digitEnd < digitStart {
			// Defensive: no digit span to rewrite; append a safe cap instead.
			return trimmed + " LIMIT " + strconv.Itoa(maxLimit)
		}
		return trimmed[:digitStart] + strconv.Itoa(maxLimit) + trimmed[digitEnd:]
	}

	return trimmed + " LIMIT " + strconv.Itoa(maxLimit)
}
