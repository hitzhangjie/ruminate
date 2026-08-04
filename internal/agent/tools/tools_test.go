package tools

import (
	"context"
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
