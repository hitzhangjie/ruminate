package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

// setupTestWiki creates a wiki manager backed by a temp directory, initialises
// it, and returns the manager along with a cleanup function.
func setupTestWiki(t *testing.T) (*wiki.Manager, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	cfg := &config.Config{WikiPath: tmpDir}
	mgr := wiki.NewManager(cfg)
	if err := mgr.Init(); err != nil {
		t.Fatalf("failed to init wiki: %v", err)
	}

	cleanup := func() {
		mgr.Close()
	}
	return mgr, cleanup
}

// createPage is a test helper that calls mgr.Create and fails the test on error.
func createPage(t *testing.T, mgr *wiki.Manager, title string, pt wiki.PageType, content string) *wiki.Page {
	t.Helper()
	p, err := mgr.Create(title, pt, content)
	if err != nil {
		t.Fatalf("failed to create page %q: %v", title, err)
	}
	return p
}

// ---------------------------------------------------------------------------
// lint problems detection
// ---------------------------------------------------------------------------

func TestCheckOrphans(t *testing.T) {
	t.Run("Isolated", func(t *testing.T) {
		mgr, cleanup := setupTestWiki(t)
		defer cleanup()

		// A page with no links in content and no other pages referencing it.
		createPage(t, mgr, "lonely-page", wiki.PageTypeConcept, "# Lonely Page\n\nNo links here.")

		eng := New(mgr)
		report, err := eng.Run(Options{Checks: []string{CheckOrphan}})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		found := false
		for _, iss := range report.Issues {
			if iss.Check == CheckOrphan && strings.Contains(iss.Title, "lonely-page") {
				found = true
				if iss.Severity != SeverityWarning {
					t.Errorf("expected warning severity for isolated page, got %s", iss.Severity)
				}
			}
		}
		if !found {
			t.Error("expected isolated page to be flagged")
		}
	})

	t.Run("Unreferenced", func(t *testing.T) {
		mgr, cleanup := setupTestWiki(t)
		defer cleanup()

		// A page that links to another but nothing links back to it.
		createPage(t, mgr, "target", wiki.PageTypeEntity, "# Target\n\nA target page.")
		createPage(t, mgr, "source", wiki.PageTypeConcept, "# Source\n\nSee [[target]].")

		eng := New(mgr)
		report, err := eng.Run(Options{Checks: []string{CheckOrphan}})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		// "target" has an incoming link → not orphaned.
		// "source" has an outgoing link but no incoming → unreferenced (info).
		foundSource := false
		for _, iss := range report.Issues {
			if iss.Check == CheckOrphan && strings.Contains(iss.Page, "source") {
				foundSource = true
				if iss.Severity != SeverityInfo {
					t.Errorf("expected info severity for unreferenced page, got %s", iss.Severity)
				}
			}
			if iss.Check == CheckOrphan && strings.Contains(iss.Page, "target") {
				t.Error("target page should not be flagged as orphan – it has an incoming link")
			}
		}
		if !foundSource {
			t.Error("expected source page to be flagged as unreferenced")
		}
	})

	t.Run("NoIssues", func(t *testing.T) {
		mgr, cleanup := setupTestWiki(t)
		defer cleanup()

		// Two pages that link to each other.
		createPage(t, mgr, "alpha", wiki.PageTypeConcept, "# Alpha\n\nSee [[beta]].")
		createPage(t, mgr, "beta", wiki.PageTypeConcept, "# Beta\n\nSee [[alpha]].")

		eng := New(mgr)
		report, err := eng.Run(Options{Checks: []string{CheckOrphan}})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		for _, iss := range report.Issues {
			if iss.Check == CheckOrphan {
				t.Errorf("unexpected orphan issue: %s", iss.Title)
			}
		}
	})
}

func TestCheckBrokenLinks(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		mgr, cleanup := setupTestWiki(t)
		defer cleanup()

		createPage(t, mgr, "real-page", wiki.PageTypeEntity, "# Real\n\nA real page.")
		createPage(t, mgr, "linker", wiki.PageTypeConcept,
			"# Linker\n\nLinks to [[real-page]] and [[ghost-page]] and [[entities:missing]].")

		eng := New(mgr)
		report, err := eng.Run(Options{Checks: []string{CheckBrokenLink}})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		brokenCount := 0
		for _, iss := range report.Issues {
			if iss.Check != CheckBrokenLink {
				continue
			}
			brokenCount++
			if iss.Severity != SeverityError {
				t.Errorf("expected error severity for broken link, got %s: %s", iss.Severity, iss.Title)
			}
		}
		if brokenCount != 2 {
			t.Errorf("expected 2 broken links, got %d", brokenCount)
		}
	})

	t.Run("SkipsExternalURLs", func(t *testing.T) {
		mgr, cleanup := setupTestWiki(t)
		defer cleanup()

		createPage(t, mgr, "linker", wiki.PageTypeConcept,
			"# Linker\n\nCheck https://example.com and [[real-page]].")
		createPage(t, mgr, "real-page", wiki.PageTypeEntity, "# Real\n\nA real page.")

		eng := New(mgr)
		report, err := eng.Run(Options{Checks: []string{CheckBrokenLink}})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		// The external URL https://example.com should not trigger a broken link.
		// But note: ParseWikiLinks only captures [[...]] patterns, so the URL is
		// never extracted as a link target anyway. This test just confirms no
		// false positives.
		for _, iss := range report.Issues {
			if iss.Check == CheckBrokenLink {
				t.Errorf("unexpected broken link: %s", iss.Title)
			}
		}
	})
}

func TestCheckStaleness(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		mgr, cleanup := setupTestWiki(t)
		defer cleanup()

		// Create a fresh page – should not be flagged.
		createPage(t, mgr, "fresh", wiki.PageTypeConcept, "# Fresh\n\nJust created.")

		// Create a page and backdate its mtime.
		stale := createPage(t, mgr, "stale", wiki.PageTypeConcept, "# Stale\n\nVery old.")
		fullPath := filepath.Join(mgr.Root(), stale.Path)
		oldTime := time.Now().AddDate(0, 0, -100) // 100 days ago
		if err := os.Chtimes(fullPath, oldTime, oldTime); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}

		eng := New(mgr)
		report, err := eng.Run(Options{
			Checks:        []string{CheckStaleness},
			StalenessDays: 90,
		})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		foundStale := false
		foundFresh := false
		for _, iss := range report.Issues {
			if iss.Check != CheckStaleness {
				continue
			}
			if strings.Contains(iss.Page, "stale") {
				foundStale = true
				if iss.Severity != SeverityWarning {
					t.Errorf("expected warning for stale page, got %s", iss.Severity)
				}
			}
			if strings.Contains(iss.Page, "fresh") {
				foundFresh = true
			}
		}
		if !foundStale {
			t.Error("expected stale page to be flagged")
		}
		if foundFresh {
			t.Error("fresh page should not be flagged as stale")
		}
	})

	t.Run("CustomThreshold", func(t *testing.T) {
		mgr, cleanup := setupTestWiki(t)
		defer cleanup()

		p := createPage(t, mgr, "kinda-old", wiki.PageTypeConcept, "# Kinda Old\n\nNot that old.")
		fullPath := filepath.Join(mgr.Root(), p.Path)
		oldTime := time.Now().AddDate(0, 0, -60) // 60 days ago
		if err := os.Chtimes(fullPath, oldTime, oldTime); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}

		// With a 90-day threshold the page should NOT be stale.
		eng := New(mgr)
		report, err := eng.Run(Options{
			Checks:        []string{CheckStaleness},
			StalenessDays: 90,
		})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		for _, iss := range report.Issues {
			if iss.Check == CheckStaleness {
				t.Errorf("page is 60 days old but threshold is 90 – should not be flagged: %s", iss.Title)
			}
		}

		// With a 30-day threshold it SHOULD be stale.
		report, err = eng.Run(Options{
			Checks:        []string{CheckStaleness},
			StalenessDays: 30,
		})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		found := false
		for _, iss := range report.Issues {
			if iss.Check == CheckStaleness && strings.Contains(iss.Page, "kinda-old") {
				found = true
			}
		}
		if !found {
			t.Error("page is 60 days old and threshold is 30 – should be flagged as stale")
		}
	})
}

func TestCheckContradictions(t *testing.T) {
	t.Run("HeuristicFallback", func(t *testing.T) {
		mgr, cleanup := setupTestWiki(t)
		defer cleanup()

		// Two pages that share a common outlink → without LLM, heuristic fallback
		// should flag them as info for manual review.
		createPage(t, mgr, "shared-target", wiki.PageTypeEntity, "# Shared\n\nA shared target.")
		createPage(t, mgr, "page-a", wiki.PageTypeConcept,
			"# Page A\n\nTalks about [[shared-target]] and says X is good.")
		createPage(t, mgr, "page-b", wiki.PageTypeConcept,
			"# Page B\n\nAlso talks about [[shared-target]] but says X is bad.")

		eng := New(mgr)
		report, err := eng.Run(Options{
			Checks:        []string{CheckContradiction},
			UseLLM:        false, // triggers heuristic fallback
			StalenessDays: 90,
		})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		found := false
		for _, iss := range report.Issues {
			if iss.Check == CheckContradiction && iss.Severity == SeverityInfo {
				if strings.Contains(iss.Title, "page-a") && strings.Contains(iss.Title, "page-b") {
					found = true
				}
			}
		}
		if !found {
			t.Error("expected heuristic fallback contradiction flag for pages sharing a common outlink")
		}
	})

	t.Run("NoSharedLinks", func(t *testing.T) {
		mgr, cleanup := setupTestWiki(t)
		defer cleanup()

		// Pages that do NOT share any common outlinks — no candidates, no LLM calls.
		createPage(t, mgr, "alpha-target", wiki.PageTypeEntity, "# Alpha Target\n\n.")
		createPage(t, mgr, "beta-target", wiki.PageTypeEntity, "# Beta Target\n\n.")
		createPage(t, mgr, "page-a", wiki.PageTypeConcept, "# Page A\n\nSee [[alpha-target]].")
		createPage(t, mgr, "page-b", wiki.PageTypeConcept, "# Page B\n\nSee [[beta-target]].")

		eng := New(mgr)
		report, err := eng.Run(Options{
			Checks:        []string{CheckContradiction},
			UseLLM:        false,
			StalenessDays: 90,
		})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		for _, iss := range report.Issues {
			if iss.Check == CheckContradiction {
				t.Errorf("unexpected contradiction flag when pages share no links: %s", iss.Title)
			}
		}
	})
}

// End-to-end: all checks together
func TestAllChecks(t *testing.T) {
	checks := AllChecks()
	if len(checks) != 4 {
		t.Fatalf("AllChecks() returned %d checks, want 4", len(checks))
	}

	expected := []string{CheckOrphan, CheckBrokenLink, CheckStaleness, CheckContradiction}
	for i, c := range expected {
		if checks[i] != c {
			t.Errorf("AllChecks()[%d] = %q, want %q", i, checks[i], c)
		}
	}
}

func TestRun_AllChecks(t *testing.T) {
	mgr, cleanup := setupTestWiki(t)
	defer cleanup()

	// Create a realistic wiki with several issues.

	// 1. An isolated page (no links in/out).
	createPage(t, mgr, "lonely", wiki.PageTypeConcept, "# Lonely\n\nNobody links here.")

	// 2. A broken link.
	createPage(t, mgr, "broken-linker", wiki.PageTypeConcept,
		"# Broken Linker\n\nSee [[nowhere]].")

	// 3. A stale page.
	stale := createPage(t, mgr, "ancient", wiki.PageTypeConcept, "# Ancient\n\nOld wisdom.")
	fullPath := filepath.Join(mgr.Root(), stale.Path)
	oldTime := time.Now().AddDate(0, 0, -120)
	if err := os.Chtimes(fullPath, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// 4. Pages with shared links (contradiction heuristic fallback, no LLM).
	createPage(t, mgr, "common", wiki.PageTypeEntity, "# Common\n\nCommon target.")
	createPage(t, mgr, "view-a", wiki.PageTypeConcept,
		"# View A\n\nRegarding [[common]], we believe P.")
	createPage(t, mgr, "view-b", wiki.PageTypeConcept,
		"# View B\n\nRegarding [[common]], we believe not-P.")

	// 5. A healthy, well-linked page (should generate no issues).
	createPage(t, mgr, "healthy", wiki.PageTypeSynthesis,
		"# Healthy\n\nSee [[view-a]] and [[view-b]].")

	eng := New(mgr)
	report, err := eng.Run(DefaultOptions())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Check stats.
	if report.Stats.TotalPages != 7 {
		t.Errorf("expected 7 total pages, got %d", report.Stats.TotalPages)
	}
	if report.Stats.TotalIssues == 0 {
		t.Error("expected issues but got none")
	}

	// Verify each check produced something.
	checksSeen := make(map[string]bool)
	for _, iss := range report.Issues {
		checksSeen[iss.Check] = true
	}
	for _, ck := range []string{CheckOrphan, CheckBrokenLink, CheckStaleness, CheckContradiction} {
		if !checksSeen[ck] {
			t.Errorf("expected issues from check %q but found none", ck)
		}
	}

	t.Logf("Report: %d issues (%d errors, %d warnings, %d info)",
		report.Stats.TotalIssues, report.Stats.Errors, report.Stats.Warnings, report.Stats.Infos)
}

// ---------------------------------------------------------------------------
// buildInlinkMap unit tests
// ---------------------------------------------------------------------------

func TestBuildInlinkMap(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		pages := []*wiki.Page{
			{
				Title:   "alpha",
				Path:    "wiki/concepts/alpha.md",
				Type:    wiki.PageTypeConcept,
				Content: "See [[beta]].",
				Links:   []string{"beta"},
			},
			{
				Title:   "beta",
				Path:    "wiki/concepts/beta.md",
				Type:    wiki.PageTypeConcept,
				Content: "See [[alpha]].",
				Links:   []string{"alpha"},
			},
			{
				Title:   "gamma",
				Path:    "wiki/concepts/gamma.md",
				Type:    wiki.PageTypeConcept,
				Content: "See [[alpha]].",
				Links:   []string{"alpha"},
			},
		}

		inlinks := buildInlinkMap(pages)

		if len(inlinks["wiki/concepts/alpha.md"]) != 2 {
			t.Errorf("alpha should have 2 inlink, got %d", len(inlinks["wiki/concepts/alpha.md"]))
		}
		if len(inlinks["wiki/concepts/beta.md"]) != 1 {
			t.Errorf("beta should have 1 inlink, got %d", len(inlinks["wiki/concepts/beta.md"]))
		}
	})

	t.Run("TypePrefix", func(t *testing.T) {
		pages := []*wiki.Page{
			{
				Title:   "target",
				Path:    "wiki/entities/target.md",
				Type:    wiki.PageTypeEntity,
				Content: "# Target",
				Links:   nil,
			},
			{
				Title:   "source",
				Path:    "wiki/concepts/source.md",
				Type:    wiki.PageTypeConcept,
				Content: "See [[entities:target]].",
				Links:   []string{"entities:target"},
			},
		}

		inlinks := buildInlinkMap(pages)

		if len(inlinks["wiki/entities/target.md"]) != 1 {
			t.Errorf("target should have 1 inlink (via type:title), got %d",
				len(inlinks["wiki/entities/target.md"]))
		}
	})
}

// ---------------------------------------------------------------------------
// buildLinkTargetIndex unit tests
// ---------------------------------------------------------------------------

func TestBuildLinkTargetIndex(t *testing.T) {
	pages := []*wiki.Page{
		{
			Title: "karpathy",
			Path:  "wiki/entities/karpathy.md",
			Type:  wiki.PageTypeEntity,
		},
	}

	idx := buildLinkTargetIndex(pages)

	// All three forms should be valid targets.
	if !idx["karpathy"] {
		t.Error("title should be a valid target")
	}
	if !idx["entities:karpathy"] {
		t.Error("type:title should be a valid target")
	}
	if !idx["wiki/entities/karpathy"] {
		t.Error("path (without .md) should be a valid target")
	}
	if idx["nonexistent"] {
		t.Error("unknown target should not be in index")
	}
}

// ---------------------------------------------------------------------------
// sharedLinks
// ---------------------------------------------------------------------------

func TestSharedLinks(t *testing.T) {
	tests := []struct {
		a, b   []string
		expect int
	}{
		{[]string{"a", "b", "c"}, []string{"b", "c", "d"}, 2},
		{[]string{"x"}, []string{"y"}, 0},
		{nil, []string{"a"}, 0},
		{[]string{}, []string{}, 0},
	}
	for _, tc := range tests {
		got := sharedLinks(tc.a, tc.b)
		if len(got) != tc.expect {
			t.Errorf("sharedLinks(%v, %v) = %d items, want %d",
				tc.a, tc.b, len(got), tc.expect)
		}
	}
}

// ---------------------------------------------------------------------------
// truncateContent
// ---------------------------------------------------------------------------

func TestTruncateContent(t *testing.T) {
	s := "hello world"
	if truncateContent(s, 100) != s {
		t.Error("short content should not be truncated")
	}
	if truncateContent(s, 5) != "hello…" {
		t.Error("content should be truncated with ellipsis")
	}
	if truncateContent("", 10) != "" {
		t.Error("empty content should remain empty")
	}
}

// ---------------------------------------------------------------------------
// DefaultOptions and AllChecks
// ---------------------------------------------------------------------------

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	if opts.StalenessDays != 90 {
		t.Errorf("default staleness = %d, want 90", opts.StalenessDays)
	}
	if !opts.UseLLM {
		t.Error("default UseLLM should be true")
	}
	if len(opts.Checks) != 4 {
		t.Errorf("default checks = %d, want 4", len(opts.Checks))
	}
}

func TestRun(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		mgr, cleanup := setupTestWiki(t)
		defer cleanup()

		eng := New(mgr)
		report, err := eng.Run(DefaultOptions())
		if err != nil {
			t.Fatalf("Run on empty wiki: %v", err)
		}
		if report.Stats.TotalPages != 0 {
			t.Errorf("expected 0 pages in empty wiki, got %d", report.Stats.TotalPages)
		}
		if len(report.Issues) != 0 {
			t.Errorf("expected 0 issues in empty wiki, got %d", len(report.Issues))
		}
	})

	t.Run("EmptyChecksDefaultsToAll", func(t *testing.T) {
		mgr, cleanup := setupTestWiki(t)
		defer cleanup()

		// A page with a broken link – should trigger even with empty Checks.
		createPage(t, mgr, "p", wiki.PageTypeConcept, "# P\n\nSee [[ghost]].")

		eng := New(mgr)
		report, err := eng.Run(Options{}) // empty Checks → defaults to all
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		foundBroken := false
		for _, iss := range report.Issues {
			if iss.Check == CheckBrokenLink {
				foundBroken = true
			}
		}
		if !foundBroken {
			t.Error("empty Checks should run all checks and find the broken link")
		}
	})

	// Verify issue sorting order
	t.Run("IssueSorting", func(t *testing.T) {
		mgr, cleanup := setupTestWiki(t)
		defer cleanup()

		// Create a page with a broken link (error) and make it stale (warning).
		// Also create an isolated page (warning).
		p := createPage(t, mgr, "bad-page", wiki.PageTypeConcept,
			"# Bad Page\n\nSee [[missing-link]].")
		fullPath := filepath.Join(mgr.Root(), p.Path)
		oldTime := time.Now().AddDate(0, 0, -100)
		if err := os.Chtimes(fullPath, oldTime, oldTime); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}

		createPage(t, mgr, "lonely", wiki.PageTypeConcept, "# Lonely\n\nNo links.")
		createPage(t, mgr, "shared", wiki.PageTypeEntity, "# Shared\n\n.")
		createPage(t, mgr, "p1", wiki.PageTypeConcept, "# P1\n\nSee [[shared]].")
		createPage(t, mgr, "p2", wiki.PageTypeConcept, "# P2\n\nSee [[shared]].")

		eng := New(mgr)
		report, err := eng.Run(DefaultOptions())
		if err != nil {
			t.Fatalf("Run: %v", err)
		}

		// Verify errors come before warnings, warnings before info.
		lastSev := SeverityError // start with most severe = earliest in sort
		for _, iss := range report.Issues {
			curSev := iss.Severity
			sevOrder := map[Severity]int{SeverityError: 0, SeverityWarning: 1, SeverityInfo: 2}
			if sevOrder[curSev] < sevOrder[lastSev] {
				t.Errorf("sort order violated: %s after %s\n  issue: %s",
					curSev, lastSev, iss.Title)
			}
			lastSev = curSev
		}
	})
}

// ---------------------------------------------------------------------------
// Benchmark
// ---------------------------------------------------------------------------

func BenchmarkRun(b *testing.B) {
	// Create a wiki with many pages.
	tmpDir := b.TempDir()
	cfg := &config.Config{WikiPath: tmpDir}
	mgr := wiki.NewManager(cfg)
	if err := mgr.Init(); err != nil {
		b.Fatalf("init: %v", err)
	}
	defer mgr.Close()

	for i := 0; i < 100; i++ {
		title := fmt.Sprintf("page-%d", i)
		content := fmt.Sprintf("# %s\n\nSee [[page-%d]] and [[missing-%d]].", title, (i+1)%100, i)
		_, err := mgr.Create(title, wiki.PageTypeConcept, content)
		if err != nil {
			b.Fatalf("create %s: %v", title, err)
		}
	}

	eng := New(mgr)
	opts := DefaultOptions()
	opts.UseLLM = false // no LLM in benchmarks

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := eng.Run(opts)
		if err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}
