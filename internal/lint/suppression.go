// Package lint provides suppression management for lint issues.
//
// Suppressions allow users to mark specific issues as "not a problem" so they
// are excluded from future lint reports. This is particularly useful for:
//   - Polysemy: same term referring to different entities in different contexts
//   - Acknowledged issues: known problems that are deferred or won't fix
//
// Suppressions are stored in .ruminate/lint-suppressions.json within the wiki
// root directory. The file is JSON for easy programmatic and manual editing.
package lint

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Suppression represents a single lint issue suppression rule.
type Suppression struct {
	// ID is a stable identifier derived from the check and page pair.
	ID string `json:"id"`

	// Check identifies which lint check this suppression applies to
	// (e.g., "contradiction", "broken_link").
	Check string `json:"check"`

	// Page is the primary page path involved in the suppressed issue.
	Page string `json:"page"`

	// RelatedPage is the secondary page path, if applicable.
	// For contradiction issues, this is the other page in the pair.
	RelatedPage string `json:"related_page,omitempty"`

	// Reason explains why this issue is suppressed.
	Reason string `json:"reason"`

	// CreatedAt is the timestamp when this suppression was created.
	CreatedAt string `json:"created_at"`
}

// SuppressionFile manages the collection of suppression rules persisted to disk.
type SuppressionFile struct {
	// Version is the schema version for forward compatibility.
	Version int `json:"version"`

	// Suppressions is the ordered list of suppression rules.
	Suppressions []Suppression `json:"suppressions"`

	path string // absolute path to the JSON file on disk
}

// suppressionFilePath returns the absolute path to the suppressions file
// within the given wiki root directory.
func suppressionFilePath(wikiRoot string) string {
	return filepath.Join(wikiRoot, ".ruminate", "lint-suppressions.json")
}

// LoadSuppressions loads suppression rules from disk.
// Returns an empty SuppressionFile if the file does not exist.
func LoadSuppressions(wikiRoot string) (*SuppressionFile, error) {
	path := suppressionFilePath(wikiRoot)
	sf := &SuppressionFile{
		Version:      1,
		Suppressions: []Suppression{},
		path:         path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return sf, nil // no suppressions yet
		}
		return nil, fmt.Errorf("reading suppressions file: %w", err)
	}

	if err := json.Unmarshal(data, sf); err != nil {
		return nil, fmt.Errorf("parsing suppressions file: %w", err)
	}

	if sf.Version == 0 {
		sf.Version = 1
	}
	if sf.Suppressions == nil {
		sf.Suppressions = []Suppression{}
	}
	sf.path = path
	return sf, nil
}

// Save writes the suppression rules to disk, creating the file if needed.
func (sf *SuppressionFile) Save() error {
	// Ensure the parent directory exists.
	if err := os.MkdirAll(filepath.Dir(sf.path), 0755); err != nil {
		return fmt.Errorf("creating suppressions directory: %w", err)
	}

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling suppressions: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(sf.path, data, 0644); err != nil {
		return fmt.Errorf("writing suppressions file: %w", err)
	}
	return nil
}

// Add adds a new suppression rule and saves to disk.
// The ID is computed deterministically from check + page pair so that
// re-adding the same rule is idempotent.
func (sf *SuppressionFile) Add(check, page, relatedPage, reason string) error {
	id := suppressionID(check, page, relatedPage)

	// Don't add duplicates.
	for _, s := range sf.Suppressions {
		if s.ID == id {
			return nil // idempotent
		}
	}

	sf.Suppressions = append(sf.Suppressions, Suppression{
		ID:          id,
		Check:       check,
		Page:        page,
		RelatedPage: relatedPage,
		Reason:      reason,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	})

	// Keep list sorted by created time for deterministic output.
	sort.Slice(sf.Suppressions, func(i, j int) bool {
		return sf.Suppressions[i].CreatedAt < sf.Suppressions[j].CreatedAt
	})

	return sf.Save()
}

// Remove removes a suppression rule by ID and saves to disk.
// Returns an error if the ID is not found.
func (sf *SuppressionFile) Remove(id string) error {
	for i, s := range sf.Suppressions {
		if s.ID == id {
			sf.Suppressions = append(sf.Suppressions[:i], sf.Suppressions[i+1:]...)
			return sf.Save()
		}
	}
	return fmt.Errorf("suppression %q not found", id)
}

// List returns a copy of all suppression rules.
func (sf *SuppressionFile) List() []Suppression {
	result := make([]Suppression, len(sf.Suppressions))
	copy(result, sf.Suppressions)
	return result
}

// IsSuppressed checks whether an issue matches any suppression rule.
// For page pairs, matching is order-independent: (A, B) matches both
// suppressions for (A, B) and (B, A).
func (sf *SuppressionFile) IsSuppressed(issue Issue) bool {
	for _, s := range sf.Suppressions {
		if s.Check != issue.Check {
			continue
		}
		// Order-independent page pair matching.
		if (s.Page == issue.Page && s.RelatedPage == issue.RelatedPage) ||
			(s.Page == issue.RelatedPage && s.RelatedPage == issue.Page) ||
			(s.Page == issue.Page && s.RelatedPage == "" && issue.RelatedPage == "") {
			return true
		}
	}
	return false
}

// suppressionID computes a stable, deterministic ID from the suppression key fields.
func suppressionID(check, page, relatedPage string) string {
	// Normalize page order for stable IDs.
	a, b := page, relatedPage
	if a > b {
		a, b = b, a
	}
	input := fmt.Sprintf("%s|%s|%s", check, a, b)
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", hash[:8])
}

// FilterSuppressed returns a new slice with suppressed issues removed.
func FilterSuppressed(issues []Issue, sf *SuppressionFile) []Issue {
	if sf == nil || len(sf.Suppressions) == 0 {
		return issues
	}
	filtered := make([]Issue, 0, len(issues))
	for _, iss := range issues {
		if !sf.IsSuppressed(iss) {
			filtered = append(filtered, iss)
		}
	}
	return filtered
}

