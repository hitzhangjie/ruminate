package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/hitzhangjie/ruminate/internal/llm"
)

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

func TestMergeEntityContent(t *testing.T) {
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
}

func TestMergeEntityContent_Duplicate(t *testing.T) {
	existing := "# Go\n\nAlready has new-article reference"
	entity := EntityInfo{Name: "Go", Type: "term", Description: "Should not be added"}
	src := &Source{Title: "new-article"}

	merged := mergeEntityContent(existing, entity, src)

	if strings.Contains(merged, "## From new-article") {
		t.Error("should not add duplicate section")
	}
}

func TestTruncateContent(t *testing.T) {
	short := "short content"
	if got := truncateContent(short); got != short {
		t.Error("short content should not be truncated")
	}

	long := strings.Repeat("x", 20000)
	truncated := truncateContent(long)
	if len(truncated) >= 20000 {
		t.Error("long content should be truncated")
	}
	if !strings.Contains(truncated, "truncated") {
		t.Error("truncated content should have a note")
	}
}
