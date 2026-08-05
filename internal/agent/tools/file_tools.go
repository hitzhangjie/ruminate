package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ---- file_read ----

type fileReadTool struct {
	sb *Sandbox
}

func (t *fileReadTool) Schema() Schema {
	return Schema{
		Name:        "file_read",
		Description: "Read a file under agent roots with optional offset/limit (1-based line numbers).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":   map[string]any{"type": "string"},
				"offset": map[string]any{"type": "integer", "description": "1-based start line (default 1)"},
				"limit":  map[string]any{"type": "integer", "description": "Max lines to return (default 200)"},
			},
			"required": []string{"path"},
		},
	}
}

func (t *fileReadTool) Exec(ctx context.Context, args map[string]any) (string, error) {
	path := argString(args, "path")
	abs, err := t.sb.Resolve(path)
	if err != nil {
		return "", err
	}
	offset := argInt(args, "offset", 1)
	if offset < 1 {
		offset = 1
	}
	limit := argInt(args, "limit", 200)
	if limit <= 0 {
		limit = 200
	}

	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var b strings.Builder
	fmt.Fprintf(&b, "file: %s\n", t.sb.RelToRoot(abs))
	sc := bufio.NewScanner(f)
	// Allow long lines
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	lineNo := 0
	written := 0
	totalBytes := 0
	for sc.Scan() {
		lineNo++
		if lineNo < offset {
			continue
		}
		if written >= limit {
			fmt.Fprintf(&b, "…(more lines after %d)\n", lineNo-1)
			break
		}
		line := sc.Text()
		// Cap total bytes
		if totalBytes+len(line)+16 > t.sb.MaxReadBytes {
			b.WriteString("…[byte limit reached]\n")
			break
		}
		fmt.Fprintf(&b, "%6d|%s\n", lineNo, line)
		totalBytes += len(line) + 8
		written++
	}
	if err := sc.Err(); err != nil {
		return b.String(), err
	}
	if written == 0 {
		return b.String() + "(empty range or file)\n", nil
	}
	return b.String(), nil
}

// ---- file_grep ----

type fileGrepTool struct {
	sb *Sandbox
}

func (t *fileGrepTool) Schema() Schema {
	return Schema{
		Name:        "file_grep",
		Description: "Search for a regex/literal pattern under agent roots using rg (or built-in fallback). Returns path:line matches.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string"},
				"path":    map[string]any{"type": "string", "description": "Subpath within a root (optional)"},
				"glob":    map[string]any{"type": "string", "description": "File glob e.g. *.go"},
				"max":     map[string]any{"type": "integer", "description": "Max matches (default 40)"},
			},
			"required": []string{"pattern"},
		},
	}
}

func (t *fileGrepTool) Exec(ctx context.Context, args map[string]any) (string, error) {
	pattern := argString(args, "pattern")
	if pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	sub := argString(args, "path")
	glob := argString(args, "glob")
	max := argInt(args, "max", 40)
	if max <= 0 {
		max = 40
	}

	// Resolve search roots
	var searchRoots []string
	if sub != "" {
		abs, err := t.sb.Resolve(sub)
		if err != nil {
			return "", err
		}
		searchRoots = []string{abs}
	} else {
		searchRoots = t.sb.Roots
	}

	// Prefer rg
	if rgPath, err := exec.LookPath("rg"); err == nil {
		return t.rgSearch(ctx, rgPath, pattern, glob, max, searchRoots)
	}
	return t.builtinGrep(ctx, pattern, glob, max, searchRoots)
}

func (t *fileGrepTool) rgSearch(ctx context.Context, rg, pattern, glob string, max int, roots []string) (string, error) {
	args := []string{"--line-number", "--no-heading", "--color", "never", "-m", fmt.Sprintf("%d", max)}
	if glob != "" {
		args = append(args, "--glob", glob)
	}
	args = append(args, pattern)
	args = append(args, roots...)

	cmd := exec.CommandContext(ctx, rg, args...)
	out, err := cmd.CombinedOutput()
	// rg exit 1 = no matches
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return "No matches.", nil
		}
		// Still return partial output if any
		if len(out) == 0 {
			return "", fmt.Errorf("rg: %w", err)
		}
	}
	text := string(out)
	if strings.TrimSpace(text) == "" {
		return "No matches.", nil
	}
	// Relativize paths for readability
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String(), nil
}

func (t *fileGrepTool) builtinGrep(ctx context.Context, pattern, glob string, max int, roots []string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		// Fall back to literal
		re = regexp.MustCompile(regexp.QuoteMeta(pattern))
	}
	var matches []string
	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if glob != "" {
				ok, _ := filepath.Match(glob, filepath.Base(path))
				if !ok {
					return nil
				}
			}
			// Skip binary-ish / large
			if info.Size() > 2*1024*1024 {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			// Skip if contains NUL
			if strings.Contains(string(data), "\x00") {
				return nil
			}
			for i, line := range strings.Split(string(data), "\n") {
				if re.MatchString(line) {
					rel := t.sb.RelToRoot(path)
					matches = append(matches, fmt.Sprintf("%s:%d:%s", rel, i+1, line))
					if len(matches) >= max {
						return fmt.Errorf("done")
					}
				}
			}
			return nil
		})
		if len(matches) >= max {
			break
		}
	}
	if len(matches) == 0 {
		return "No matches.", nil
	}
	return strings.Join(matches, "\n"), nil
}

// ---- list_dir ----

type listDirTool struct {
	sb *Sandbox
}

func (t *listDirTool) Schema() Schema {
	return Schema{
		Name: "list_dir",
		Description: "List a directory under agent roots (shallow, limited entries). " +
			"Pass an absolute root path from the system prompt (or a subpath). " +
			"Omit path (or use \".\" / \"\") to list available agent roots — use this when unsure which paths are allowed.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Absolute path under an agent root, or relative to a root. Empty/\".\" lists roots.",
				},
				"limit": map[string]any{"type": "integer", "description": "Max entries (default 50)"},
			},
			// path is optional: empty → list roots (models often omit it otherwise).
		},
	}
}

func (t *listDirTool) Exec(ctx context.Context, args map[string]any) (string, error) {
	path := strings.TrimSpace(argString(args, "path"))
	// Empty / "." → catalog of roots so the model learns concrete paths
	// (CLI --agent-root is otherwise only enforced by the sandbox, not discoverable).
	if path == "" || path == "." {
		return t.listRoots(), nil
	}
	abs, err := t.sb.Resolve(path)
	if err != nil {
		// Help the model recover: include root list in the error observation path.
		return "", fmt.Errorf("%w\n\n%s", err, t.listRoots())
	}
	// If path resolved to a file, show parent listing hint via ReadDir error.
	limit := argInt(args, "limit", 50)
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", fmt.Errorf("%w\n\n%s", err, t.listRoots())
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s/\n", abs)
	n := 0
	for _, e := range entries {
		if n >= limit {
			fmt.Fprintf(&b, "…(%d more)\n", len(entries)-n)
			break
		}
		suffix := ""
		if e.IsDir() {
			suffix = "/"
		}
		fmt.Fprintf(&b, "  %s%s\n", e.Name(), suffix)
		n++
	}
	return b.String(), nil
}

// listRoots formats the sandbox allow-list for list_dir with no path.
func (t *listDirTool) listRoots() string {
	var b strings.Builder
	b.WriteString("Available agent roots (pass one as list_dir path, or use under file_grep/file_read):\n")
	if len(t.sb.Roots) == 0 {
		b.WriteString("  (none configured)\n")
		return b.String()
	}
	for i, r := range t.sb.Roots {
		fmt.Fprintf(&b, "%d. %s\n", i+1, r)
	}
	b.WriteString("Example: {\"action\":\"list_dir\",\"args\":{\"path\":\"")
	b.WriteString(t.sb.Roots[0])
	b.WriteString("\"}}\n")
	return b.String()
}

// RegisterFileTools registers file_read, file_grep, list_dir.
func RegisterFileTools(r *Registry, sb *Sandbox) {
	r.Register(&fileReadTool{sb: sb})
	r.Register(&fileGrepTool{sb: sb})
	r.Register(&listDirTool{sb: sb})
}
