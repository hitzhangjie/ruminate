package gitwrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	g := New("/tmp/test")
	if g.RepoPath() != "/tmp/test" {
		t.Errorf("RepoPath = %q, want /tmp/test", g.RepoPath())
	}
}

func TestInit(t *testing.T) {
	dir := t.TempDir()

	g := New(dir)
	if err := g.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if !g.IsRepo() {
		t.Error("IsRepo should return true after Init")
	}

	// Verify .git directory exists
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Errorf(".git should exist after Init: %v", err)
	}
}

func TestInit_Idempotent(t *testing.T) {
	dir := t.TempDir()

	g := New(dir)
	if err := g.Init(); err != nil {
		t.Fatalf("first Init() error: %v", err)
	}
	// Second init should succeed
	if err := g.Init(); err != nil {
		t.Errorf("second Init() error: %v", err)
	}
}

func TestIsRepo_False(t *testing.T) {
	dir := t.TempDir()
	g := New(dir)
	if g.IsRepo() {
		t.Error("IsRepo should return false for non-git directory")
	}
}

func TestAddAndCommit(t *testing.T) {
	dir := t.TempDir()

	// Initialize git repo
	g := New(dir)
	if err := g.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(testFile, []byte("# Test"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	// Stage the file
	if err := g.Add("test.md"); err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	// Commit
	if err := g.Commit("initial commit"); err != nil {
		// In CI environments without git user config, this may fail
		// Try to set minimal config
		exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
		exec.Command("git", "-C", dir, "config", "user.name", "Test User").Run()
		if err := g.Commit("initial commit"); err != nil {
			t.Fatalf("Commit() error: %v", err)
		}
	}
}

func TestLog(t *testing.T) {
	dir := t.TempDir()

	g := New(dir)
	if err := g.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Set up git user config
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test User").Run()

	// Create and commit a file
	os.WriteFile(filepath.Join(dir, "test.md"), []byte("# Test"), 0644)
	g.Add("test.md")
	g.Commit("test commit")

	commits, err := g.Log(5)
	if err != nil {
		t.Fatalf("Log() error: %v", err)
	}
	if len(commits) != 1 {
		t.Errorf("expected 1 commit, got %d", len(commits))
	}
	if commits[0].Message != "test commit" {
		t.Errorf("commit message = %q, want 'test commit'", commits[0].Message)
	}
}

func TestParseLogOutput(t *testing.T) {
	output := "abc123|Test User|2026-07-01T10:00:00+08:00|test commit\n"
	commits, err := parseLogOutput(output)
	if err != nil {
		t.Fatalf("parseLogOutput error: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}
	if commits[0].Hash != "abc123" {
		t.Errorf("Hash = %q", commits[0].Hash)
	}
	if commits[0].Author != "Test User" {
		t.Errorf("Author = %q", commits[0].Author)
	}
	if commits[0].Message != "test commit" {
		t.Errorf("Message = %q", commits[0].Message)
	}
}

func TestParseLogOutput_Empty(t *testing.T) {
	commits, err := parseLogOutput("")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(commits) != 0 {
		t.Errorf("expected 0 commits, got %d", len(commits))
	}
}

func TestParseLogOutput_Malformed(t *testing.T) {
	output := "incomplete\nabc|def|ghi\n"
	commits, err := parseLogOutput(output)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(commits) != 0 {
		t.Errorf("expected 0 commits for malformed input, got %d", len(commits))
	}
}

func TestAddAll(t *testing.T) {
	dir := t.TempDir()

	g := New(dir)
	if err := g.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Create multiple files
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A"), 0644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("# B"), 0644)

	if err := g.AddAll(); err != nil {
		t.Fatalf("AddAll() error: %v", err)
	}

	// Set up git user config and commit
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test User").Run()
	if err := g.Commit("add all"); err != nil {
		t.Fatalf("Commit() error: %v", err)
	}
}

func TestGit_RunError(t *testing.T) {
	// Calling git with invalid args on a valid repo should error
	dir := t.TempDir()
	g := New(dir)
	g.Init()

	// Temporarily redirect stderr to /dev/null to avoid noise
	err := g.run("nonexistent-git-command")
	if err == nil {
		t.Error("expected error for invalid git command")
	}
	if !strings.Contains(err.Error(), "git") {
		t.Errorf("error should mention git: %v", err)
	}
}
