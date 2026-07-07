package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/hitzhangjie/ruminate/internal/llm"
)

// ---------------------- parse llm chat response --------------------------//
func TestParseAndAnalysisResponse(t *testing.T) {
	t.Run("ValidJSON", func(t *testing.T) {
		raw := `{
		"summary": "A test article about Go testing.",
		"entities": [
			{"name": "Go", "type": "term", "description": "A programming language"}
		],
		"concepts": [
			{"name": "Unit Testing", "description": "Testing individual units of code"}
		],
		"key_points": ["Go has built-in testing", "Tests use the testing package"],
		"tags": ["go", "testing", "programming"]
	}`

		result, err := parseAnalysisResponse(raw)
		if err != nil {
			t.Fatalf("parseAnalysisResponse failed: %v", err)
		}

		if result.Summary == "" {
			t.Error("expected non-empty summary")
		}
		if len(result.Entities) != 1 {
			t.Errorf("expected 1 entity, got %d", len(result.Entities))
		}
		if result.Entities[0].Name != "Go" {
			t.Errorf("expected entity name 'Go', got %q", result.Entities[0].Name)
		}
		if len(result.Concepts) != 1 {
			t.Errorf("expected 1 concept, got %d", len(result.Concepts))
		}
		if result.Concepts[0].Name != "Unit Testing" {
			t.Errorf("expected concept name 'Unit Testing', got %q", result.Concepts[0].Name)
		}
		if len(result.KeyPoints) != 2 {
			t.Errorf("expected 2 key points, got %d", len(result.KeyPoints))
		}
		if len(result.Tags) != 3 {
			t.Errorf("expected 3 tags, got %d", len(result.Tags))
		}
	})

	t.Run("JSONInMarkdownFence", func(t *testing.T) {
		raw := "```json\n{\"summary\": \"Test\", \"entities\": [], \"concepts\": [], \"key_points\": [\"point\"], \"tags\": [\"tag\"]}\n```"

		result, err := parseAnalysisResponse(raw)
		if err != nil {
			t.Fatalf("parseAnalysisResponse failed: %v", err)
		}
		if result.Summary != "Test" {
			t.Errorf("expected summary 'Test', got %q", result.Summary)
		}
	})

	t.Run("JSONWithPreamble", func(t *testing.T) {
		raw := "Here is the analysis:\n\n{\"summary\": \"Test\", \"entities\": [], \"concepts\": [], \"key_points\": [], \"tags\": []}"

		result, err := parseAnalysisResponse(raw)
		if err != nil {
			t.Fatalf("parseAnalysisResponse failed: %v", err)
		}
		if result.Summary != "Test" {
			t.Errorf("expected summary 'Test', got %q", result.Summary)
		}
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		raw := "This is not JSON at all."
		_, err := parseAnalysisResponse(raw)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

//------------------ build summary / entity / concept page -----------------//

func TestBuildSummaryContent(t *testing.T) {
	src := &Source{
		Title:      "Test Article",
		Origin:     "/tmp/test.md",
		SourceType: "article",
	}
	analysis := &AnalysisResult{
		Summary: "This is a test summary.",
		Entities: []EntityInfo{
			{Name: "Go", Type: "term", Description: "Programming language"},
		},
		Concepts: []ConceptInfo{
			{Name: "Testing", Description: "Code testing methodology"},
		},
		KeyPoints: []string{"Point 1", "Point 2"},
		Tags:      []string{"go", "testing"},
	}

	content := buildSummaryContent(src, analysis, "raw/article/Test Article.md")

	if !strings.Contains(content, "# Test Article") {
		t.Error("expected title in content")
	}
	if !strings.Contains(content, "This is a test summary") {
		t.Error("expected summary in content")
	}
	if !strings.Contains(content, "Point 1") {
		t.Error("expected key points in content")
	}
	wikiLinkPrefix := "[["
	if !strings.Contains(content, wikiLinkPrefix) {
		t.Error("expected wiki links in content")
	}
	if !strings.Contains(content, "go") {
		t.Error("expected tags in content")
	}
	if !strings.Contains(content, "[📄 raw]") || !strings.Contains(content, "raw/article/Test Article.md") {
		t.Error("expected raw source link in content")
	}
}

func TestBuildEntityContent(t *testing.T) {
	entity := EntityInfo{Name: "Go", Type: "term", Description: "A programming language"}
	src := &Source{Title: "test-article"}

	content := buildEntityContent(entity, src)

	if !strings.Contains(content, "# Go") {
		t.Error("expected entity name as title")
	}
	if !strings.Contains(content, "Type**: term") {
		t.Error("expected entity type")
	}
	if !strings.Contains(content, "summaries:test-article") {
		t.Error("expected reference to source summary")
	}
}

func TestBuildConceptContent(t *testing.T) {
	concept := ConceptInfo{Name: "Testing", Description: "Code testing methodology"}
	src := &Source{Title: "test-article"}

	content := buildConceptContent(concept, src)

	if !strings.Contains(content, "# Testing") {
		t.Error("expected concept name as title")
	}
	if !strings.Contains(content, "summaries:test-article") {
		t.Error("expected reference to source summary")
	}
}

//-------------- merge content into existing entity / concept -------------//

func TestMergeEntityContent(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		existing := "# Go\n\nProgramming language\n\n## References\n"
		entity := EntityInfo{Name: "Go", Type: "term", Description: "A statically typed language"}
		src := &Source{Title: "new-article"}

		merged := mergeEntityContent(existing, entity, src)

		if !strings.Contains(merged, "## From new-article") {
			t.Error("expected new section in merged content")
		}
		if !strings.Contains(merged, "statically typed") {
			t.Error("expected entity description in merged content")
		}
	})

	t.Run("Duplicate", func(t *testing.T) {
		existing := "# Go\n\nAlready has new-article reference"
		entity := EntityInfo{Name: "Go", Type: "term", Description: "Should not be added"}
		src := &Source{Title: "new-article"}

		merged := mergeEntityContent(existing, entity, src)

		if strings.Contains(merged, "## From new-article") {
			t.Error("should not add duplicate section")
		}
	})
}

func TestMergeConceptContent(t *testing.T) {
	src := &Source{Title: "Test Source"}

	t.Run("append to empty content", func(t *testing.T) {
		concept := ConceptInfo{Name: "Test Concept", Description: "A test description"}
		result := mergeConceptContent("", concept, src)
		if !strings.Contains(result, "## From Test Source") {
			t.Errorf("mergeConceptContent should add source section, got: %s", result)
		}
		if !strings.Contains(result, "A test description") {
			t.Errorf("mergeConceptContent should include description, got: %s", result)
		}
	})

	t.Run("duplicate source skipped", func(t *testing.T) {
		concept := ConceptInfo{Name: "Test Concept", Description: "New desc"}
		existing := "## From Test Source\n\nOld description"
		result := mergeConceptContent(existing, concept, src)
		if result != existing {
			t.Errorf("mergeConceptContent should skip duplicate source, got: %s", result)
		}
	})
}

// -------------------------------- others ---------------------------------//
func TestFilepathToLink(t *testing.T) {
	tests := []struct {
		name    string
		rawPath string
		want    string
	}{
		{"normal path", "raw/article/test.md", "../../raw/article/test.md"},
		{"empty path", "", "../../"},
		{"deep path", "raw/paper/sub/deep.md", "../../raw/paper/sub/deep.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filepathToLink(tt.rawPath)
			if got != tt.want {
				t.Errorf("filepathToLink(%q) = %q, want %q", tt.rawPath, got, tt.want)
			}
		})
	}
}

func TestEstimateTokens(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		if got := estimateTokens(""); got != 0 {
			t.Errorf("empty string should be 0 tokens, got %d", got)
		}

		// 400 chars → ~100 tokens
		if got := estimateTokens(strings.Repeat("x", 400)); got != 100 {
			t.Errorf("400 chars should be ~100 tokens, got %d", got)
		}
	})

	t.Run("Edge Cases: single char", func(t *testing.T) {
		if got := estimateTokens("x"); got != 0 {
			t.Errorf("1 char = %d tokens, want 0", got)
		}
	})

	t.Run("Edge Cases: non-ASCII content", func(t *testing.T) {
		content := "你好世界测试文本"
		got := estimateTokens(content)
		if got != 6 {
			t.Errorf("8 CJK chars (24 bytes) = %d tokens, want 6", got)
		}
	})
}

func TestValidateContentSize(t *testing.T) {
	t.Run("within limit", func(t *testing.T) {
		content := strings.Repeat("x", 400) // ~100 tokens
		err := validateContentSize(content, 200)
		if err != nil {
			t.Errorf("content within limit should pass, got: %v", err)
		}
	})

	t.Run("exceeds limit", func(t *testing.T) {
		content := strings.Repeat("x", 4000) // ~1000 tokens
		err := validateContentSize(content, 200)
		if err == nil {
			t.Fatal("content exceeding limit should return error")
		}
	})

	t.Run("no limit configured", func(t *testing.T) {
		content := strings.Repeat("x", 100000)
		err := validateContentSize(content, 0)
		if err != nil {
			t.Errorf("zero limit should disable validation, got: %v", err)
		}
	})

	t.Run("negative limit treated as no limit", func(t *testing.T) {
		content := strings.Repeat("x", 4000)
		err := validateContentSize(content, -1)
		if err != nil {
			t.Errorf("negative limit should disable validation, got: %v", err)
		}
	})

	t.Run("boundary just under limit", func(t *testing.T) {
		content := strings.Repeat("x", 400)
		err := validateContentSize(content, 100)
		if err != nil {
			t.Errorf("content at boundary should pass, got: %v", err)
		}
	})
}

// ---------------------- Mocked LLM chat/chatStream -----------------------//

// mockLLM is a mock LLM provider for testing.
type mockLLM struct {
	response string
}

func (m *mockLLM) Chat(ctx context.Context, messages []llm.Message, opts *llm.ChatOptions) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: m.response}, nil
}

func (m *mockLLM) ChatStream(ctx context.Context, messages []llm.Message, opts *llm.ChatOptions) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 1)
	go func() {
		ch <- llm.Chunk{Content: m.response, Done: true}
		close(ch)
	}()
	return ch, nil
}
