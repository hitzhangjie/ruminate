package gitwrap

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Git provides a thin wrapper around git operations.
// It uses os/exec to call the system git binary.
type Git struct {
	// repoPath is the path to the git repository.
	repoPath string
}

// CommitInfo holds metadata about a single commit.
type CommitInfo struct {
	Hash    string
	Author  string
	Date    time.Time
	Message string
}

// New creates a new Git wrapper for the given repository path.
func New(repoPath string) *Git {
	return &Git{repoPath: repoPath}
}

// Init initializes a new git repository at repoPath.
// If the repo already exists, returns nil (no error).
func (g *Git) Init() error {
	if _, err := os.Stat(g.gitDir()); err == nil {
		// .git directory exists, already initialized
		return nil
	}
	return g.run("init")
}

// Add stages the given paths for commit.
func (g *Git) Add(paths ...string) error {
	args := append([]string{"add"}, paths...)
	return g.run(args...)
}

// AddAll stages all changes in the repository.
func (g *Git) AddAll() error {
	return g.run("add", "-A")
}

// Commit creates a commit with the given message.
func (g *Git) Commit(message string) error {
	return g.run("commit", "-m", message)
}

// Log returns the last n commit entries.
func (g *Git) Log(n int) ([]CommitInfo, error) {
	format := "--format=%H|%an|%aI|%s"
	args := []string{"log", format, "-n", fmt.Sprintf("%d", n)}

	cmd := exec.Command("git", args...)
	cmd.Dir = g.repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	return parseLogOutput(string(out))
}

// IsRepo returns true if repoPath is inside a git repository.
func (g *Git) IsRepo() bool {
	_, err := os.Stat(g.gitDir())
	return err == nil
}

// RepoPath returns the repository path.
func (g *Git) RepoPath() string {
	return g.repoPath
}

// run executes a git command with the given arguments.
func (g *Git) run(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// gitDir returns the path to the .git directory.
func (g *Git) gitDir() string {
	return g.repoPath + "/.git"
}

// parseLogOutput parses git log output into CommitInfo slices.
func parseLogOutput(output string) ([]CommitInfo, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	commits := make([]CommitInfo, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 {
			continue
		}

		date, err := time.Parse(time.RFC3339, parts[2])
		if err != nil {
			date = time.Time{}
		}

		commits = append(commits, CommitInfo{
			Hash:    parts[0],
			Author:  parts[1],
			Date:    date,
			Message: parts[3],
		})
	}

	return commits, nil
}
