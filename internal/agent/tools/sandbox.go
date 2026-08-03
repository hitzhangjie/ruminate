package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Sandbox restricts filesystem tools to configured roots.
type Sandbox struct {
	// Roots are absolute paths the agent may read.
	Roots []string
	// MaxReadBytes caps a single file_read / observation.
	MaxReadBytes int
}

// DefaultMaxReadBytes is the default per-read limit.
const DefaultMaxReadBytes = 64 * 1024

// NewSandbox creates a sandbox with the given roots (expanded to abs).
func NewSandbox(roots []string, maxReadBytes int) (*Sandbox, error) {
	if maxReadBytes <= 0 {
		maxReadBytes = DefaultMaxReadBytes
	}
	abs := make([]string, 0, len(roots))
	for _, r := range roots {
		if r == "" {
			continue
		}
		a, err := filepath.Abs(r)
		if err != nil {
			return nil, fmt.Errorf("resolving root %q: %w", r, err)
		}
		// Ensure directory exists (optional — code roots may be optional)
		if info, err := os.Stat(a); err == nil && !info.IsDir() {
			return nil, fmt.Errorf("root is not a directory: %s", a)
		}
		abs = append(abs, a)
	}
	if len(abs) == 0 {
		return nil, fmt.Errorf("agent sandbox requires at least one root")
	}
	return &Sandbox{Roots: abs, MaxReadBytes: maxReadBytes}, nil
}

// Resolve checks that path is under a root and returns the absolute path.
// path may be absolute or relative to a root (best-effort: try each root).
func (s *Sandbox) Resolve(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	// Clean and expand
	path = filepath.Clean(path)

	// Absolute path: must be under a root
	if filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		if !s.underRoot(abs) {
			return "", fmt.Errorf("path %q is outside agent roots", path)
		}
		return abs, nil
	}

	// Relative: try each root
	var candidates []string
	for _, root := range s.Roots {
		cand := filepath.Join(root, path)
		if s.underRoot(cand) {
			if _, err := os.Stat(cand); err == nil {
				return cand, nil
			}
			candidates = append(candidates, cand)
		}
	}
	// Also try path relative to CWD if under a root
	if abs, err := filepath.Abs(path); err == nil && s.underRoot(abs) {
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}
	if len(candidates) > 0 {
		// Return first candidate even if missing (caller may want the error)
		return candidates[0], nil
	}
	return "", fmt.Errorf("path %q not found under any agent root", path)
}

// underRoot reports whether absPath is within any configured root.
func (s *Sandbox) underRoot(absPath string) bool {
	absPath = filepath.Clean(absPath)
	for _, root := range s.Roots {
		root = filepath.Clean(root)
		if absPath == root {
			return true
		}
		prefix := root + string(os.PathSeparator)
		if strings.HasPrefix(absPath, prefix) {
			return true
		}
	}
	return false
}

// RelToRoot returns a path relative to the matching root, or the abs path.
func (s *Sandbox) RelToRoot(absPath string) string {
	absPath = filepath.Clean(absPath)
	for _, root := range s.Roots {
		if rel, err := filepath.Rel(root, absPath); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return absPath
}
