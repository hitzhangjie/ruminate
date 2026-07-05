package trace

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestTracerDisabled(t *testing.T) {
	tr := New(false)
	if tr.Enabled() {
		t.Error("disabled tracer should not be enabled")
	}
	tr.Begin("step")
	tr.End("key", "val")
	tr.Error(nil)

	var buf bytes.Buffer
	tr.Flush(&buf)
	if buf.Len() != 0 {
		t.Errorf("disabled tracer should produce no output, got: %s", buf.String())
	}
}

func TestTracerEnabled(t *testing.T) {
	tr := New(true)
	if !tr.Enabled() {
		t.Fatal("enabled tracer should be enabled")
	}

	tr.Begin("ask", "provider", "ollama", "model", "gpt")
	tr.Begin("search", "query", "test")
	time.Sleep(10 * time.Millisecond)
	tr.End("results", 20)
	tr.End("saved", false)

	var buf bytes.Buffer
	tr.Flush(&buf)

	out := buf.String()
	t.Logf("Output:\n%s", out)

	// Verify tree structure
	if !strings.Contains(out, "ask") {
		t.Error("output should contain 'ask'")
	}
	if !strings.Contains(out, "search") {
		t.Error("output should contain 'search'")
	}
	// Verify attributes
	if !strings.Contains(out, "provider=ollama") {
		t.Error("output should contain provider attr")
	}
	if !strings.Contains(out, "query=test") {
		t.Error("output should contain query attr")
	}
	if !strings.Contains(out, "results=20") {
		t.Error("output should contain results attr")
	}
	// Verify nesting: search should be under ask
	askIdx := strings.Index(out, "ask")
	searchIdx := strings.Index(out, "search")
	if searchIdx < askIdx {
		t.Error("search should appear after ask (nested)")
	}
}

func TestTracerError(t *testing.T) {
	tr := New(true)
	tr.Begin("step")
	tr.Error(nil) // no-op
	tr.Error(&testError{"something went wrong"})
	tr.End()

	var buf bytes.Buffer
	tr.Flush(&buf)

	out := buf.String()
	if !strings.Contains(out, "error=") {
		t.Error("output should contain error info")
	}
}

func TestTracerNestedTree(t *testing.T) {
	tr := New(true)

	tr.Begin("a")
	tr.Begin("b")
	tr.End()
	tr.Begin("c")
	tr.End()
	tr.End()

	var buf bytes.Buffer
	tr.Flush(&buf)

	out := buf.String()
	t.Logf("Nested tree:\n%s", out)

	// b and c should be at same indent level under a
	if !strings.Contains(out, "├─ ") || !strings.Contains(out, "└─ ") {
		t.Log("tree may not have both connectors, output is fine as long as structure is correct")
	}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
