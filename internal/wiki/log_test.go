package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogManager_Init(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log.md")

	lm := NewLogManager(logPath)
	if err := lm.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Verify file content
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	if !strings.Contains(string(content), "# Operations Log") {
		t.Errorf("log.md should contain header, got:\n%s", string(content))
	}
}

func TestLogManager_Append(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log.md")

	lm := NewLogManager(logPath)
	if err := lm.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Append first entry
	if err := lm.Append("create", PageTypeEntity, "karpathy", "Initial creation"); err != nil {
		t.Fatalf("Append() error: %v", err)
	}

	// Append second entry
	if err := lm.Append("update", PageTypeConcept, "deep-learning", "Added references"); err != nil {
		t.Fatalf("Append() error: %v", err)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	s := string(content)

	// Verify both entries exist
	if !strings.Contains(s, "create | entities | karpathy") {
		t.Errorf("log should contain create entry, got:\n%s", s)
	}
	if !strings.Contains(s, "update | concepts | deep-learning") {
		t.Errorf("log should contain update entry, got:\n%s", s)
	}

	// Most recent entry should appear first
	firstIdx := strings.Index(s, "deep-learning")
	secondIdx := strings.Index(s, "karpathy")
	if firstIdx < 0 || secondIdx < 0 {
		t.Fatalf("entries not found in log")
	}
	if firstIdx > secondIdx {
		t.Errorf("most recent entry should appear first (deep-learning before karpathy)")
	}
}

func TestLogManager_ReadLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log.md")

	lm := NewLogManager(logPath)
	if err := lm.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	lm.Append("create", PageTypeEntity, "karpathy", "")
	lm.Append("update", PageTypeSummary, "test-summary", "")
	lm.Append("delete", PageTypeConcept, "old-concept", "")

	entries, err := lm.ReadLog()
	if err != nil {
		t.Fatalf("ReadLog() error: %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}

	// Entries should be in order (most recent first)
	if entries[0].Operation != "delete" {
		t.Errorf("first entry should be 'delete', got %q", entries[0].Operation)
	}
	if entries[1].Operation != "update" {
		t.Errorf("second entry should be 'update', got %q", entries[1].Operation)
	}
	if entries[2].Operation != "create" {
		t.Errorf("third entry should be 'create', got %q", entries[2].Operation)
	}
}

func TestLogManager_RecentEntries(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log.md")

	lm := NewLogManager(logPath)
	if err := lm.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	for i := 0; i < 10; i++ {
		lm.Append("create", PageTypeEntity, fmt.Sprintf("page-%d", i), "")
	}

	recent, err := lm.RecentEntries(3)
	if err != nil {
		t.Fatalf("RecentEntries() error: %v", err)
	}

	if len(recent) != 3 {
		t.Errorf("expected 3 recent entries, got %d", len(recent))
	}

	// Entries should be the most recent 3, in order (most recent first).
	want := []string{"page-9", "page-8", "page-7"}
	for i, entry := range recent {
		if entry.Title != want[i] {
			t.Errorf("recent[%d].Title = %q, want %q", i, entry.Title, want[i])
		}
	}
}

func TestLogManager_ReadLog_Nonexistent(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nonexistent", "log.md")

	lm := NewLogManager(logPath)
	entries, err := lm.ReadLog()
	if err != nil {
		t.Fatalf("ReadLog() error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for nonexistent log, got %d", len(entries))
	}
}

func TestParseLogMd(t *testing.T) {
	content := `# Operations Log

> Chronological record.

## [2026-07-01] create | entities | karpathy

## [2026-06-30] update | concepts | deep-learning

Added references.
`

	entries := parseLogMd(content)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Operation != "create" {
		t.Errorf("first entry op = %q, want 'create'", entries[0].Operation)
	}
	if entries[0].PageType != PageTypeEntity {
		t.Errorf("first entry type = %q, want 'entities'", entries[0].PageType)
	}
	if entries[0].Title != "karpathy" {
		t.Errorf("first entry title = %q, want 'karpathy'", entries[0].Title)
	}

	if entries[1].Operation != "update" {
		t.Errorf("second entry op = %q, want 'update'", entries[1].Operation)
	}
	if entries[1].PageType != PageTypeConcept {
		t.Errorf("second entry type = %q, want 'concepts'", entries[1].PageType)
	}
	if entries[1].Title != "deep-learning" {
		t.Errorf("second entry title = %q, want 'deep-learning'", entries[1].Title)
	}
}
