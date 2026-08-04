// Package jsonx provides robust JSON extraction and sanitization for LLM
// responses. Smaller/weaker models (especially via Ollama) often produce
// nearly-valid JSON with common defects: real newlines inside string values,
// unmatched brackets from embedded code, invalid escape sequences, and extra
// text before/after the JSON object.
//
// The functions here handle the "strip fences → extract object → sanitize
// strings → parse" pipeline that is duplicated across the codebase.
package jsonx

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// UnmarshalObject strips markdown fences, extracts the outermost JSON object
// with proper brace matching, sanitizes common LLM JSON defects, and unmarshals
// into v.
func UnmarshalObject(raw string, v any) error {
	cleaned := CleanObject(raw)
	return json.Unmarshal([]byte(cleaned), v)
}

// UnmarshalArray is like UnmarshalObject but for JSON arrays.
func UnmarshalArray(raw string, v any) error {
	cleaned := CleanArray(raw)
	return json.Unmarshal([]byte(cleaned), v)
}

// CleanObject returns the sanitized JSON object string (ready for
// json.Unmarshal). It strips fences, extracts the outermost {…}, and fixes
// real control characters inside string values.
func CleanObject(raw string) string {
	s := strings.TrimSpace(raw)
	s = stripFences(s)
	s = extractJSON(s, '{', '}')
	s = sanitizeJSON(s)
	return s
}

// CleanArray is like CleanObject but for JSON arrays.
func CleanArray(raw string) string {
	s := strings.TrimSpace(raw)
	s = stripFences(s)
	s = extractJSON(s, '[', ']')
	s = sanitizeJSON(s)
	return s
}

// stripFences removes markdown code fences from s. It handles:
//   - ```json … ```
//   - ``` … ```
//   - leading/trailing text that is not part of the fence
func stripFences(s string) string {
	// Case 1: ```json ... ```
	if idx := strings.Index(s, "```json"); idx >= 0 {
		start := idx + len("```json")
		// Skip a trailing newline after the opening fence
		if len(s) > start && s[start] == '\n' {
			start++
		}
		if end := strings.Index(s[start:], "```"); end >= 0 {
			s = s[start : start+end]
		}
	} else if idx := strings.Index(s, "```"); idx >= 0 {
		start := idx + len("```")
		if len(s) > start && s[start] == '\n' {
			start++
		}
		if end := strings.Index(s[start:], "```"); end >= 0 {
			s = s[start : start+end]
		}
	}
	return strings.TrimSpace(s)
}

// extractJSON finds the first open bracket and returns the substring up to its
// matching close bracket, using a depth counter that is string-aware (it skips
// brackets inside quoted strings). Falls back to first-open / last-close if no
// matching bracket is found.
func extractJSON(s string, openBrace, closeBrace byte) string {
	start := strings.IndexByte(s, openBrace)
	if start < 0 {
		return s
	}

	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(s); i++ {
		c := s[i]

		if escaped {
			escaped = false
			continue
		}

		if inString {
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch c {
		case '"':
			inString = true
		case openBrace:
			depth++
		case closeBrace:
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}

	// No matching close: fall back to the old LastIndex heuristic.
	if j := strings.LastIndexByte(s, closeBrace); j > start {
		return s[start : j+1]
	}
	return s
}

// sanitizeJSON replaces real control characters inside JSON string values with
// their escape sequences, so that the result is valid JSON. It uses a simple
// state machine that tracks whether we are inside a double-quoted string.
//
// This fixes the most common defect from smaller LLMs (especially Ollama): they
// output literal newlines, tabs, and other control chars inside string values
// instead of the JSON escape sequences \n, \t, etc.
func sanitizeJSON(s string) string {
	// Fast path: no control characters that need fixing (just validate).
	needsFix := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			needsFix = true
			break
		}
		if (c == '\t' || c == '\n' || c == '\r') && !needsFix {
			// We only know we need to fix tab/newline after confirming we're inside
			// a string. Do a quick pre-check.
			needsFix = true
			break
		}
	}
	if !needsFix {
		return s
	}

	var b strings.Builder
	b.Grow(len(s) + 64) // a little headroom for escape sequences

	inString := false
	escaped := false

	for i := 0; i < len(s); {
		c := s[i]
		// Decode the full rune so we don't split multi-byte UTF-8.
		r, size := utf8.DecodeRuneInString(s[i:])

		if escaped {
			b.WriteByte(c)
			escaped = false
			i += size
			continue
		}

		if inString {
			switch c {
			case '\\':
				b.WriteByte(c)
				escaped = true
			case '"':
				b.WriteByte(c)
				inString = false
			case '\n':
				b.WriteString("\\n")
			case '\r':
				b.WriteString("\\r")
			case '\t':
				b.WriteString("\\t")
			default:
				if c < 0x20 {
					// Other control character: emit as \uXXXX.
					fmt.Fprintf(&b, "\\u%04x", c)
				} else {
					b.WriteRune(r)
				}
			}
		} else {
			b.WriteByte(c)
			if c == '"' {
				inString = true
			}
		}
		i += size
	}
	return b.String()
}
