// Package trace provides a lightweight span-based pipeline recorder.
//
// Usage:
//
//	tr := trace.New(enabled)
//	defer tr.Flush(os.Stderr)
//
//	tr.Begin("ask", "provider", "ollama")
//	tr.Begin("search", "query", "what is RAG?")
//	// ... do work ...
//	tr.End("results", 20)
//	tr.End("saved", false)
//
// When enabled, Flush writes a tree of timed spans with key=value attributes.
// When disabled, all methods are no-ops. Spans are JSON-serializable for web UI.
package trace

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Span represents a single timed step in the pipeline.
type Span struct {
	Name     string        `json:"name"`
	Attrs    []string      `json:"attrs,omitempty"` // key=value pairs, ordered
	Start    time.Time     `json:"start"`
	Elapsed  time.Duration `json:"elapsed"`
	Children []*Span       `json:"children,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// Tracer records a tree of timed spans.
// The zero value is a disabled tracer.
type Tracer struct {
	enabled bool
	root    *Span
	stack   []*Span // current nesting path; root is always at stack[0]
}

// New creates a new Tracer. When enabled is false, all methods are no-ops
// and Flush produces no output.
func New(enabled bool) *Tracer {
	if !enabled {
		return &Tracer{enabled: false}
	}
	root := &Span{Name: "pipeline", Start: time.Now()}
	return &Tracer{
		enabled: true,
		root:    root,
		stack:   []*Span{root},
	}
}

// Enabled returns true if the tracer is recording.
func (t *Tracer) Enabled() bool {
	if t == nil {
		return false
	}
	return t.enabled
}

// Begin starts a new span as a child of the current span.
// attrs is alternating key-value pairs: key1, val1, key2, val2, ...
func (t *Tracer) Begin(name string, attrs ...any) {
	if t == nil {
		return
	}
	if !t.enabled {
		return
	}
	s := &Span{
		Name:  name,
		Attrs: formatAttrs(attrs),
		Start: time.Now(),
	}
	parent := t.stack[len(t.stack)-1]
	parent.Children = append(parent.Children, s)
	t.stack = append(t.stack, s)
}

// End closes the current span. Optional attrs are appended.
func (t *Tracer) End(attrs ...any) {
	if t == nil {
		return
	}
	if !t.enabled {
		return
	}
	if len(t.stack) <= 1 {
		return // don't pop the root
	}
	cur := t.stack[len(t.stack)-1]
	cur.Elapsed = time.Since(cur.Start)
	if len(attrs) > 0 {
		cur.Attrs = append(cur.Attrs, formatAttrs(attrs)...)
	}
	t.stack = t.stack[:len(t.stack)-1]
}

// Error records an error on the current span (does not end the span).
func (t *Tracer) Error(err error) {
	if t == nil {
		return
	}
	if !t.enabled || err == nil {
		return
	}
	cur := t.stack[len(t.stack)-1]
	cur.Error = err.Error()
}

// Root returns the root span.
func (t *Tracer) Root() *Span {
	if t == nil {
		return nil
	}
	return t.root
}

// Flush finalizes the root span and writes the tree to w.
// If the tracer is disabled, this is a no-op.
func (t *Tracer) Flush(w io.Writer) {
	if t == nil {
		return
	}
	if !t.enabled {
		return
	}
	t.root.Elapsed = time.Since(t.root.Start)
	writeTree(w, t.root, "")
}

// formatAttrs converts alternating key-value pairs to "key=value" strings.
func formatAttrs(attrs []any) []string {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]string, 0, len(attrs)/2)
	for i := 0; i+1 < len(attrs); i += 2 {
		out = append(out, fmt.Sprintf("%v=%v", attrs[i], attrs[i+1]))
	}
	// Odd trailing value (shouldn't happen, but be tolerant)
	if len(attrs)%2 != 0 {
		out = append(out, fmt.Sprintf("_extra=%v", attrs[len(attrs)-1]))
	}
	return out
}

// writeTree writes the root span and its children as a tree.
func writeTree(w io.Writer, s *Span, prefix string) {
	connector := "── "
	attrStr := ""
	if len(s.Attrs) > 0 {
		attrStr = "  " + strings.Join(s.Attrs, " ")
	}
	duration := ""
	if s.Elapsed > 0 && s.Name != "pipeline" {
		duration = fmt.Sprintf("  %v", s.Elapsed.Round(time.Millisecond))
	}
	fmt.Fprintf(w, "%s%s%s%s%s\n", prefix, connector, s.Name, duration, attrStr)

	for i, child := range s.Children {
		isLast := i == len(s.Children)-1
		writeTreeNode(w, child, prefix, isLast)
	}
}

// writeTreeNode writes a span and its subtree.
// isLast indicates whether this node is the last child of its parent,
// which controls whether a vertical continuation line (│) is drawn.
func writeTreeNode(w io.Writer, s *Span, parentPrefix string, isLast bool) {
	connector := "├─ "
	childPrefix := parentPrefix + "│   "
	if isLast {
		connector = "└─ "
		childPrefix = parentPrefix + "    "
	}

	attrStr := ""
	if len(s.Attrs) > 0 {
		attrStr = "  " + strings.Join(s.Attrs, " ")
	}
	duration := fmt.Sprintf("  %v", s.Elapsed.Round(time.Millisecond))
	errStr := ""
	if s.Error != "" {
		errStr = fmt.Sprintf("  error=%q", s.Error)
	}
	fmt.Fprintf(w, "%s%s%s%s%s%s\n", parentPrefix, connector, s.Name, duration, attrStr, errStr)

	for i, child := range s.Children {
		childIsLast := i == len(s.Children)-1
		writeTreeNode(w, child, childPrefix, childIsLast)
	}
}
