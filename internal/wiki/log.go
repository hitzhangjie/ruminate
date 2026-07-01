package wiki

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// LogManager manages the structured log.md file.
// Each log entry follows the format:
//
//	## [日期] 操作类型 | 页面类型 | 标题
//
//	详细描述（可选）
type LogManager struct {
	logPath string
}

// LogEntry represents a single operation log entry.
type LogEntry struct {
	Date        time.Time
	Operation   string // create, update, delete
	PageType    PageType
	Title       string
	Description string
}

// NewLogManager creates a new LogManager.
func NewLogManager(logPath string) *LogManager {
	return &LogManager{logPath: logPath}
}

// Init initializes the log.md file with a header.
func (lm *LogManager) Init() error {
	content := `# Operations Log

> Chronological record of all write operations on the wiki.

`
	return os.WriteFile(lm.logPath, []byte(content), 0644)
}

// Append adds a new log entry to log.md.
// Each entry is prepended to the log (most recent first) after the header.
func (lm *LogManager) Append(operation string, pageType PageType, title string, description string) error {
	entry := lm.formatEntry(operation, pageType, title, description)

	// Read existing content
	content, err := os.ReadFile(lm.logPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Initialize and retry
			if err := lm.Init(); err != nil {
				return err
			}
			content = []byte("# Operations Log\n\n> Chronological record of all write operations on the wiki.\n\n")
		} else {
			return fmt.Errorf("reading log.md: %w", err)
		}
	}

	// Find the end of the header to insert new entry
	headerEnd := strings.Index(string(content), "\n\n")
	if headerEnd < 0 {
		headerEnd = 0
	} else {
		headerEnd += 2 // skip past "\n\n"
	}

	// Insert new entry after header (most recent first)
	newContent := string(content[:headerEnd]) + entry + "\n" + string(content[headerEnd:])

	return os.WriteFile(lm.logPath, []byte(newContent), 0644)
}

// ReadLog reads and parses the entire log.md.
func (lm *LogManager) ReadLog() ([]LogEntry, error) {
	content, err := os.ReadFile(lm.logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading log.md: %w", err)
	}

	return parseLogMd(string(content)), nil
}

// RecentEntries returns the last n log entries.
func (lm *LogManager) RecentEntries(n int) ([]LogEntry, error) {
	entries, err := lm.ReadLog()
	if err != nil {
		return nil, err
	}
	if len(entries) > n {
		entries = entries[:n]
	}
	return entries, nil
}

// LogPath returns the path to the log file.
func (lm *LogManager) LogPath() string {
	return lm.logPath
}

// formatEntry formats a log entry as Markdown.
func (lm *LogManager) formatEntry(operation string, pageType PageType, title string, description string) string {
	date := time.Now().Format("2006-01-02")
	heading := fmt.Sprintf("## [%s] %s | %s | %s", date, operation, pageType, title)

	if description != "" {
		return heading + "\n\n" + description + "\n"
	}
	return heading
}

// parseLogMd parses log.md content into LogEntry slices.
func parseLogMd(content string) []LogEntry {
	var entries []LogEntry
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "## [") {
			continue
		}

		// Parse: ## [2006-01-02] create | summaries | Page Title
		// Remove heading prefix
		rest := strings.TrimPrefix(line, "## [")

		// Extract date
		dateEnd := strings.Index(rest, "]")
		if dateEnd < 0 {
			continue
		}
		dateStr := rest[:dateEnd]
		rest = strings.TrimSpace(rest[dateEnd+1:])

		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			date = time.Time{}
		}

		// Split by |
		parts := strings.SplitN(rest, "|", 3)
		if len(parts) < 3 {
			continue
		}

		entries = append(entries, LogEntry{
			Date:      date,
			Operation: strings.TrimSpace(parts[0]),
			PageType:  PageType(strings.TrimSpace(parts[1])),
			Title:     strings.TrimSpace(parts[2]),
		})
	}

	return entries
}
