package gitwrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	dir := t.TempDir()
	g := New(dir)

	if g.RepoPath() != dir {
		t.Errorf("RepoPath = %q, want %q", g.RepoPath(), dir)
	}

	if g.IsGitRepo() {
		t.Error("IsRepo should return false for non-git directory")
	}
}

func TestInit(t *testing.T) {
	t.Run("New_And_GitInit", func(t *testing.T) {
		dir := t.TempDir()
		g := New(dir)
		if err := g.Init(); err != nil {
			t.Fatalf("Init() error: %v", err)
		}

		if !g.IsGitRepo() {
			t.Error("IsRepo should return true after Init")
		}

		// Verify .git directory exists
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			t.Errorf(".git should exist after Init: %v", err)
		}
	})

	t.Run("New_And_GitInit_MultipleTimes", func(t *testing.T) {
		dir := t.TempDir()

		g := New(dir)
		if err := g.Init(); err != nil {
			t.Fatalf("first Init() error: %v", err)
		}
		// Second init should succeed
		if err := g.Init(); err != nil {
			t.Errorf("second Init() error: %v", err)
		}
	})
}

func TestAddAndCommit(t *testing.T) {
	t.Run("Commit 1 file", func(t *testing.T) {
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
	})

	t.Run("Commit 2 files", func(t *testing.T) {
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
	})

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
	t.Run("Normal", func(t *testing.T) {
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
	})

	t.Run("Empty", func(t *testing.T) {
		commits, err := parseLogOutput("")
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if len(commits) != 0 {
			t.Errorf("expected 0 commits, got %d", len(commits))
		}
	})

	t.Run("Malformed", func(t *testing.T) {
		output := "incomplete\nabc|def|ghi\n"
		commits, err := parseLogOutput(output)
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if len(commits) != 0 {
			t.Errorf("expected 0 commits for malformed input, got %d", len(commits))
		}
	})
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

func TestHeadSHA(t *testing.T) {
	dir := t.TempDir()
	g := New(dir)
	if err := g.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// HeadSHA requires at least one commit
	writeFile(t, dir, "test.txt", "hello")
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()
	if err := g.Add("test.txt"); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if err := g.Commit("initial"); err != nil {
		t.Fatalf("Commit() error: %v", err)
	}

	sha, err := g.HeadSHA()
	if err != nil {
		t.Fatalf("HeadSHA() error: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("HeadSHA() = %q (len=%d), want 40 hex chars", sha, len(sha))
	}
}

func TestListFiles(t *testing.T) {
	dir := t.TempDir()
	g := New(dir)
	g.Init()
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()

	writeFile(t, dir, "a.txt", "a")
	writeFile(t, dir, "b.txt", "b")
	g.AddAll()
	g.Commit("add files")

	files, err := g.ListFiles()
	if err != nil {
		t.Fatalf("ListFiles() error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}

	// Files should be listed with relative paths
	found := make(map[string]bool)
	for _, f := range files {
		found[f] = true
	}
	if !found["a.txt"] || !found["b.txt"] {
		t.Errorf("ListFiles() missing expected files: %v", files)
	}
}

func TestDiffNameStatus(t *testing.T) {
	dir := t.TempDir()
	g := New(dir)
	g.Init()
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()

	// Commit 1: create two files
	writeFile(t, dir, "keep.txt", "initial")
	writeFile(t, dir, "delete_me.txt", "to be deleted")
	g.AddAll()
	g.Commit("first commit")
	sha1, err := g.HeadSHA()
	if err != nil {
		t.Fatalf("HeadSHA() error: %v", err)
	}

	// Commit 2: add one, modify one, delete one
	writeFile(t, dir, "added.txt", "new file")
	writeFile(t, dir, "keep.txt", "modified content")
	exec.Command("git", "-C", dir, "rm", "delete_me.txt").Run()
	g.AddAll()
	g.Commit("second commit")
	sha2, err := g.HeadSHA()
	if err != nil {
		t.Fatalf("HeadSHA() error: %v", err)
	}

	// Diff between the two commits
	diffs, err := g.DiffNameStatus(sha1, sha2)
	if err != nil {
		t.Fatalf("DiffNameStatus() error: %v", err)
	}
	if len(diffs) < 3 {
		t.Fatalf("expected at least 3 diffs, got %d: %v", len(diffs), diffs)
	}

	statuses := make(map[string]string)
	for _, d := range diffs {
		statuses[d[1]] = d[0]
	}
	// added.txt is new in commit 2
	if statuses["added.txt"] != "A" {
		t.Errorf("added.txt status = %q, want A", statuses["added.txt"])
	}
	// keep.txt was modified (content changed)
	if statuses["keep.txt"] != "M" {
		t.Errorf("keep.txt status = %q, want M", statuses["keep.txt"])
	}
	// delete_me.txt was removed
	if statuses["delete_me.txt"] != "D" {
		t.Errorf("delete_me.txt status = %q, want D", statuses["delete_me.txt"])
	}
}

func TestListFiles_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	g := New(dir)
	g.Init()

	files, err := g.ListFiles()
	if err != nil {
		t.Fatalf("ListFiles() error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("empty repo should have no files, got %v", files)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("writeFile(%s): %v", name, err)
	}
}
