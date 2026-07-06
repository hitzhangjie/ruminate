package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/lint"
	"github.com/hitzhangjie/ruminate/internal/wiki"
)

var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Run health checks on the wiki",
	Long: `Analyze the wiki for issues such as:
  - Content contradictions
  - Orphaned pages
  - Stale content
  - Broken or missing links

Each issue is classified by severity: error, warning, or info.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		mgr := wiki.NewManager(cfg)
		defer mgr.Close()

		if !mgr.IsInitialized() {
			return fmt.Errorf("wiki not initialized at %s — run 'ruminate init' first", mgr.Root())
		}

		// Build options from flags.
		opts := lint.DefaultOptions()
		if checks, _ := cmd.Flags().GetStringSlice("check"); len(checks) > 0 {
			opts.Checks = checks
		}
		if cmd.Flags().Changed("staleness-days") {
			opts.StalenessDays, _ = cmd.Flags().GetInt("staleness-days")
		}
		if noLLM, _ := cmd.Flags().GetBool("no-llm"); noLLM {
			opts.UseLLM = false
		}
		if cmd.Flags().Changed("max-llm-pairs") {
			opts.ContradictionMaxPagePairs, _ = cmd.Flags().GetInt("max-llm-pairs")
		}
		if cmd.Flags().Changed("max-llm-chars") {
			opts.ContradictionMaxPageChars, _ = cmd.Flags().GetInt("max-llm-chars")
		}

		engine := lint.New(mgr)
		report, err := engine.Run(opts)
		if err != nil {
			return fmt.Errorf("running lint: %w", err)
		}

		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(report)
		}

		printLintReport(report)
		return nil
	},
}

func init() {
	lintCmd.Flags().StringSlice("check", nil, "Checks to run: orphan, broken_link, staleness, contradiction (default: all)")
	lintCmd.Flags().Int("staleness-days", lint.DefaultStalenessDays, "Days threshold for stale content detection")
	lintCmd.Flags().Bool("no-llm", false, "Skip LLM-assisted contradiction detection")
	lintCmd.Flags().Int("max-llm-pairs", lint.DefaultContradictionMaxPagePairs, "Max candidate page pairs sent to LLM for contradiction analysis")
	lintCmd.Flags().Int("max-llm-chars", lint.DefaultContradictionMaxPageChars, "Max chars of page content per page in contradiction LLM prompt")
	lintCmd.Flags().Bool("json", false, "Output report as JSON")

	// Register subcommands.
	lintCmd.AddCommand(lintSuppressCmd)
	lintCmd.AddCommand(lintSuppressionsCmd)
}

// lintSuppressCmd adds a suppression rule to exclude a lint issue from future reports.
var lintSuppressCmd = &cobra.Command{
	Use:   "suppress",
	Short: "Suppress a lint issue so it is hidden from future reports",
	Long: `Add a suppression rule to hide a specific lint issue from future reports.

Use this when you've reviewed an issue and determined it's not a real problem —
for example, when two pages use the same term for different entities (polysemy).

Suppressions are stored in .ruminate/lint-suppressions.json and can be
edited manually or removed by editing the file.`,
	Example: `  ruminate lint suppress --page wiki/summaries/xxx.md --related wiki/summaries/yyy.md --reason "一词多义：元宝指代不同实体"
  ruminate lint suppress --check broken_link --page wiki/entities/old.md --reason "Known issue, will fix later"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		page, _ := cmd.Flags().GetString("page")
		related, _ := cmd.Flags().GetString("related")
		check, _ := cmd.Flags().GetString("check")
		reason, _ := cmd.Flags().GetString("reason")

		if page == "" {
			return fmt.Errorf("--page is required")
		}
		if reason == "" {
			return fmt.Errorf("--reason is required")
		}

		wikiRoot := config.ExpandPath(cfg.WikiPath)
		sf, err := lint.LoadSuppressions(wikiRoot)
		if err != nil {
			return fmt.Errorf("loading suppressions: %w", err)
		}

		if err := sf.Add(check, page, related, reason); err != nil {
			return fmt.Errorf("adding suppression: %w", err)
		}

		fmt.Printf("✓ Suppression added: %s\n", reason)
		if related != "" {
			fmt.Printf("  Check: %s, Page: %s ↔ %s\n", check, page, related)
		} else {
			fmt.Printf("  Check: %s, Page: %s\n", check, page)
		}
		return nil
	},
}

// lintSuppressionsCmd lists all active suppression rules.
var lintSuppressionsCmd = &cobra.Command{
	Use:   "suppressions",
	Short: "List all active suppression rules",
	Long:  `Display all lint issue suppression rules currently in effect.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		wikiRoot := config.ExpandPath(cfg.WikiPath)
		sf, err := lint.LoadSuppressions(wikiRoot)
		if err != nil {
			return fmt.Errorf("loading suppressions: %w", err)
		}

		list := sf.List()
		if len(list) == 0 {
			fmt.Println("No active suppression rules.")
			return nil
		}

		fmt.Printf("Suppression rules (%d):\n\n", len(list))
		for _, s := range list {
			fmt.Printf("  [%s]\n", s.ID[:8])
			fmt.Printf("  Check:   %s\n", s.Check)
			fmt.Printf("  Page:    %s\n", s.Page)
			if s.RelatedPage != "" {
				fmt.Printf("  Related: %s\n", s.RelatedPage)
			}
			fmt.Printf("  Reason:  %s\n", s.Reason)
			fmt.Printf("  Created: %s\n", s.CreatedAt)
			fmt.Println()
		}
		return nil
	},
}

func init() {
	lintSuppressCmd.Flags().String("page", "", "Primary page path (required)")
	lintSuppressCmd.Flags().String("related", "", "Related page path (for pair-based issues like contradictions)")
	lintSuppressCmd.Flags().String("check", "contradiction", "Check type: contradiction, broken_link, staleness, orphan")
	lintSuppressCmd.Flags().String("reason", "", "Reason for suppression (required)")
}

// printLintReport prints a human-readable lint report to stdout.
func printLintReport(report *lint.Report) {
	fmt.Println()
	fmt.Println("══ Wiki Health Check Report ══")
	fmt.Println()

	// Summary line.
	fmt.Printf("  Pages scanned: %d\n", report.Stats.TotalPages)
	fmt.Printf("  Issues found:  %d", report.Stats.TotalIssues)
	if report.Stats.TotalIssues > 0 {
		fmt.Printf(" (%d errors, %d warnings, %d info)",
			report.Stats.Errors, report.Stats.Warnings, report.Stats.Infos)
	}
	fmt.Println()
	if report.Stats.Suppressed > 0 {
		fmt.Printf("  Suppressed:     %d issue(s) hidden by suppression rules\n", report.Stats.Suppressed)
	}
	fmt.Println()

	if len(report.Issues) == 0 {
		fmt.Println("  ✓ No issues found — your wiki looks healthy!")
		fmt.Println()
		return
	}

	// Group issues by check.
	currentCheck := ""
	for _, issue := range report.Issues {
		if issue.Check != currentCheck {
			currentCheck = issue.Check
			fmt.Printf("── %s ──\n", checkLabel(currentCheck))
			fmt.Println()
		}

		icon := severityIcon(issue.Severity)
		fmt.Printf("  %s %s\n", icon, issue.Title)
		fmt.Printf("    Page: %s\n", issue.Page)
		if issue.RelatedPage != "" {
			fmt.Printf("    Related: %s\n", issue.RelatedPage)
		}
		for _, line := range wrapLines(issue.Detail, 72) {
			fmt.Printf("    %s\n", line)
		}
		fmt.Println()
	}
}

func severityIcon(s lint.Severity) string {
	switch s {
	case lint.SeverityError:
		return "✗"
	case lint.SeverityWarning:
		return "⚠"
	case lint.SeverityInfo:
		return "ℹ"
	default:
		return "•"
	}
}

func checkLabel(check string) string {
	switch check {
	case lint.CheckOrphan:
		return "Orphaned & Unreferenced Pages"
	case lint.CheckBrokenLink:
		return "Broken Links"
	case lint.CheckStaleness:
		return "Stale Content"
	case lint.CheckContradiction:
		return "Potential Contradictions"
	default:
		return check
	}
}

// wrapLines wraps text to a maximum width, breaking on word boundaries.
func wrapLines(text string, width int) []string {
	if len(text) <= width {
		return []string{text}
	}
	var lines []string
	words := strings.Fields(text)
	var current string
	for _, w := range words {
		if len(current)+len(w)+1 > width {
			if current != "" {
				lines = append(lines, current)
				current = ""
			}
		}
		if current == "" {
			current = w
		} else {
			current += " " + w
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
