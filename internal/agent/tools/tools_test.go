package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type stubTool struct {
	name string
}

func (t *stubTool) Schema() Schema {
	return Schema{Name: t.name, Description: "stub"}
}

func (t *stubTool) Exec(ctx context.Context, args map[string]any) (string, error) {
	return "ok:" + t.name, nil
}

func TestResolveName(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stubTool{name: "list_dir"})
	reg.Register(&stubTool{name: "file_read"})
	reg.Register(&stubTool{name: "wiki_search"})

	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"list_dir", "list_dir", true},
		{"repo_browser.list_dir", "list_dir", true},
		{"tools/list_dir", "list_dir", true},
		{"functions.file_read", "file_read", true},
		{"LIST_DIR", "list_dir", true},
		{"ls", "list_dir", true},
		{"read_file", "file_read", true},
		{"repo_browser.ls", "list_dir", true}, // last segment → alias
		{"totally_fake", "", false},
		{"repo_browser.nope", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := reg.ResolveName(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ResolveName(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestExecResolvesNamespacedName(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stubTool{name: "list_dir"})

	obs, err := reg.Exec(context.Background(), "repo_browser.list_dir", nil, 1024)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if obs != "ok:list_dir" {
		t.Errorf("obs = %q", obs)
	}
}

func TestListDir_EmptyPathListsRoots(t *testing.T) {
	dir := t.TempDir()
	extra := filepath.Join(dir, "code")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a marker file so a real listing is distinguishable.
	if err := os.WriteFile(filepath.Join(extra, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sb, err := NewSandbox([]string{dir, extra}, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	tool := &listDirTool{sb: sb}

	// Empty path → root catalog (models often omit path after --agent-root).
	obs, err := tool.Exec(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("empty path: %v", err)
	}
	if !strings.Contains(obs, "Available agent roots") {
		t.Errorf("expected roots catalog, got:\n%s", obs)
	}
	if !strings.Contains(obs, extra) {
		t.Errorf("expected extra root %q in catalog:\n%s", extra, obs)
	}

	// Explicit root path → directory listing.
	obs2, err := tool.Exec(context.Background(), map[string]any{"path": extra})
	if err != nil {
		t.Fatalf("list extra: %v", err)
	}
	if !strings.Contains(obs2, "main.go") {
		t.Errorf("expected main.go in listing:\n%s", obs2)
	}
}
