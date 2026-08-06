package serve

import (
	"fmt"
	"strings"
	"sync"

	"github.com/hitzhangjie/ruminate/internal/query"
	"github.com/hitzhangjie/ruminate/internal/serve/handler"
)

// EngineFactory creates a query engine for a wiki name.
type EngineFactory func(wikiName string) (*query.Engine, error)

// Hub lazily opens query engines for one or more wikis.
//
// Modes:
//   - Fixed: FixedName != "" — only that wiki is allowed (ruminate serve --wiki X)
//   - Multi: FixedName == "" — any name in Catalog is allowed (serve without --wiki)
type Hub struct {
	mu       sync.Mutex
	engines  map[string]handler.QueryService
	factory  EngineFactory
	catalog  []string // registered wiki names
	defaultN string
	fixed    string // empty when multi-wiki
}

// NewHub constructs a Hub. catalog must be non-empty; defaultName must be in catalog.
// fixed is empty for multi-wiki mode, or equal to the only allowed wiki name.
func NewHub(catalog []string, defaultName, fixed string, factory EngineFactory) (*Hub, error) {
	if len(catalog) == 0 {
		return nil, fmt.Errorf("no wikis registered — run 'ruminate init' first")
	}
	if factory == nil {
		return nil, fmt.Errorf("engine factory is required")
	}
	if defaultName == "" {
		defaultName = catalog[0]
	}
	if !containsName(catalog, defaultName) {
		return nil, fmt.Errorf("default wiki %q is not in catalog %v", defaultName, catalog)
	}
	if fixed != "" {
		if !containsName(catalog, fixed) {
			return nil, fmt.Errorf("fixed wiki %q is not in catalog %v", fixed, catalog)
		}
		// Pin catalog to the fixed wiki only.
		catalog = []string{fixed}
		defaultName = fixed
	}
	return &Hub{
		engines:  make(map[string]handler.QueryService),
		factory:  factory,
		catalog:  append([]string(nil), catalog...),
		defaultN: defaultName,
		fixed:    fixed,
	}, nil
}

// Catalog returns registered wiki names (copy).
func (h *Hub) Catalog() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.catalog...)
}

// Default returns the default wiki name.
func (h *Hub) Default() string {
	return h.defaultN
}

// Fixed returns the pinned wiki name, or "" in multi-wiki mode.
func (h *Hub) Fixed() string {
	return h.fixed
}

// Multi reports whether clients may select among multiple wikis.
func (h *Hub) Multi() bool {
	return h.fixed == "" && len(h.catalog) > 1
}

// Resolve returns the engine for the requested wiki.
// Empty name resolves to the default. Fixed mode rejects any other name.
func (h *Hub) Resolve(name string) (handler.QueryService, string, error) {
	name = trimWiki(name)
	if name == "" {
		name = h.defaultN
	}

	if h.fixed != "" && name != h.fixed {
		return nil, "", fmt.Errorf("server is locked to wiki %q (started with --wiki)", h.fixed)
	}
	if !containsName(h.catalog, name) {
		return nil, "", fmt.Errorf("unknown wiki %q (known: %v)", name, h.catalog)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if eng, ok := h.engines[name]; ok {
		return eng, name, nil
	}
	eng, err := h.factory(name)
	if err != nil {
		return nil, "", err
	}
	h.engines[name] = eng
	return eng, name, nil
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func trimWiki(s string) string {
	return strings.TrimSpace(s)
}
