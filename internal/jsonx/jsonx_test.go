package jsonx

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStripFences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain json",
			input: `{"key": "value"}`,
			want:  `{"key": "value"}`,
		},
		{
			name:  "json code fence",
			input: "```json\n{\"key\": \"value\"}\n```",
			want:  `{"key": "value"}`,
		},
		{
			name:  "plain code fence",
			input: "```\n{\"key\": \"value\"}\n```",
			want:  `{"key": "value"}`,
		},
		{
			name:  "trailing text after fence",
			input: "```json\n{\"key\": \"value\"}\n```\nsome trailing text",
			want:  `{"key": "value"}`,
		},
		{
			name:  "no fence - already clean",
			input: `  {"key": "value"}  `,
			want:  `{"key": "value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripFences(tt.input)
			if got != tt.want {
				t.Errorf("stripFences() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple object",
			input: `{"a":1, "b":2}`,
			want:  `{"a":1, "b":2}`,
		},
		{
			name:  "nested braces",
			input: `{"a":{"b":2}}`,
			want:  `{"a":{"b":2}}`,
		},
		{
			name:  "braces inside string",
			input: `{"code":"func foo(){return}"}`,
			want:  `{"code":"func foo(){return}"}`,
		},
		{
			name:  "braces inside string with escapes",
			input: `{"text":"escaped \\\" quote { still in string } end"}`,
			want:  `{"text":"escaped \\\" quote { still in string } end"}`,
		},
		{
			name:  "extra text before object",
			input: `some preamble {"a":1} trailing`,
			want:  `{"a":1}`,
		},
		{
			name:  "realistic decision with final_answer containing braces",
			input: `{"thought":"done","final_answer":"func main() { fmt.Println(\"hello\") } output is hello","references":[]}`,
			want:  `{"thought":"done","final_answer":"func main() { fmt.Println(\"hello\") } output is hello","references":[]}`,
		},
		{
			name:  "unmatched - no close",
			input: `{"a":1`,
			want:  `{"a":1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input, '{', '}')
			if got != tt.want {
				t.Errorf("extractJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "clean json passes through",
			input: `{"key":"value with \\n escaped newline"}`,
			want:  `{"key":"value with \\n escaped newline"}`,
		},
		{
			name: "real newline inside string",
			// Raw newline (0x0A) inside a string value — common Ollama defect.
			input: "{\"thought\":\"done\",\"final_answer\":\"line1\nline2\nline3\"}",
			want:  `{"thought":"done","final_answer":"line1\nline2\nline3"}`,
		},
		{
			name: "real tab inside string",
			input: "{\"text\":\"col1\tcol2\"}",
			want:  `{"text":"col1\tcol2"}`,
		},
		{
			name: "real carriage return inside string",
			input: "{\"text\":\"line1\r\nline2\"}",
			want:  `{"text":"line1\r\nline2"}`,
		},
		{
			name:  "newline outside string is untouched",
			input: "{\"a\":1}\n", // newline AFTER closing brace
			want:  "{\"a\":1}\n", // should be preserved (it's outside a string)
		},
		{
			name: "mixed escapes and real newlines",
			// Already-escaped \n stay; real newlines get escaped
			input: "{\"text\":\"already\\nhere\nreal newline\"}",
			want:  `{"text":"already\nhere\nreal newline"}`,
		},
		{
			name:  "strings with unicode",
			input: `{"text":"你好世界"}`,
			want:  `{"text":"你好世界"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeJSON(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanObjectRoundtrip(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantVal map[string]string // expected key-values after unmarshal
	}{
		{
			name:  "clean json",
			input: `{"key":"value"}`,
			wantVal: map[string]string{"key": "value"},
		},
		{
			name:  "fenced with real newlines in final_answer",
			input: "```json\n{\"final_answer\":\"line1\nline2\",\"references\":[]}\n```",
			wantVal: map[string]string{"final_answer": "line1\nline2"},
		},
		{
			name:  "llm response with preamble and real newlines",
			input: "Here is the JSON:\n\n{\"thought\":\"done\",\"final_answer\":\"answer with\nembedded newlines\"}",
			wantVal: map[string]string{"thought": "done", "final_answer": "answer with\nembedded newlines"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleaned := CleanObject(tt.input)
			var result map[string]any
			if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
				t.Fatalf("CleanObject produced invalid JSON: %v\nCleaned:\n%s", err, cleaned)
			}
			for k, wantV := range tt.wantVal {
				got, ok := result[k]
				if !ok {
					t.Errorf("missing key %q in %v", k, result)
					continue
				}
				if got != wantV {
					t.Errorf("key %q = %q, want %q", k, got, wantV)
				}
			}
		})
	}
}

// TestRealWorldOllamaFailure reproduces the exact failure pattern the user
// reported: a final_answer containing markdown with backticks and \n escapes.
func TestRealWorldOllamaFailure(t *testing.T) {
	// This is a realistic Ollama output — the final_answer contains \n
	// (literal backslash + n, which IS valid JSON) and markdown backticks.
	realisticOllamaJSON := `{"thought":"已收集到直接相关的博客文章证据，可以给出最终答案。","final_answer":"在 Go 1.26 中，` + "`fmt.Errorf`" + ` 的核心优化是：\n\n## 1. 旧版本的开销\n即使不使用任何占位符，` + "`fmt.Errorf`" + ` 也会走一整套沉重流程：\n- ` + "`newPrinter()`" + ` 获取打印对象\n- ` + "`doPrintf`" + ` 扫描字符串\n","references":[{"title":"Go 1.26 Release Notes","path":"raw/sources/go126.md","layer":"raw"}]}`

	cleaned := CleanObject(realisticOllamaJSON)
	var result map[string]any
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		t.Fatalf("Failed to parse realistic Ollama JSON: %v\nCleaned:\n%s", err, cleaned)
	}

	finalAnswer, ok := result["final_answer"].(string)
	if !ok {
		t.Fatalf("final_answer not a string: %T", result["final_answer"])
	}
	if !strings.Contains(finalAnswer, "Go 1.26") {
		t.Errorf("final_answer lost content: %s", finalAnswer)
	}
	if !strings.Contains(finalAnswer, "fmt.Errorf") {
		t.Errorf("final_answer lost backtick content: %s", finalAnswer)
	}

	refs, ok := result["references"].([]any)
	if !ok || len(refs) != 1 {
		t.Errorf("references broken: %v", result["references"])
	}
}

// TestDecisionStructRoundtrip tests the exact decision struct used by the agent.
func TestDecisionStructRoundtrip(t *testing.T) {
	input := `{"thought":"search for info","action":"wiki_search","args":{"query":"GC","top_n":5}}`

	cleaned := CleanObject(input)
	type decision struct {
		Thought string         `json:"thought"`
		Action  string         `json:"action"`
		Args    map[string]any `json:"args"`
	}
	var d decision
	if err := json.Unmarshal([]byte(cleaned), &d); err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if d.Action != "wiki_search" || d.Args["query"] != "GC" {
		t.Errorf("unexpected: %+v", d)
	}
}
