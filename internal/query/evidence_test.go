package query

import (
	"strings"
	"testing"

	"github.com/hitzhangjie/ruminate/internal/wiki"
)

func TestNeedsEvidenceEscalation(t *testing.T) {
	if !needsEvidenceEscalation("hello", nil) {
		t.Error("empty sources should escalate")
	}
	if !needsEvidenceEscalation("原文怎么说", []Source{{Title: "A"}, {Title: "B"}}) {
		t.Error("trigger word should escalate")
	}
	if needsEvidenceEscalation("what is GC", []Source{{Title: "A"}, {Title: "B"}, {Title: "C"}}) {
		t.Error("enough sources, no trigger — should not escalate")
	}
}

func TestParseEvidenceMode(t *testing.T) {
	if ParseEvidenceMode("auto") != EvidenceAuto {
		t.Fatal()
	}
	if ParseEvidenceMode("RAW") != EvidenceRaw {
		t.Fatal()
	}
	if ParseEvidenceMode("") != EvidenceWiki {
		t.Fatal()
	}
}

type memReader struct {
	pages map[string]*wiki.Page
}

func (m *memReader) ReadByPath(path string) (*wiki.Page, error) {
	p, ok := m.pages[path]
	if !ok {
		return nil, errNotFound
	}
	return p, nil
}

func (m *memReader) SearchRaw(query string, topN int) ([]wiki.SearchResult, error) {
	return nil, nil
}

func TestAttachEvidence(t *testing.T) {
	r := &memReader{pages: map[string]*wiki.Page{
		"wiki/concepts/GC.md": {
			Title: "GC",
			Path:  "wiki/concepts/GC.md",
			Sources: []wiki.SourceRef{
				{Path: "raw/article/gc.md"},
			},
		},
		"raw/article/gc.md": {
			Title:   "gc",
			Path:    "raw/article/gc.md",
			Content: "Original text about garbage collection defaults.",
		},
	}}
	wikiSrc := []Source{{Title: "GC", Path: "wiki/concepts/GC.md", Layer: "wiki"}}
	out := attachEvidence(r, wikiSrc, "defaults", 10*1024)
	if len(out) < 2 {
		t.Fatalf("expected wiki+raw, got %d", len(out))
	}
	foundRaw := false
	for _, s := range out {
		if s.Layer == "raw" {
			foundRaw = true
			if !strings.Contains(s.Content, "garbage collection") {
				t.Errorf("raw content missing: %+v", s)
			}
		}
	}
	if !foundRaw {
		t.Error("expected raw layer source")
	}
}
