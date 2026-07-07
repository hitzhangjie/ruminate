// Package sync implements the sync engine that detects file changes in a source
// repository and incrementally ingests them into the ruminate knowledge base.
//
// The sync engine maintains per-source-repo state in the wiki's .ruminate directory,
// tracking the last synced commit so only new changes are processed on each run.
package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/gitwrap"
	"github.com/hitzhangjie/ruminate/internal/ingest"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// SyncState tracks the sync progress for one or more source repositories.
// Stored as JSON at wiki_root/.ruminate/sync_state.json.
type SyncState struct {
	Sources map[string]SourceSyncState `json:"sources"`
}

// SourceSyncState records the last synced commit for a single source repository.
type SourceSyncState struct {
	LastSyncedCommit string    `json:"last_synced_commit"`
	LastSyncedAt     time.Time `json:"last_synced_at"`
}

// SyncResult summarizes the outcome of a sync operation.
type SyncResult struct {
	SourceRepo    string
	FilesAdded    int
	FilesModified int
	FilesRenamed  int
	FilesDeleted  int
	FilesSkipped  int
	Errors        []string
}

// Engine drives the sync pipeline: detect changes → ingest new/modified files → update state.
type Engine struct {
	wikiRoot     string
	ingestEngine *ingest.Engine
	wikiManager  *wiki.Manager
	sourceRepo   string
	sourceType   string
	dryRun       bool
}

// NewEngine creates a new sync Engine.
//
// sourceRepo is the path to the source (notes) repository.
// sourceType is the default type for ingested files ("note", "article", etc.).
// dryRun enables dry-run mode where changes are detected but not applied.
func NewEngine(cfg *config.Config, sourceRepo, sourceType string, dryRun bool) (*Engine, error) {
	engine, err := ingest.NewEngine(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating ingest engine: %w", err)
	}

	// Create a wiki.Manager for log access and page operations during sync.
	mgr := wiki.NewManager(cfg)

	return &Engine{
		wikiRoot:     cfg.WikiPath,
		ingestEngine: engine,
		wikiManager:  mgr,
		sourceRepo:   sourceRepo,
		sourceType:   sourceType,
		dryRun:       dryRun,
	}, nil
}

// statePath returns the path to the sync state file in the wiki's .ruminate directory.
func (e *Engine) statePath() string {
	return filepath.Join(e.wikiRoot, ".ruminate", "sync_state.json")
}

// loadState reads the sync state from disk. Returns an empty state if the file
// doesn't exist (first sync).
func (e *Engine) loadState() (*SyncState, error) {
	state := &SyncState{Sources: make(map[string]SourceSyncState)}

	data, err := os.ReadFile(e.statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return nil, fmt.Errorf("reading sync state: %w", err)
	}

	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("parsing sync state: %w", err)
	}

	if state.Sources == nil {
		state.Sources = make(map[string]SourceSyncState)
	}

	return state, nil
}

// saveState writes the sync state to disk.
func (e *Engine) saveState(state *SyncState) error {
	ruminateDir := filepath.Join(e.wikiRoot, ".ruminate")
	if err := os.MkdirAll(ruminateDir, 0755); err != nil {
		return fmt.Errorf("creating .ruminate dir: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling sync state: %w", err)
	}

	if err := os.WriteFile(e.statePath(), data, 0644); err != nil {
		return fmt.Errorf("writing sync state: %w", err)
	}

	return nil
}

// Sync runs a full sync cycle for the source repository.
//
// Pipeline:
//  1. Verify source repo is a git repository
//  2. Read last synced commit from state
//  3. Get current HEAD SHA
//  4. Diff between last synced and HEAD (or list all files on first sync)
//  5. Process each changed file (A/M/R/D)
//  6. Update sync state
func (e *Engine) Sync(ctx context.Context) (*SyncResult, error) {
	result := &SyncResult{SourceRepo: e.sourceRepo}

	git := gitwrap.New(e.sourceRepo)
	if !git.IsGitRepo() {
		return nil, fmt.Errorf("%s is not a git repository", e.sourceRepo)
	}

	headSHA, err := git.HeadSHA()
	if err != nil {
		return nil, fmt.Errorf("getting HEAD SHA: %w", err)
	}

	state, err := e.loadState()
	if err != nil {
		return nil, err
	}

	sourceState, exists := state.Sources[e.sourceRepo]
	lastSynced := sourceState.LastSyncedCommit

	var changes [][2]string

	if !exists || lastSynced == "" {
		// First sync — ingest all tracked files
		fmt.Printf("First sync for %s — ingesting all tracked files\n", e.sourceRepo)
		files, err := git.ListFiles()
		if err != nil {
			return nil, fmt.Errorf("listing tracked files: %w", err)
		}
		for _, f := range files {
			changes = append(changes, [2]string{"A", f})
		}
	} else if lastSynced == headSHA {
		fmt.Println("Already up to date — no new commits to sync")
		return result, nil
	} else {
		changes, err = git.DiffNameStatus(lastSynced, headSHA)
		if err != nil {
			return nil, fmt.Errorf("getting diff: %w", err)
		}
	}

	if len(changes) == 0 {
		fmt.Println("No file changes detected")
	} else {
		fmt.Printf("Detected %d file change(s)\n", len(changes))
		if err := e.processChanges(ctx, changes, result); err != nil {
			return nil, err
		}
	}

	// Update sync state
	if e.dryRun {
		fmt.Printf("[dry-run] Would update sync state to commit: %s\n", headSHA[:min(8, len(headSHA))])
	} else {
		now := time.Now().UTC()
		state.Sources[e.sourceRepo] = SourceSyncState{
			LastSyncedCommit: headSHA,
			LastSyncedAt:     now,
		}
		if err := e.saveState(state); err != nil {
			return nil, fmt.Errorf("saving sync state: %w", err)
		}
	}

	return result, nil
}

// processChanges iterates over the diff entries and processes each file.
func (e *Engine) processChanges(ctx context.Context, changes [][2]string, result *SyncResult) error {
	for _, change := range changes {
		status := change[0]
		filePath := change[1]

		// For renames, the status is like "R100" and the filePath may be the new path.
		// Normalize the status to first character.
		statusCode := status[0:1]

		// Deletions don't read file content — always process them regardless of
		// file type, since they only update wiki metadata.
		if statusCode == "D" {
			fmt.Printf("  [D] %s (keeping wiki content)\n", filePath)
			if e.dryRun {
				fmt.Printf("    [dry-run] Would mark as deleted in log\n")
			} else {
				if err := e.handleDeletedFile(filePath); err != nil {
					errMsg := fmt.Sprintf("handling deleted %s: %v", filePath, err)
					result.Errors = append(result.Errors, errMsg)
					fmt.Fprintf(os.Stderr, "  Error: %s\n", errMsg)
					continue
				}
			}
			result.FilesDeleted++
			continue
		}

		// For A/M/R, check that the file type is supported before attempting ingest.
		ext := strings.ToLower(filepath.Ext(filePath))
		if !e.ingestEngine.IsSupportedExtension(ext) {
			fmt.Printf("  [-] %s (unsupported file type, skipping)\n", filePath)
			result.FilesSkipped++
			continue
		}

		fullPath := filepath.Join(e.sourceRepo, filePath)

		switch statusCode {
		case "A":
			fmt.Printf("  [A] %s\n", filePath)
			if e.dryRun {
				fmt.Printf("    [dry-run] Would ingest: %s\n", fullPath)
			} else {
				if err := e.ingestFile(ctx, fullPath); err != nil {
					errMsg := fmt.Sprintf("ingesting %s: %v", filePath, err)
					result.Errors = append(result.Errors, errMsg)
					fmt.Fprintf(os.Stderr, "  Error: %s\n", errMsg)
					continue
				}
			}
			result.FilesAdded++

		case "M":
			fmt.Printf("  [M] %s\n", filePath)
			if e.dryRun {
				fmt.Printf("    [dry-run] Would re-ingest: %s\n", fullPath)
			} else {
				if err := e.ingestFile(ctx, fullPath); err != nil {
					errMsg := fmt.Sprintf("ingesting %s: %v", filePath, err)
					result.Errors = append(result.Errors, errMsg)
					fmt.Fprintf(os.Stderr, "  Error: %s\n", errMsg)
					continue
				}
			}
			result.FilesModified++

		case "R":
			fmt.Printf("  [R] %s\n", filePath)
			if e.dryRun {
				fmt.Printf("    [dry-run] Would ingest renamed file: %s\n", fullPath)
			} else {
				if err := e.ingestFile(ctx, fullPath); err != nil {
					errMsg := fmt.Sprintf("ingesting %s: %v", filePath, err)
					result.Errors = append(result.Errors, errMsg)
					fmt.Fprintf(os.Stderr, "  Error: %s\n", errMsg)
					continue
				}
			}
			result.FilesRenamed++

		default:
			fmt.Printf("  [?] %s (unhandled status: %s)\n", filePath, status)
		}
	}
	return nil
}

// ingestFile calls the ingest engine to process a single file.
func (e *Engine) ingestFile(ctx context.Context, filePath string) error {
	return e.ingestEngine.Ingest(ctx, filePath, e.sourceType)
}

// handleDeletedFile records a deletion event in the wiki log and marks the
// corresponding summary page with a warning.
//
// Per sync-hook-design.md: wiki content is NOT deleted when the source file is
// removed — extracted knowledge (entities, concepts) may still be referenced by
// other sources.
func (e *Engine) handleDeletedFile(sourceFile string) error {
	title := filepath.Base(sourceFile)
	title = strings.TrimSuffix(title, filepath.Ext(title))

	// Append deletion event to log.md
	desc := fmt.Sprintf("Source file deleted: `%s`. Wiki content preserved.", sourceFile)
	if err := e.wikiManager.Log().Append("delete", wiki.PageTypeSummary, title, desc); err != nil {
		return fmt.Errorf("logging deletion: %w", err)
	}

	// Try to mark the summary page with a warning
	page, err := e.wikiManager.Read(title, wiki.PageTypeSummary)
	if err != nil {
		// Summary page doesn't exist — nothing to mark
		return nil
	}

	// Append warning marker if not already present
	if !strings.Contains(page.Content, "⚠️ Source removed") {
		warning := "\n\n> ⚠️ **Source removed**: The original source file has been deleted. Wiki content is preserved for reference.\n"
		newContent := page.Content + warning
		if _, err := e.wikiManager.Update(title, wiki.PageTypeSummary, newContent); err != nil {
			return fmt.Errorf("marking summary page: %w", err)
		}
	}

	return nil
}
