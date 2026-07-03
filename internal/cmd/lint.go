package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

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
