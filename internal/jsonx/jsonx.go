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

// stripFences removes an *outer* markdown code fence wrapping the payload.
// It handles:
//   - ```json … ```
//   - ``` … ```
//   - optional preamble text before the opening fence
//
// Important: fences that appear *inside* an already-started JSON value
// (e.g. a Go code sample in final_answer: "…\n```go\nfunc f(){}\n```")
// must NOT be stripped. Doing so was the root cause of intermittent
// parse_error on otherwise-valid decision JSON.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	// Already looks like a JSON value: keep intact. Inner ``` fences are
	// data, not wrappers.
	if s[0] == '{' || s[0] == '[' {
		return s
	}

	// Only treat a fence as outer if it appears before any JSON start char.
	// Otherwise the fence is embedded in preamble/prose or string content.
	jsonStart := indexJSONStart(s)

	// Prefer ```json over bare ```.
	if idx := strings.Index(s, "```json"); idx >= 0 {
		if jsonStart >= 0 && jsonStart < idx {
			return s
		}
		start := idx + len("```json")
		// Skip optional language-tag whitespace and the first newline.
		start = skipFenceHeader(s, start)
		if end := strings.Index(s[start:], "```"); end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
		return strings.TrimSpace(s[start:])
	}

	if idx := strings.Index(s, "```"); idx >= 0 {
		if jsonStart >= 0 && jsonStart < idx {
			return s
		}
		start := idx + len("```")
		start = skipFenceHeader(s, start)
		if end := strings.Index(s[start:], "```"); end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
		return strings.TrimSpace(s[start:])
	}
	return s
}

// indexJSONStart returns the index of the first '{' or '[', or -1.
func indexJSONStart(s string) int {
	iObj := strings.IndexByte(s, '{')
	iArr := strings.IndexByte(s, '[')
	switch {
	case iObj < 0:
		return iArr
	case iArr < 0:
		return iObj
	case iObj < iArr:
		return iObj
	default:
		return iArr
	}
}

// skipFenceHeader advances past an optional language tag and the newline that
// ends the opening fence line (```json\n or ```go\n or ```\n).
func skipFenceHeader(s string, start int) int {
	// Consume the rest of the fence line (language tag, spaces) up to newline.
	for start < len(s) {
		c := s[start]
		if c == '\n' {
			return start + 1
		}
		if c == '\r' {
			if start+1 < len(s) && s[start+1] == '\n' {
				return start + 2
			}
			return start + 1
		}
		// Only skip typical fence-header chars; stop if we hit JSON early.
		if c == '{' || c == '[' {
			return start
		}
		start++
	}
	return start
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
