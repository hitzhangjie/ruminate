package lint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSuppressionFilePath(t *testing.T) {
	got := suppressionFilePath("/home/user/wiki")
	want := filepath.Join("/home/user/wiki", ".ruminate", "lint-suppressions.json")
	if got != want {
		t.Errorf("suppressionFilePath = %q, want %q", got, want)
	}
}

func TestLoadSuppressions(t *testing.T) {
	t.Run("non-existent file returns empty", func(t *testing.T) {
		dir := t.TempDir()
		sf, err := LoadSuppressions(dir)
		if err != nil {
			t.Fatalf("LoadSuppressions() error: %v", err)
		}
		if sf == nil {
			t.Fatal("LoadSuppressions() returned nil")
		}
		if len(sf.Suppressions) != 0 {
			t.Errorf("expected empty suppressions, got %d", len(sf.Suppressions))
		}
		if sf.Version != 1 {
			t.Errorf("version = %d, want 1", sf.Version)
		}
	})

	t.Run("valid file loads correctly", func(t *testing.T) {
		dir := t.TempDir()
		ruminateDir := filepath.Join(dir, ".ruminate")
		os.MkdirAll(ruminateDir, 0755)
		suppressionPath := filepath.Join(ruminateDir, "lint-suppressions.json")

		content := `{
  "version": 1,
  "suppressions": [
    {"id": "abc123", "check": "contradiction", "page": "page_a", "related_page": "page_b", "reason": "test", "created_at": "2026-01-01T00:00:00Z"}
  ]
}`
		os.WriteFile(suppressionPath, []byte(content), 0644)

		sf, err := LoadSuppressions(dir)
		if err != nil {
			t.Fatalf("LoadSuppressions() error: %v", err)
		}
		if len(sf.Suppressions) != 1 {
			t.Fatalf("expected 1 suppression, got %d", len(sf.Suppressions))
		}
		if sf.Suppressions[0].ID != "abc123" {
			t.Errorf("ID = %q, want 'abc123'", sf.Suppressions[0].ID)
		}
	})

	t.Run("malformed JSON returns error", func(t *testing.T) {
		dir := t.TempDir()
		ruminateDir := filepath.Join(dir, ".ruminate")
		os.MkdirAll(ruminateDir, 0755)
		os.WriteFile(filepath.Join(ruminateDir, "lint-suppressions.json"), []byte("{not json"), 0644)

		_, err := LoadSuppressions(dir)
		if err == nil {
			t.Error("LoadSuppressions() should error on malformed JSON")
		}
	})

	t.Run("zero version upgraded to 1", func(t *testing.T) {
		dir := t.TempDir()
		ruminateDir := filepath.Join(dir, ".ruminate")
		os.MkdirAll(ruminateDir, 0755)
		content := `{"version": 0, "suppressions": []}`
		os.WriteFile(filepath.Join(ruminateDir, "lint-suppressions.json"), []byte(content), 0644)

		sf, err := LoadSuppressions(dir)
		if err != nil {
			t.Fatalf("LoadSuppressions() error: %v", err)
		}
		if sf.Version != 1 {
			t.Errorf("version = %d, want 1 (upgraded from 0)", sf.Version)
		}
	})

	t.Run("null suppressions upgraded to empty slice", func(t *testing.T) {
		dir := t.TempDir()
		ruminateDir := filepath.Join(dir, ".ruminate")
		os.MkdirAll(ruminateDir, 0755)
		content := `{"version": 1}`
		os.WriteFile(filepath.Join(ruminateDir, "lint-suppressions.json"), []byte(content), 0644)

		sf, err := LoadSuppressions(dir)
		if err != nil {
			t.Fatalf("LoadSuppressions() error: %v", err)
		}
		if sf.Suppressions == nil {
			t.Error("Suppressions should not be nil, should be empty slice")
		}
	})
}

func TestSuppressionFile(t *testing.T) {
	t.Run("IsSuppressed", func(t *testing.T) {
		sf := &SuppressionFile{
			Suppressions: []Suppression{
				{Check: "contradiction", Page: "alpha", RelatedPage: "beta"},
				{Check: "broken_link", Page: "ghost_page", RelatedPage: ""},
			},
		}

		tests := []struct {
			name       string
			issue      Issue
			suppressed bool
		}{
			{
				name:       "exact match",
				issue:      Issue{Check: "contradiction", Page: "alpha", RelatedPage: "beta"},
				suppressed: true,
			},
			{
				name:       "order-independent match",
				issue:      Issue{Check: "contradiction", Page: "beta", RelatedPage: "alpha"},
				suppressed: true,
			},
			{
				name:       "no related page match",
				issue:      Issue{Check: "broken_link", Page: "ghost_page"},
				suppressed: true,
			},
			{
				name:       "no match - different check",
				issue:      Issue{Check: "staleness", Page: "alpha", RelatedPage: "beta"},
				suppressed: false,
			},
			{
				name:       "no match - different pages",
				issue:      Issue{Check: "contradiction", Page: "gamma", RelatedPage: "delta"},
				suppressed: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := sf.IsSuppressed(tt.issue)
				if got != tt.suppressed {
					t.Errorf("IsSuppressed = %v, want %v", got, tt.suppressed)
				}
			})
		}
	})

	t.Run("Add", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".ruminate", "lint-suppressions.json")
		sf := &SuppressionFile{
			Version:      1,
			Suppressions: []Suppression{},
			path:         path,
		}

		// Add first suppression
		err := sf.Add("contradiction", "page_a", "page_b", "test reason")
		if err != nil {
			t.Fatalf("Add() error: %v", err)
		}
		if len(sf.Suppressions) != 1 {
			t.Fatalf("expected 1 suppression, got %d", len(sf.Suppressions))
		}
		if sf.Suppressions[0].Reason != "test reason" {
			t.Errorf("reason = %q, want 'test reason'", sf.Suppressions[0].Reason)
		}

		// Adding same suppression should be idempotent
		err = sf.Add("contradiction", "page_a", "page_b", "duplicate")
		if err != nil {
			t.Fatalf("Add() duplicate error: %v", err)
		}
		if len(sf.Suppressions) != 1 {
			t.Errorf("duplicate Add should be idempotent, got %d suppressions", len(sf.Suppressions))
		}

		// Add second suppression
		err = sf.Add("broken_link", "ghost", "", "link is known")
		if err != nil {
			t.Fatalf("Add() second error: %v", err)
		}
		if len(sf.Suppressions) != 2 {
			t.Fatalf("expected 2 suppressions, got %d", len(sf.Suppressions))
		}

		// Verify file was saved
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading saved file: %v", err)
		}
		var saved SuppressionFile
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatalf("parsing saved file: %v", err)
		}
		if len(saved.Suppressions) != 2 {
			t.Errorf("saved file has %d suppressions, want 2", len(saved.Suppressions))
		}
	})

	t.Run("Remove", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".ruminate", "lint-suppressions.json")
		sf := &SuppressionFile{
			Version:      1,
			Suppressions: []Suppression{},
			path:         path,
		}

		id := suppressionID("contradiction", "a", "b")
		sf.Add("contradiction", "a", "b", "reason")

		// Remove existing
		err := sf.Remove(id)
		if err != nil {
			t.Fatalf("Remove() error: %v", err)
		}
		if len(sf.Suppressions) != 0 {
			t.Errorf("expected 0 suppressions after remove, got %d", len(sf.Suppressions))
		}

		// Remove non-existent
		err = sf.Remove("nonexistent_id__")
		if err == nil {
			t.Error("Remove() should error for non-existent ID")
		}
	})

	t.Run("List", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".ruminate", "lint-suppressions.json")
		sf := &SuppressionFile{
			Version:      1,
			Suppressions: []Suppression{},
			path:         path,
		}

		// Empty list
		list := sf.List()
		if len(list) != 0 {
			t.Errorf("empty list should have 0 items, got %d", len(list))
		}

		// Add items
		sf.Add("orphan", "page1", "", "r1")
		sf.Add("staleness", "page2", "", "r2")

		list = sf.List()
		if len(list) != 2 {
			t.Errorf("list should have 2 items, got %d", len(list))
		}

		// Verify it's a copy: modifying the returned slice doesn't affect original
		list[0] = Suppression{}
		if sf.Suppressions[0].Check == "" {
			t.Error("List() should return a copy, not the internal slice")
		}
	})

	t.Run("Save", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".ruminate", "lint-suppressions.json")
		sf := &SuppressionFile{
			Version:      1,
			Suppressions: []Suppression{},
			path:         path,
		}
		sf.Add("contradiction", "a", "b", "test save")

		// Verify directory was created
		if _, err := os.Stat(filepath.Dir(path)); os.IsNotExist(err) {
			t.Error("Save() should create parent directory")
		}

		// Verify file exists and is valid JSON
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading saved file: %v", err)
		}
		var check SuppressionFile
		if err := json.Unmarshal(data, &check); err != nil {
			t.Fatalf("saved file is not valid JSON: %v", err)
		}

		// File should end with newline
		if len(data) > 0 && data[len(data)-1] != '\n' {
			t.Error("saved file should end with newline")
		}
	})
}

func TestSuppressionID(t *testing.T) {
	// Same inputs should produce same ID
	id1 := suppressionID("contradiction", "page_a", "page_b")
	id2 := suppressionID("contradiction", "page_a", "page_b")
	if id1 != id2 {
		t.Errorf("suppressionID not deterministic: %q vs %q", id1, id2)
	}

	// Order of pages should not matter (stabilized by sorting)
	idOrdered1 := suppressionID("contradiction", "alpha", "beta")
	idOrdered2 := suppressionID("contradiction", "beta", "alpha")
	if idOrdered1 != idOrdered2 {
		t.Errorf("suppressionID should be order-independent: %q vs %q", idOrdered1, idOrdered2)
	}

	// Different checks produce different IDs
	idCheck1 := suppressionID("contradiction", "a", "b")
	idCheck2 := suppressionID("broken_link", "a", "b")
	if idCheck1 == idCheck2 {
		t.Error("different checks should produce different IDs")
	}

	// With empty related page
	idNoRel := suppressionID("orphan", "page_a", "")
	if idNoRel == "" {
		t.Error("suppressionID should not be empty")
	}

	// ID should be a hex string of length 16 (8 bytes)
	if len(id1) != 16 {
		t.Errorf("suppressionID length = %d, want 16", len(id1))
	}
}

func TestFilterSuppressed(t *testing.T) {
	issues := []Issue{
		{Check: "contradiction", Page: "a", RelatedPage: "b"},
		{Check: "contradiction", Page: "c", RelatedPage: "d"},
		{Check: "broken_link", Page: "x"},
	}

	t.Run("nil SuppressionFile returns all issues", func(t *testing.T) {
		got := FilterSuppressed(issues, nil)
		if len(got) != 3 {
			t.Errorf("FilterSuppressed with nil = %d issues, want 3", len(got))
		}
	})

	t.Run("empty suppressions returns all issues", func(t *testing.T) {
		sf := &SuppressionFile{Suppressions: []Suppression{}}
		got := FilterSuppressed(issues, sf)
		if len(got) != 3 {
			t.Errorf("FilterSuppressed empty = %d issues, want 3", len(got))
		}
	})

	t.Run("all suppressed returns empty", func(t *testing.T) {
		sf := &SuppressionFile{
			Suppressions: []Suppression{
				{Check: "contradiction", Page: "a", RelatedPage: "b"},
				{Check: "contradiction", Page: "c", RelatedPage: "d"},
				{Check: "broken_link", Page: "x", RelatedPage: ""},
			},
		}
		got := FilterSuppressed(issues, sf)
		if len(got) != 0 {
			t.Errorf("FilterSuppressed all = %d issues, want 0", len(got))
		}
	})

	t.Run("partial suppression", func(t *testing.T) {
		sf := &SuppressionFile{
			Suppressions: []Suppression{
				{Check: "contradiction", Page: "a", RelatedPage: "b"},
			},
		}
		got := FilterSuppressed(issues, sf)
		if len(got) != 2 {
			t.Errorf("FilterSuppressed partial = %d issues, want 2", len(got))
		}
	})
}
