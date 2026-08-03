package tools

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Code intelligence tools use go/ast for Go (P0). Same tool API can later
// be backed by tree-sitter grammars for other languages (task 7.8).
// Results are syntactic — not type-system guarantees (docs/109).

// ---- ast_outline ----

type astOutlineTool struct {
	sb *Sandbox
}

func (t *astOutlineTool) Schema() Schema {
	return Schema{
		Name:        "ast_outline",
		Description: "List top-level symbols (funcs/types/methods) in a Go source file. Syntactic only (go/ast).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Path to a .go file"},
			},
			"required": []string{"path"},
		},
	}
}

func (t *astOutlineTool) Exec(ctx context.Context, args map[string]any) (string, error) {
	path := argString(args, "path")
	abs, err := t.sb.Resolve(path)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(abs, ".go") {
		return "", fmt.Errorf("ast_outline currently supports Go files only (.go); got %s", filepath.Base(abs))
	}
	syms, err := parseGoSymbols(abs)
	if err != nil {
		return "", err
	}
	if len(syms) == 0 {
		return "No top-level symbols found.", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "outline: %s\n", t.sb.RelToRoot(abs))
	for _, s := range syms {
		fmt.Fprintf(&b, "  %s %s  L%d-L%d\n", s.Kind, s.Name, s.StartLine, s.EndLine)
	}
	return b.String(), nil
}

// ---- symbol_search ----

type symbolSearchTool struct {
	sb *Sandbox
}

func (t *symbolSearchTool) Schema() Schema {
	return Schema{
		Name:        "symbol_search",
		Description: "Find definition candidates for a symbol name under agent roots (Go via go/ast). May return multiple candidates — do not assume unique.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"path": map[string]any{"type": "string", "description": "Optional subpath to narrow search"},
				"max":  map[string]any{"type": "integer", "description": "Max candidates (default 20)"},
			},
			"required": []string{"name"},
		},
	}
}

func (t *symbolSearchTool) Exec(ctx context.Context, args map[string]any) (string, error) {
	name := argString(args, "name")
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	sub := argString(args, "path")
	max := argInt(args, "max", 20)
	if max <= 0 {
		max = 20
	}

	var roots []string
	if sub != "" {
		abs, err := t.sb.Resolve(sub)
		if err != nil {
			return "", err
		}
		roots = []string{abs}
	} else {
		roots = t.sb.Roots
	}

	var hits []goSymbol
	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				// Skip common noise dirs
				if info != nil && info.IsDir() {
					base := info.Name()
					if base == "vendor" || base == "node_modules" || base == ".git" || base == "testdata" {
						return filepath.SkipDir
					}
				}
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				// Still search test files? include them
				if !strings.HasSuffix(path, ".go") {
					return nil
				}
			}
			syms, err := parseGoSymbols(path)
			if err != nil {
				return nil
			}
			for _, s := range syms {
				if s.Name == name || strings.HasSuffix(s.Name, "."+name) {
					s.Path = path
					hits = append(hits, s)
					if len(hits) >= max {
						return fmt.Errorf("done")
					}
				}
			}
			return nil
		})
		if len(hits) >= max {
			break
		}
	}

	if len(hits) == 0 {
		return fmt.Sprintf("No definition candidates for %q (syntactic Go search only).", name), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "symbol_search %q — %d candidate(s) (syntactic, not type-checked):\n", name, len(hits))
	for i, h := range hits {
		fmt.Fprintf(&b, "%d. %s %s  %s:%d\n", i+1, h.Kind, h.Name, t.sb.RelToRoot(h.Path), h.StartLine)
	}
	return b.String(), nil
}

// ---- read_enclosing ----

type readEnclosingTool struct {
	sb *Sandbox
}

func (t *readEnclosingTool) Schema() Schema {
	return Schema{
		Name:        "read_enclosing",
		Description: "Given a file path and line number, return the enclosing function/method/type body (Go). Prefer this over reading whole files.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
				"line": map[string]any{"type": "integer", "description": "1-based line number"},
			},
			"required": []string{"path", "line"},
		},
	}
}

func (t *readEnclosingTool) Exec(ctx context.Context, args map[string]any) (string, error) {
	path := argString(args, "path")
	line := argInt(args, "line", 0)
	if path == "" || line < 1 {
		return "", fmt.Errorf("path and line (>=1) are required")
	}
	abs, err := t.sb.Resolve(path)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(abs, ".go") {
		return "", fmt.Errorf("read_enclosing currently supports Go files only")
	}

	src, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, abs, src, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}

	var best ast.Node
	var bestKind, bestName string
	var bestSpan int

	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		var kind, name string
		var start, end token.Pos
		switch x := n.(type) {
		case *ast.FuncDecl:
			kind = "func"
			name = x.Name.Name
			if x.Recv != nil && len(x.Recv.List) > 0 {
				name = recvString(x.Recv) + "." + name
				kind = "method"
			}
			start, end = x.Pos(), x.End()
		case *ast.GenDecl:
			if x.Tok != token.TYPE {
				return true
			}
			for _, spec := range x.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				s, e := ts.Pos(), ts.End()
				if posInLines(fset, s, e, line) {
					// Prefer type if line inside
					span := int(e - s)
					if best == nil || span < bestSpan {
						best = ts
						bestKind = "type"
						bestName = ts.Name.Name
						bestSpan = span
					}
				}
			}
			return true
		default:
			return true
		}
		if !posInLines(fset, start, end, line) {
			return true
		}
		span := int(end - start)
		if best == nil || span < bestSpan {
			best = n
			bestKind = kind
			bestName = name
			bestSpan = span
		}
		return true
	})

	if best == nil {
		return fmt.Sprintf("No enclosing function/type at %s:%d", t.sb.RelToRoot(abs), line), nil
	}

	start := fset.Position(best.Pos())
	end := fset.Position(best.End())
	// Extract source bytes for the node
	startOff := start.Offset
	endOff := end.Offset
	if startOff < 0 || endOff > len(src) || startOff >= endOff {
		return "", fmt.Errorf("invalid source range")
	}
	body := string(src[startOff:endOff])
	if len(body) > t.sb.MaxReadBytes {
		body = body[:t.sb.MaxReadBytes] + "\n…[truncated]"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s  %s:%d-%d  (syntactic enclosing)\n\n", bestKind, bestName, t.sb.RelToRoot(abs), start.Line, end.Line)
	b.WriteString(body)
	return b.String(), nil
}

// ---- helpers ----

type goSymbol struct {
	Kind      string
	Name      string
	Path      string
	StartLine int
	EndLine   int
}

func parseGoSymbols(path string) ([]goSymbol, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
	}
	var out []goSymbol
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			kind := "func"
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = recvString(d.Recv) + "." + name
				kind = "method"
			}
			start := fset.Position(d.Pos())
			end := fset.Position(d.End())
			out = append(out, goSymbol{Kind: kind, Name: name, Path: path, StartLine: start.Line, EndLine: end.Line})
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				start := fset.Position(ts.Pos())
				end := fset.Position(ts.End())
				out = append(out, goSymbol{Kind: "type", Name: ts.Name.Name, Path: path, StartLine: start.Line, EndLine: end.Line})
			}
		}
	}
	return out, nil
}

func recvString(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	f := fl.List[0]
	switch t := f.Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return "*" + id.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return "recv"
}

func posInLines(fset *token.FileSet, start, end token.Pos, line int) bool {
	s := fset.Position(start).Line
	e := fset.Position(end).Line
	return line >= s && line <= e
}

// RegisterCodeTools registers ast_outline, symbol_search, read_enclosing.
func RegisterCodeTools(r *Registry, sb *Sandbox) {
	r.Register(&astOutlineTool{sb: sb})
	r.Register(&symbolSearchTool{sb: sb})
	r.Register(&readEnclosingTool{sb: sb})
}
