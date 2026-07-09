// Package lint provides the wiki health check engine.
//
// It detects common wiki issues across four dimensions:
//   - Broken links: WikiLink targets that don't resolve to any existing page
//   - Orphan pages: pages with zero incoming links (unreferenced or fully isolated)
//   - Stale content: pages not modified within a configurable threshold
//   - Contradictions: pages that share topics but may conflict (heuristic + optional LLM)
//
// The engine can run all checks or a subset, and produces a structured Report
// that the CLI or API can render as plain text or JSON.
package lint

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/llm"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// Severity classifies the importance of a lint finding.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Well-known check identifiers.
const (
	CheckOrphan        = "orphan"
	CheckBrokenLink    = "broken_link"
	CheckStaleness     = "staleness"
	CheckContradiction = "contradiction"
)

// AllChecks returns the ordered list of all registered check names.
func AllChecks() []string {
	return []string{CheckOrphan, CheckBrokenLink, CheckStaleness, CheckContradiction}
}

// Issue represents a single lint finding.
type Issue struct {
	Severity    Severity `json:"severity"`
	Check       string   `json:"check"`
	Title       string   `json:"title"`
	Detail      string   `json:"detail"`
	Page        string   `json:"page"`
	RelatedPage string   `json:"related_page,omitempty"`
}

// Report contains all lint findings and summary statistics.
type Report struct {
	Issues []Issue `json:"issues"`
	Stats  Stats   `json:"stats"`
}

// Stats holds summary statistics for a lint run.
type Stats struct {
	TotalPages  int `json:"total_pages"`
	TotalIssues int `json:"total_issues"`
	Errors      int `json:"errors"`
	Warnings    int `json:"warnings"`
	Infos       int `json:"infos"`
	Suppressed  int `json:"suppressed"`
}

// Options controls which checks to run and their parameters.
type Options struct {
	// Checks is the list of check identifiers to run.
	// Default (nil or empty): all checks.
	Checks []string

	// StalenessDays is the threshold in days for marking a page as potentially stale.
	StalenessDays int

	// UseLLM enables LLM-assisted contradiction detection for deeper analysis.
	// Ignored if no LLM provider is configured in the wiki manager.
	UseLLM bool

	// ContradictionMaxPagePairs caps the number of candidate page pairs sent to the
	// LLM for contradiction analysis. Higher values increase LLM cost and latency
	// but may find more contradictions. 0 means use the default (5).
	ContradictionMaxPagePairs int

	// ContradictionMaxPageChars is the maximum number of characters from each
	// page's content to include in the LLM prompt for contradiction checking.
	// Larger values give the LLM more context at the cost of higher token usage.
	// 0 means use the default (2000).
	ContradictionMaxPageChars int
}

const (
	DefaultStalenessDays             = 90
	DefaultContradictionMaxPagePairs = 10
	DefaultContradictionMaxPageChars = 512000
)

// DefaultOptions returns the default lint options.
func DefaultOptions() Options {
	return Options{
		Checks:                    AllChecks(),
		StalenessDays:             90,
		UseLLM:                    true,
		ContradictionMaxPagePairs: DefaultContradictionMaxPagePairs,
		ContradictionMaxPageChars: DefaultContradictionMaxPageChars,
	}
}

// Engine runs lint checks on a wiki.
type Engine struct {
	mgr    *wiki.Manager
	llmCfg config.LLMConfig
}

// New creates a new lint engine backed by the given wiki manager.
func New(mgr *wiki.Manager, llmCfg config.LLMConfig) *Engine {
	return &Engine{mgr: mgr, llmCfg: llmCfg}
}

// Run executes all configured lint checks and returns a report.
func (e *Engine) Run(opts Options) (*Report, error) {
	pages, err := e.mgr.List("")
	if err != nil {
		return nil, fmt.Errorf("listing pages: %w", err)
	}

	report := &Report{
		Stats: Stats{TotalPages: len(pages)},
	}

	if len(pages) == 0 {
		return report, nil
	}

	// Build shared lookup structures once.
	inlinks := buildInlinkMap(pages)
	linkIndex := buildLinkTargetIndex(pages)

	checkSet := make(map[string]bool, len(opts.Checks))
	for _, c := range opts.Checks {
		checkSet[c] = true
	}
	if len(checkSet) == 0 {
		// Default: run all checks if none specified.
		for _, c := range AllChecks() {
			checkSet[c] = true
		}
	}

	// Run each enabled check. Structurals run unconditionally; contradiction
	// uses heuristics and optionally an LLM deep pass.
	if checkSet[CheckOrphan] {
		report.Issues = append(report.Issues, e.checkOrphans(pages, inlinks)...)
	}
	if checkSet[CheckBrokenLink] {
		report.Issues = append(report.Issues, e.checkBrokenLinks(pages, linkIndex)...)
	}
	if checkSet[CheckStaleness] {
		if opts.StalenessDays <= 0 {
			opts.StalenessDays = 90
		}
		report.Issues = append(report.Issues, e.checkStaleness(pages, opts.StalenessDays)...)
	}
	if checkSet[CheckContradiction] {
		report.Issues = append(report.Issues, e.checkContradictions(pages, opts)...)
	}

	// Apply suppressions. Issues matching a suppression rule are removed
	// from the report but counted so the user knows filtering occurred.
	suppressions, _ := LoadSuppressions(e.mgr.Root())
	beforeFilter := len(report.Issues)
	report.Issues = FilterSuppressed(report.Issues, suppressions)
	report.Stats.Suppressed = beforeFilter - len(report.Issues)

	// Compute aggregate stats.
	report.Stats.TotalIssues = len(report.Issues)
	for _, iss := range report.Issues {
		switch iss.Severity {
		case SeverityError:
			report.Stats.Errors++
		case SeverityWarning:
			report.Stats.Warnings++
		case SeverityInfo:
			report.Stats.Infos++
		}
	}

	// Stable sort: errors → warnings → infos, then by check, then by page.
	sort.Slice(report.Issues, func(i, j int) bool {
		sevOrder := map[Severity]int{SeverityError: 0, SeverityWarning: 1, SeverityInfo: 2}
		if sevOrder[report.Issues[i].Severity] != sevOrder[report.Issues[j].Severity] {
			return sevOrder[report.Issues[i].Severity] < sevOrder[report.Issues[j].Severity]
		}
		if report.Issues[i].Check != report.Issues[j].Check {
			return report.Issues[i].Check < report.Issues[j].Check
		}
		return report.Issues[i].Page < report.Issues[j].Page
	})

	return report, nil
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// buildInlinkMap returns a map from page path → list of pages that link to it.
// A link from page A to page B is recognized when B's title, type:title, or
// relative path (without .md) appears in A's parsed WikiLinks.
func buildInlinkMap(pages []*wiki.Page) map[string][]string {
	// Reverse index: link target → which pages reference it.
	targetToPages := make(map[string][]string)
	for _, p := range pages {
		for _, link := range p.Links {
			targetToPages[link] = append(targetToPages[link], p.Path)
		}
	}

	inlinks := make(map[string][]string, len(pages))
	for _, p := range pages {
		candidates := []string{
			p.Title,
			fmt.Sprintf("%s:%s", p.Type, p.Title),
			strings.TrimSuffix(p.Path, ".md"),
		}
		seen := make(map[string]bool)
		for _, c := range candidates {
			for _, linker := range targetToPages[c] {
				if linker != p.Path && !seen[linker] {
					seen[linker] = true
					inlinks[p.Path] = append(inlinks[p.Path], linker)
				}
			}
		}
	}
	return inlinks
}

// buildLinkTargetIndex returns the set of all strings that can be used as a
// valid WikiLink target for the given pages.
func buildLinkTargetIndex(pages []*wiki.Page) map[string]bool {
	idx := make(map[string]bool, len(pages)*3)
	for _, p := range pages {
		idx[p.Title] = true
		idx[fmt.Sprintf("%s:%s", p.Type, p.Title)] = true
		idx[strings.TrimSuffix(p.Path, ".md")] = true
	}
	return idx
}

// ---------------------------------------------------------------------------
// Checks
// ---------------------------------------------------------------------------

// checkOrphans flags pages with zero incoming links.
// Pages with neither incoming nor outgoing links are "isolated" (warning);
// pages with outgoing but no incoming links are "unreferenced" (info).
func (e *Engine) checkOrphans(pages []*wiki.Page, inlinks map[string][]string) []Issue {
	var issues []Issue
	for _, p := range pages {
		inCount := len(inlinks[p.Path])
		outCount := len(p.Links)

		if inCount == 0 && outCount == 0 {
			issues = append(issues, Issue{
				Severity: SeverityWarning,
				Check:    CheckOrphan,
				Title:    fmt.Sprintf("Isolated page: %s", p.Title),
				Detail:   "Page has no incoming or outgoing links. It is disconnected from the rest of the wiki. Consider linking to it from related pages, or adding links from this page to others.",
				Page:     p.Path,
			})
		} else if inCount == 0 {
			issues = append(issues, Issue{
				Severity: SeverityInfo,
				Check:    CheckOrphan,
				Title:    fmt.Sprintf("Unreferenced page: %s", p.Title),
				Detail:   fmt.Sprintf("No other pages link to this page (it has %d outgoing links). Consider adding links from related pages.", outCount),
				Page:     p.Path,
			})
		}
	}
	return issues
}

// checkBrokenLinks finds WikiLinks whose target does not match any existing page.
// External URLs (containing ://) are skipped.
func (e *Engine) checkBrokenLinks(pages []*wiki.Page, linkIndex map[string]bool) []Issue {
	var issues []Issue
	for _, p := range pages {
		for _, link := range p.Links {
			if linkIndex[link] {
				continue
			}
			// Skip external URLs.
			if strings.Contains(link, "://") {
				continue
			}
			issues = append(issues, Issue{
				Severity:    SeverityError,
				Check:       CheckBrokenLink,
				Title:       fmt.Sprintf("Broken link in %s: [[%s]]", p.Title, link),
				Detail:      fmt.Sprintf("The link target %q does not match any existing page. Create the target page or fix the link.", link),
				Page:        p.Path,
				RelatedPage: link,
			})
		}
	}
	return issues
}

// checkStaleness flags pages whose file modification time is older than the
// given threshold in days.
func (e *Engine) checkStaleness(pages []*wiki.Page, thresholdDays int) []Issue {
	var issues []Issue
	cutoff := time.Now().AddDate(0, 0, -thresholdDays)

	for _, p := range pages {
		fullPath := filepath.Join(e.mgr.Root(), p.Path)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			days := int(time.Since(info.ModTime()).Hours() / 24)
			issues = append(issues, Issue{
				Severity: SeverityWarning,
				Check:    CheckStaleness,
				Title:    fmt.Sprintf("Potentially stale page: %s", p.Title),
				Detail:   fmt.Sprintf("Last modified %d days ago (threshold: %d days). The content may be outdated. Consider reviewing and updating this page.", days, thresholdDays),
				Page:     p.Path,
			})
		}
	}
	return issues
}

// contradictionCandidate is a pair of pages that share common outgoing links.
type contradictionCandidate struct {
	pageA, pageB *wiki.Page
	sharedLinks  []string
}

// checkContradictions looks for factual contradictions between pages.
//
// Strategy (in priority order):
//   1. LLM pass (preferred, enabled via Options.UseLLM and an available LLM):
//      Takes the top-N candidate pairs and asks the LLM to identify factual
//      contradictions. Findings are reported as "warning" severity.
//   2. Heuristic fallback (when LLM is unavailable):
//      Flags every pair of pages sharing at least one outgoing WikiLink as
//      "info" for manual review. Less precise than LLM, but ensures issues
//      are surfaced rather than silently ignored.
func (e *Engine) checkContradictions(pages []*wiki.Page, opts Options) []Issue {
	var candidates []contradictionCandidate

	// Build link → pages index to detect shared topics.
	linkToPages := make(map[string][]string)
	for _, p := range pages {
		for _, link := range p.Links {
			linkToPages[link] = append(linkToPages[link], p.Path)
		}
	}

	seenPairs := make(map[string]bool) // "pathA|pathB" → seen
	for _, paths := range linkToPages {
		if len(paths) < 2 {
			continue
		}
		for i := 0; i < len(paths); i++ {
			for j := i + 1; j < len(paths); j++ {
				a, b := paths[i], paths[j]
				if a > b {
					a, b = b, a
				}
				key := a + "|" + b
				if seenPairs[key] {
					continue
				}
				seenPairs[key] = true

				pa := findByPath(pages, a)
				pb := findByPath(pages, b)
				if pa == nil || pb == nil {
					continue
				}
				shared := sharedLinks(pa.Links, pb.Links)
				if len(shared) > 0 {
					candidates = append(candidates, contradictionCandidate{pa, pb, shared})
				}
			}
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// LLM deep check is preferred: only the LLM can identify actual factual
	// contradictions between pages.
	if opts.UseLLM && e.mgr.LLM() != nil {
		return e.llmContradictionCheck(candidates, opts)
	}

	// Fallback: when LLM is unavailable, flag every candidate pair as "info"
	// so a human can review for overlapping or conflicting claims.
	// Less valuable than the LLM check, but better than silently ignoring
	// potentially contradictory pages.
	var issues []Issue
	for _, c := range candidates {
		sort.Strings(c.sharedLinks)
		issues = append(issues, Issue{
			Severity:    SeverityInfo,
			Check:       CheckContradiction,
			Title:       fmt.Sprintf("Related pages: %s ↔ %s", c.pageA.Title, c.pageB.Title),
			Detail:      fmt.Sprintf("These pages share %d common link(s): %s. They may contain overlapping or contradictory information. Review to ensure consistency.", len(c.sharedLinks), strings.Join(c.sharedLinks, ", ")),
			Page:        c.pageA.Path,
			RelatedPage: c.pageB.Path,
		})
	}
	return issues
}

// llmContradictionCheck sends top candidate pairs to the LLM for factual
// contradiction analysis.
func (e *Engine) llmContradictionCheck(candidates []contradictionCandidate, opts Options) []Issue {
	// Cap to avoid excessive LLM calls.
	maxPairs := opts.ContradictionMaxPagePairs
	if maxPairs <= 0 {
		maxPairs = 5 // default
	}
	if len(candidates) > maxPairs {
		candidates = candidates[:maxPairs]
	}

	contentMaxChars := opts.ContradictionMaxPageChars
	if contentMaxChars <= 0 {
		contentMaxChars = 2000 // default
	}

	var issues []Issue
	llmClient := e.mgr.LLM()
	llmCfg := e.llmCfg

	systemPrompt := `You are a knowledge-base consistency checker. Compare the two wiki pages below and identify factual contradictions — statements that cannot both be true.

IMPORTANT: Some entities may share the same name but refer to completely different things in different contexts (polysemy / homonym). For example, "元宝" could be an ancient Chinese currency in one page and a pet cat in another — this is NOT a contradiction, because the name refers to different entities in different domains.

Analyze the pages and respond with one of the following for each finding:

If the same entity name refers to clearly different things in different contexts/domains (polysemy), respond:
- POLYSEMY: <brief explanation of why these are different entities despite sharing a name>

If there are factual contradictions (same entity, same context, conflicting facts that cannot both be true), respond:
- CONTRADICTION: <brief description of the conflict>

If there are no issues at all, respond with the single word: NONE.`

	for _, c := range candidates {
		userPrompt := fmt.Sprintf(
			"Page %q (%s):\n\n%s\n\n---\n\nPage %q (%s):\n\n%s\n\nShared topics (common links): %s",
			c.pageA.Title, c.pageA.Path,
			truncateContent(c.pageA.Content, contentMaxChars),
			c.pageB.Title, c.pageB.Path,
			truncateContent(c.pageB.Content, contentMaxChars),
			strings.Join(c.sharedLinks, ", "),
		)

		resp, err := llmClient.Chat(context.Background(), []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		}, &llm.ChatOptions{Temperature: llmCfg.Temperature})

		if err != nil || resp == nil {
			continue
		}

		content := strings.TrimSpace(resp.Content)
		if content == "" || content == "NONE" {
			continue
		}

		// Parse response lines.
		// - CONTRADICTION: factual conflict → warning
		// - POLYSEMY: same name, different entities → info
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if desc, ok := strings.CutPrefix(line, "- CONTRADICTION:"); ok {
				desc = strings.TrimSpace(desc)
				if desc != "" {
					issues = append(issues, Issue{
						Severity:    SeverityWarning,
						Check:       CheckContradiction,
						Title:       fmt.Sprintf("Contradiction between %s and %s", c.pageA.Title, c.pageB.Title),
						Detail:      desc,
						Page:        c.pageA.Path,
						RelatedPage: c.pageB.Path,
					})
				}
			}
			if desc, ok := strings.CutPrefix(line, "- POLYSEMY:"); ok {
				desc = strings.TrimSpace(desc)
				if desc != "" {
					issues = append(issues, Issue{
						Severity:    SeverityInfo,
						Check:       CheckContradiction,
						Title:       fmt.Sprintf("Polysemy: %s and %s share a term", c.pageA.Title, c.pageB.Title),
						Detail:      desc,
						Page:        c.pageA.Path,
						RelatedPage: c.pageB.Path,
					})
				}
			}
		}
	}

	return issues
}

// ---------------------------------------------------------------------------
// Pure helpers (no receiver needed)
// ---------------------------------------------------------------------------

// findByPath returns the page with the given relative path, or nil.
func findByPath(pages []*wiki.Page, path string) *wiki.Page {
	for _, p := range pages {
		if p.Path == path {
			return p
		}
	}
	return nil
}

// sharedLinks returns the intersection of two string slices.
func sharedLinks(a, b []string) []string {
	set := make(map[string]bool, len(a))
	for _, l := range a {
		set[l] = true
	}
	var shared []string
	for _, l := range b {
		if set[l] {
			shared = append(shared, l)
		}
	}
	return shared
}

// truncateContent truncates a string to maxLen characters, appending "…" if
// truncated.
func truncateContent(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
