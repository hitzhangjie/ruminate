package wiki

import (
	"strings"
	"testing"
)

func TestParseFrontMatter(t *testing.T) {
	t.Run("with sources", func(t *testing.T) {
		content := `---
title: GC
type: concept
sources:
  - path: raw/article/go-gc.md
    ingested_at: 2026-08-04
---

# GC

Body text.
`
		fm, body, err := ParseFrontMatter(content)
		if err != nil {
			t.Fatalf("ParseFrontMatter: %v", err)
		}
		if fm.Title != "GC" {
			t.Errorf("title = %q", fm.Title)
		}
		if fm.Type != "concept" {
			t.Errorf("type = %q", fm.Type)
		}
		if len(fm.Sources) != 1 || fm.Sources[0].Path != "raw/article/go-gc.md" {
			t.Errorf("sources = %+v", fm.Sources)
		}
		if !strings.Contains(body, "# GC") {
			t.Errorf("body missing title: %q", body)
		}
	})

	t.Run("no front matter", func(t *testing.T) {
		content := "# Just a page\n\nHello"
		fm, body, err := ParseFrontMatter(content)
		if err != nil {
			t.Fatalf("ParseFrontMatter: %v", err)
		}
		if fm.Title != "" || len(fm.Sources) != 0 {
			t.Errorf("expected empty front matter, got %+v", fm)
		}
		if body != content {
			t.Errorf("body should be unchanged")
		}
	})
}

func TestWithSources(t *testing.T) {
	body := "# Entity\n\nDescription"
	out := WithSources(body, "Entity", PageTypeEntity, []SourceRef{
		{Path: "raw/note/a.md", IngestedAt: "2026-08-04"},
	})
	if !strings.HasPrefix(out, "---\n") {
		t.Fatalf("expected front matter, got: %s", out)
	}
	fm, gotBody, err := ParseFrontMatter(out)
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "Entity" || fm.Type != "entities" {
		// Type uses PageType string which is "entities"
		if fm.Type != string(PageTypeEntity) {
			t.Errorf("type = %q want %q", fm.Type, PageTypeEntity)
		}
	}
	if len(fm.Sources) != 1 {
		t.Fatalf("sources = %+v", fm.Sources)
	}
	if !strings.Contains(gotBody, "# Entity") {
		t.Errorf("body lost: %q", gotBody)
	}

	// Merge second source
	out2 := WithSources(out, "Entity", PageTypeEntity, []SourceRef{
		{Path: "raw/note/b.md", IngestedAt: "2026-08-05"},
	})
	fm2, _, _ := ParseFrontMatter(out2)
	if len(fm2.Sources) != 2 {
		t.Errorf("expected 2 sources, got %+v", fm2.Sources)
	}
	// Idempotent
	out3 := WithSources(out2, "Entity", PageTypeEntity, []SourceRef{
		{Path: "raw/note/a.md", IngestedAt: "2026-08-99"},
	})
	fm3, _, _ := ParseFrontMatter(out3)
	if len(fm3.Sources) != 2 {
		t.Errorf("duplicate should not add, got %+v", fm3.Sources)
	}
	if fm3.Sources[0].IngestedAt != "2026-08-04" {
		t.Errorf("should keep original ingested_at, got %q", fm3.Sources[0].IngestedAt)
	}
}

func TestExtractSources(t *testing.T) {
	content := WithSources("# X\n", "X", PageTypeConcept, []SourceRef{{Path: "raw/a.md"}})
	srcs := ExtractSources(content)
	if len(srcs) != 1 || srcs[0].Path != "raw/a.md" {
		t.Errorf("ExtractSources = %+v", srcs)
	}
}
