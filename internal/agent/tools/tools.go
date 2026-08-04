// Package tools implements the ReAct tool registry and built-in tools
// for knowledge exploration (wiki/raw/files/code). See docs/109.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Schema describes a tool for the LLM.
type Schema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON-schema-like object
}

// Tool is an executable agent tool.
type Tool interface {
	Schema() Schema
	Exec(ctx context.Context, args map[string]any) (string, error)
}

// Registry holds named tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool. Later registrations with the same name overwrite.
func (r *Registry) Register(t Tool) {
	r.tools[t.Schema().Name] = t
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Names returns sorted tool names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Schemas returns all tool schemas sorted by name.
func (r *Registry) Schemas() []Schema {
	names := r.Names()
	out := make([]Schema, 0, len(names))
	for _, n := range names {
		out = append(out, r.tools[n].Schema())
	}
	return out
}

// SchemaJSON returns a compact JSON description of all tools for the system prompt.
func (r *Registry) SchemaJSON() string {
	type paramDoc struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	}
	docs := make([]paramDoc, 0, len(r.tools))
	for _, s := range r.Schemas() {
		docs = append(docs, paramDoc{
			Name:        s.Name,
			Description: s.Description,
			Parameters:  s.Parameters,
		})
	}
	b, err := json.MarshalIndent(docs, "", "  ")
	if err != nil {
		return "[]"
	}
	return string(b)
}

// toolAliases maps common LLM hallucinations / synonyms onto registered names.
// Keys must be lowercase.
var toolAliases = map[string]string{
	"ls":           "list_dir",
	"listdir":      "list_dir",
	"list-dir":     "list_dir",
	"list_directory": "list_dir",
	"readdir":      "list_dir",
	"read_file":    "file_read",
	"readfile":     "file_read",
	"cat":          "file_read",
	"grep":         "file_grep",
	"search":       "wiki_search",
	"search_wiki":  "wiki_search",
	"read":         "wiki_read",
	"read_wiki":    "wiki_read",
	"index":        "wiki_index",
}

// ResolveName maps a model-supplied tool name onto a registered name.
//
// Tool-capable models (e.g. gpt-oss via native tool_calls) often invent
// namespaced names like "repo_browser.list_dir" or synonyms like "ls".
// Resolution order: exact → last dotted/slash segment → case-insensitive
// → alias table (on full name and on last segment).
//
// Returns the canonical registered name, or ("", false) if unknown.
func (r *Registry) ResolveName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	if _, ok := r.tools[name]; ok {
		return name, true
	}

	// Candidates derived from the raw name (keep order; first hit wins).
	candidates := []string{name}
	if base := lastPathSegment(name); base != "" && base != name {
		candidates = append(candidates, base)
	}

	// Case-insensitive match against registered tools.
	lowerIndex := make(map[string]string, len(r.tools))
	for n := range r.tools {
		lowerIndex[strings.ToLower(n)] = n
	}
	for _, c := range candidates {
		if canon, ok := lowerIndex[strings.ToLower(c)]; ok {
			return canon, true
		}
	}

	// Alias table.
	for _, c := range candidates {
		if alias, ok := toolAliases[strings.ToLower(c)]; ok {
			if _, ok := r.tools[alias]; ok {
				return alias, true
			}
		}
	}
	return "", false
}

// lastPathSegment returns the final component of a dotted or slashed name
// (e.g. "repo_browser.list_dir" → "list_dir", "tools/file_read" → "file_read").
func lastPathSegment(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	// Prefer the more specific separator if both appear; take the rightmost cut.
	iDot := strings.LastIndex(name, ".")
	iSlash := strings.LastIndex(name, "/")
	i := iDot
	if iSlash > i {
		i = iSlash
	}
	if i < 0 || i+1 >= len(name) {
		return name
	}
	return name[i+1:]
}

// Exec looks up and runs a tool, returning a truncated observation string.
// The name is resolved via ResolveName so namespaced/aliased calls still work.
func (r *Registry) Exec(ctx context.Context, name string, args map[string]any, maxBytes int) (string, error) {
	resolved, ok := r.ResolveName(name)
	if !ok {
		return "", fmt.Errorf("unknown tool %q (available: %s)", name, strings.Join(r.Names(), ", "))
	}
	t := r.tools[resolved]
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	obs, err := t.Exec(ctx, args)
	if err != nil {
		return "", err
	}
	if len(obs) > maxBytes {
		obs = obs[:maxBytes] + "\n…[observation truncated]"
	}
	return obs, nil
}

// argString extracts a string argument.
func argString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

// argInt extracts an int argument (accepts float64 from JSON).
func argInt(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	case string:
		var n int
		fmt.Sscanf(t, "%d", &n)
		if n != 0 {
			return n
		}
		return def
	default:
		return def
	}
}

// argBool extracts a bool argument.
func argBool(args map[string]any, key string, def bool) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1" || t == "yes"
	default:
		return def
	}
}
