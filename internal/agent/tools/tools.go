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

// Exec looks up and runs a tool, returning a truncated observation string.
func (r *Registry) Exec(ctx context.Context, name string, args map[string]any, maxBytes int) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q (available: %s)", name, strings.Join(r.Names(), ", "))
	}
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
