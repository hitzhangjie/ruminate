package serve

import (
	"fmt"
	"testing"

	"github.com/hitzhangjie/ruminate/internal/query"
)

func TestHub_ResolveMultiAndFixed(t *testing.T) {
	created := map[string]int{}
	factory := func(name string) (*query.Engine, error) {
		created[name]++
		// Engine requires real wiki; we only test hub gating — factory returns error is fine
		// for unknown paths. Use a stub via type: factory returns error so Resolve fails open?
		// Better: factory returns nil,error only for bad names; for good names we need *query.Engine.
		// Hub stores whatever factory returns — we can't construct Engine easily.
		// So test Catalog/Fixed/Multi without successful factory for open, and test Resolve
		// rejection paths.
		return nil, fmt.Errorf("not opened")
	}

	// Multi catalog — factory always fails; Resolve should still validate names first...
	// Actually factory is only called after catalog check.
	h, err := NewHub([]string{"a", "b"}, "a", "", factory)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Multi() || h.Fixed() != "" || h.Default() != "a" {
		t.Fatalf("hub multi state: multi=%v fixed=%q def=%q", h.Multi(), h.Fixed(), h.Default())
	}

	_, _, err = h.Resolve("c")
	if err == nil {
		t.Fatal("expected unknown wiki error")
	}

	fixed, err := NewHub([]string{"a", "b"}, "a", "b", factory)
	if err != nil {
		t.Fatal(err)
	}
	if fixed.Multi() || fixed.Fixed() != "b" {
		t.Fatalf("fixed hub: multi=%v fixed=%q", fixed.Multi(), fixed.Fixed())
	}
	if len(fixed.Catalog()) != 1 || fixed.Catalog()[0] != "b" {
		t.Fatalf("catalog = %v", fixed.Catalog())
	}
	_, _, err = fixed.Resolve("a")
	if err == nil {
		t.Fatal("expected locked error")
	}
}
