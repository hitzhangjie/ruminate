// Package agentview renders ReAct agent progress in a Claude Code–style
// progressive terminal UI: spinner while deciding/running tools, compact
// cards when each step finishes. Falls back to plain text when not a TTY.
//
// See docs/111-agent-step-presentation.md.
package agentview

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	xterm "github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"

	"github.com/hitzhangjie/ruminate/internal/agent"
)

// Display width defaults when terminal size is unknown (non-TTY, pipes, tests).
const (
	defaultTermCols       = 120 // wider than classic 80; most modern terminals
	minDetailRunes        = 64  // primary-line detail floor (was hard-coded 48–56)
	maxDetailRunes        = 160 // cap so ultrawide terminals stay readable
	minSecondaryRunes     = 96
	maxSecondaryRunes     = 280
	chromePrimaryReserve  = 36 // "  ● " + tool name + " · " + "  12.3s"
	chromeSecondaryReserve = 8 // "    ⎿  "
)

// View is a live progressive renderer for agent.OnProgress / OnStep.
type View struct {
	w       io.Writer
	color   bool
	verbose bool

	mu   sync.Mutex
	spin *spinner

	// styles (set in New)
	sIconOK    lipgloss.Style
	sIconErr   lipgloss.Style
	sIconRun   lipgloss.Style
	sTool      lipgloss.Style
	sMeta      lipgloss.Style
	sErr       lipgloss.Style
	sThought   lipgloss.Style
	sDim       lipgloss.Style
	sHeader    lipgloss.Style
}

// Config controls View construction.
type Config struct {
	// Writer defaults to os.Stderr.
	Writer io.Writer
	// Color forces ANSI color; if nil, auto-detect from Writer + NO_COLOR.
	Color *bool
	// Verbose expands thought / observation after each card (like -v lite).
	// Full decide-prompt dumps stay in the plain text path (cmd package).
	Verbose bool
}

// New creates a View. Prefer NewDefault for CLI wiring.
func New(cfg Config) *View {
	w := cfg.Writer
	if w == nil {
		w = os.Stderr
	}
	color := false
	if cfg.Color != nil {
		color = *cfg.Color
	} else {
		color = shouldColor(w)
	}

	v := &View{w: w, color: color, verbose: cfg.Verbose}
	v.initStyles()
	return v
}

// NewDefault is the TTY live view used by `ask --agent` when stderr is a terminal.
func NewDefault() *View {
	return New(Config{Writer: os.Stderr})
}

// Close stops any in-flight spinner. Safe to call multiple times.
func (v *View) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.stopSpinnerLocked()
}

// OnProgress implements agent.Options.OnProgress.
func (v *View) OnProgress(p agent.Progress) {
	v.mu.Lock()
	defer v.mu.Unlock()

	label := progressLabelWidth(p, v.detailBudget())
	if isTerminalWriter(v.w) {
		v.startSpinnerLocked(label)
		return
	}
	// Non-TTY: static status line (no animation).
	fmt.Fprintf(v.w, "  · %s\n", label)
}

// OnStep implements agent.Options.OnStep.
func (v *View) OnStep(s agent.Step) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.stopSpinnerLocked()
	v.writeCardLocked(s)
}

func (v *View) initStyles() {
	if !v.color {
		v.sIconOK = lipgloss.NewStyle()
		v.sIconErr = lipgloss.NewStyle()
		v.sIconRun = lipgloss.NewStyle()
		v.sTool = lipgloss.NewStyle().Bold(true)
		v.sMeta = lipgloss.NewStyle()
		v.sErr = lipgloss.NewStyle()
		v.sThought = lipgloss.NewStyle()
		v.sDim = lipgloss.NewStyle()
		v.sHeader = lipgloss.NewStyle().Bold(true)
		return
	}
	// Palette close to common coding-agent TUIs: muted chrome, strong tool name.
	v.sIconOK = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))  // bright green
	v.sIconErr = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // bright red
	v.sIconRun = lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // bright blue
	v.sTool = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	v.sMeta = lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // gray
	v.sErr = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	v.sThought = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	v.sDim = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	v.sHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
}

func (v *View) writeCardLocked(s agent.Step) {
	errStep := isErrorStep(s)
	icon := "●"
	if errStep {
		icon = "✗"
	} else if s.Final {
		icon = "◆"
	}

	// Primary:  ● file_read · …/runtime/proc.go          2.1s
	// Paths keep the basename (suffix); width follows the terminal.
	budget := v.detailBudget()
	title, fullDetail, detailShortened := cardTitle(s, budget)
	dur := s.Duration.Round(time.Millisecond).String()
	metaRight := v.sMeta.Render(dur)

	var iconSt lipgloss.Style
	switch {
	case errStep:
		iconSt = v.sIconErr
	case s.Final:
		iconSt = v.sIconOK
	default:
		iconSt = v.sIconOK
	}

	primary := fmt.Sprintf("  %s %s", iconSt.Render(icon), v.sTool.Render(title))
	fmt.Fprintf(v.w, "%s  %s\n", primary, metaRight)

	// Secondary: ⎿  [fuller path if primary was shortened] · size · tok
	//            or error message
	secondary := cardSecondary(s, fullDetail, detailShortened, v.secondaryBudget())
	if secondary != "" {
		prefix := v.sDim.Render("    ⎿  ")
		if errStep {
			fmt.Fprintf(v.w, "%s%s\n", prefix, v.sErr.Render(secondary))
		} else {
			fmt.Fprintf(v.w, "%s%s\n", prefix, v.sMeta.Render(secondary))
		}
	}

	// Optional short thought (errors always; verbose always; success only if short)
	if thought := thoughtPreview(s, errStep || v.verbose, v.secondaryBudget()); thought != "" {
		fmt.Fprintf(v.w, "%s\n", v.sThought.Render("       "+thought))
	}

	// Verbose: dump observation preview under the card (still bounded).
	if v.verbose && !s.Final && s.Observation != "" && !errStep {
		obs := s.Observation
		if len(obs) > 600 {
			obs = obs[:600] + "…"
		}
		for _, line := range strings.Split(strings.TrimRight(obs, "\n"), "\n") {
			fmt.Fprintf(v.w, "%s\n", v.sDim.Render("       │ "+line))
		}
	}

	fmt.Fprintln(v.w)
}

// detailBudget is how many runes the primary-line action detail may use.
func (v *View) detailBudget() int {
	return clampRunes(terminalCols(v.w)-chromePrimaryReserve, minDetailRunes, maxDetailRunes)
}

// secondaryBudget is the rune budget for ⎿ lines (paths / errors).
func (v *View) secondaryBudget() int {
	return clampRunes(terminalCols(v.w)-chromeSecondaryReserve, minSecondaryRunes, maxSecondaryRunes)
}

func clampRunes(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// terminalCols returns the terminal width for w, or defaultTermCols.
func terminalCols(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return defaultTermCols
	}
	width, _, err := xterm.GetSize(f.Fd())
	if err != nil || width <= 0 {
		return defaultTermCols
	}
	return width
}

// cardTitle builds the primary label. fullDetail is the unshortened arg summary
// (may be shown on the secondary line when the primary had to shorten a path).
func cardTitle(s agent.Step, detailMax int) (title, fullDetail string, shortened bool) {
	switch {
	case s.Final:
		return "final_answer", "", false
	case s.Thought == "parse_error":
		return "parse_error", "", false
	case s.Action != "":
		fullDetail = formatActionDetail(s.Action, s.Args)
		if fullDetail == "" {
			return s.Action, "", false
		}
		shown := shortenDetail(fullDetail, detailMax)
		return s.Action + " · " + shown, fullDetail, shown != fullDetail
	default:
		return "step", "", false
	}
}

func cardSecondary(s agent.Step, fullDetail string, detailShortened bool, budget int) string {
	if isErrorStep(s) {
		msg := firstLine(s.Observation)
		msg = strings.TrimPrefix(msg, "ERROR: ")
		return shortenDetail(msg, budget)
	}
	if s.Final {
		parts := []string{"ready"}
		if t := tokenSuffix(s); t != "" {
			parts = append(parts, t)
		}
		return strings.Join(parts, " · ")
	}
	parts := []string{}
	// When the primary line hid the start of a long path, surface a wider
	// form here so the basename + more parent dirs stay visible.
	if detailShortened && fullDetail != "" {
		parts = append(parts, shortenDetail(fullDetail, budget))
	}
	if size := formatObsSize(s); size != "" {
		parts = append(parts, size)
	}
	if t := tokenSuffix(s); t != "" {
		parts = append(parts, t)
	}
	if len(parts) == 0 {
		return "done"
	}
	return strings.Join(parts, " · ")
}

func thoughtPreview(s agent.Step, show bool, budget int) string {
	if !show {
		return ""
	}
	if s.Thought == "" || s.Thought == "parse_error" {
		if s.ParseDumpPath != "" {
			return "dump: " + shortenDetail(s.ParseDumpPath, budget)
		}
		return ""
	}
	// Single line, collapsed whitespace.
	t := strings.Join(strings.Fields(s.Thought), " ")
	return truncateRunes(t, min(budget, 120))
}

func isErrorStep(s agent.Step) bool {
	return s.Thought == "parse_error" || strings.HasPrefix(s.Observation, "ERROR:")
}

func progressLabel(p agent.Progress) string {
	return progressLabelWidth(p, minDetailRunes)
}

func progressLabelWidth(p agent.Progress, detailMax int) string {
	switch p.Phase {
	case agent.ProgressTool:
		detail := formatActionDetail(p.Action, p.Args)
		if detail != "" {
			return fmt.Sprintf("%s · %s", p.Action, shortenDetail(detail, detailMax))
		}
		if p.Action != "" {
			return p.Action
		}
		return "running tool"
	default:
		return fmt.Sprintf("Thinking… (step %d)", p.Step)
	}
}

func tokenSuffix(s agent.Step) string {
	if s.PromptTokens > 0 || s.CompletionTokens > 0 {
		return fmt.Sprintf("%d→%d tok", s.PromptTokens, s.CompletionTokens)
	}
	if s.PromptChars > 0 {
		return fmt.Sprintf("~%d tok est", s.PromptChars/3)
	}
	return ""
}

func formatObsSize(s agent.Step) string {
	n := s.ObsBytes
	if n <= 0 {
		n = len(s.Observation)
	}
	if n <= 0 {
		return ""
	}
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fkB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= max-1 {
			b.WriteRune('…')
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// shortenDetail fits s into max runes for UI chrome.
// Path-like strings keep the **suffix** (basename / file:line) so long absolute
// paths still show the file name; free text keeps the prefix.
func shortenDetail(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 || runeLen(s) <= max {
		return s
	}
	if looksLikePath(s) {
		return "…" + lastRunes(s, max-1)
	}
	return truncateRunes(s, max)
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

func lastRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// looksLikePath reports whether s is better truncated from the left (keep end).
func looksLikePath(s string) bool {
	if s == "" {
		return false
	}
	// Quoted FTS queries are not paths even if they contain slashes rarely.
	if strings.HasPrefix(s, `"`) {
		return false
	}
	if strings.Contains(s, "/") || strings.Contains(s, `\`) {
		return true
	}
	// path:line (read_enclosing) without directory separators
	if i := strings.LastIndex(s, ":"); i > 0 && i < len(s)-1 {
		rest := s[i+1:]
		allDigit := true
		for _, r := range rest {
			if r < '0' || r > '9' {
				allDigit = false
				break
			}
		}
		if allDigit && (strings.Contains(s, ".") || filepath.Ext(s[:i]) != "") {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// formatActionDetail mirrors cmd.formatActionDetail for tool arg summaries.
// Kept local so agentview does not import cmd (import cycle).
func formatActionDetail(action string, args map[string]any) string {
	if args == nil {
		return ""
	}
	str := func(k string) string {
		v, ok := args[k]
		if !ok || v == nil {
			return ""
		}
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprint(v)
	}
	intv := func(k string) int {
		v, ok := args[k]
		if !ok || v == nil {
			return 0
		}
		switch t := v.(type) {
		case int:
			return t
		case int64:
			return int(t)
		case float64:
			return int(t)
		default:
			return 0
		}
	}
	switch action {
	case "wiki_search", "raw_search":
		q := str("query")
		n := intv("top_n")
		if n > 0 {
			return fmt.Sprintf("%q (top_n=%d)", q, n)
		}
		return fmt.Sprintf("%q", q)
	case "wiki_index":
		if f := str("filter"); f != "" {
			return fmt.Sprintf("filter=%q", f)
		}
		return "all"
	case "wiki_read", "raw_read", "file_read", "wiki_links", "list_dir", "ast_outline":
		return str("path")
	case "raw_list_sources":
		if p := str("path"); p != "" {
			return p
		}
		return "all"
	case "file_grep":
		pattern := str("pattern")
		glob := str("glob")
		if glob != "" {
			return fmt.Sprintf("%q (glob=%s)", pattern, glob)
		}
		return fmt.Sprintf("%q", pattern)
	case "symbol_search":
		return str("name")
	case "read_enclosing":
		path := str("path")
		line := intv("line")
		if line > 0 {
			return fmt.Sprintf("%s:%d", path, line)
		}
		return path
	default:
		return ""
	}
}

func shouldColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	return isTerminalWriter(w)
}

func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// UseLive reports whether the progressive live view should be the default
// for the given writer (TTY, not NO_COLOR-only concerns — color is separate).
func UseLive(w io.Writer) bool {
	return isTerminalWriter(w)
}
