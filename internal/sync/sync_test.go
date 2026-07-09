package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestEngine creates an Engine with just enough fields to test state I/O.
func newTestEngine(wikiRoot string) *Engine {
	return &Engine{
		wikiRoot: wikiRoot,
	}
}

// =============================================================================
// loadState
// =============================================================================

func TestLoadState(t *testing.T) {
	t.Run("no file returns empty state", func(t *testing.T) {
		dir := t.TempDir()
		e := newTestEngine(dir)

		state, err := e.loadState()
		if err != nil {
			t.Fatalf("loadState() error: %v", err)
		}
		if state == nil {
			t.Fatal("loadState() returned nil")
		}
		if len(state.Sources) != 0 {
			t.Errorf("expected empty Sources, got %d", len(state.Sources))
		}
	})

	t.Run("corrupt JSON returns error", func(t *testing.T) {
		dir := t.TempDir()
		e := newTestEngine(dir)

		ruminateDir := filepath.Join(dir, "db")
		os.MkdirAll(ruminateDir, 0755)
		os.WriteFile(filepath.Join(ruminateDir, "sync_state.json"), []byte("{not json"), 0644)

		_, err := e.loadState()
		if err == nil {
			t.Error("loadState() should error on malformed JSON")
		}
	})

	t.Run("round-trip with save and load", func(t *testing.T) {
		dir := t.TempDir()
		e := newTestEngine(dir)

		original := &SyncState{
			Sources: map[string]SourceSyncState{
				"/path/to/repo": {
					LastSyncedCommit: "abc123def456",
					LastSyncedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				},
			},
		}
		if err := e.saveState(original); err != nil {
			t.Fatalf("saveState() error: %v", err)
		}

		loaded, err := e.loadState()
		if err != nil {
			t.Fatalf("loadState() after save error: %v", err)
		}
		if len(loaded.Sources) != 1 {
			t.Fatalf("expected 1 source, got %d", len(loaded.Sources))
		}

		repoState, ok := loaded.Sources["/path/to/repo"]
		if !ok {
			t.Fatal("source /path/to/repo not found in loaded state")
		}
		if repoState.LastSyncedCommit != "abc123def456" {
			t.Errorf("LastSyncedCommit = %q, want abc123def456", repoState.LastSyncedCommit)
		}
	})
}

// =============================================================================
// saveState
// =============================================================================

func TestSaveState(t *testing.T) {
	t.Run("creates parent directory", func(t *testing.T) {
		dir := t.TempDir()
		e := newTestEngine(dir)

		// db dir should not exist yet
		ruminateDir := filepath.Join(dir, "db")
		if _, err := os.Stat(ruminateDir); !os.IsNotExist(err) {
			t.Fatal("db dir already exists")
		}

		state := &SyncState{Sources: map[string]SourceSyncState{}}
		if err := e.saveState(state); err != nil {
			t.Fatalf("saveState() error: %v", err)
		}

		if info, err := os.Stat(ruminateDir); err != nil || !info.IsDir() {
			t.Error("saveState() should create db directory")
		}
	})

	t.Run("writes valid JSON with indentation", func(t *testing.T) {
		dir := t.TempDir()
		e := newTestEngine(dir)

		state := &SyncState{
			Sources: map[string]SourceSyncState{
				"repo1": {
					LastSyncedCommit: "sha1",
					LastSyncedAt:     time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
				},
			},
		}
		if err := e.saveState(state); err != nil {
			t.Fatalf("saveState() error: %v", err)
		}

		// Verify the saved file is valid JSON on disk
		statePath := filepath.Join(dir, "db", "sync_state.json")
		data, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("reading saved state: %v", err)
		}

		var loaded SyncState
		if err := json.Unmarshal(data, &loaded); err != nil {
			t.Fatalf("saved state is not valid JSON: %v", err)
		}
		if len(loaded.Sources) != 1 {
			t.Errorf("expected 1 source, got %d", len(loaded.Sources))
		}
	})
}

// =============================================================================
// statePath
// =============================================================================

func TestStatePath(t *testing.T) {
	e := newTestEngine("/tmp/wiki")
	got := e.statePath()
	want := filepath.Join("/tmp/wiki", "db", "sync_state.json")
	if got != want {
		t.Errorf("statePath() = %q, want %q", got, want)
	}
}
