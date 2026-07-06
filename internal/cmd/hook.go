package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hitzhangjie/ruminate/internal/config"
)

const ruminateHookMarker = "# Installed by ruminate hook install"

// hookScript is the content of the post-commit hook installed by ruminate.
const hookScript = `#!/bin/sh
# Installed by ruminate hook install
# Calls ruminate sync to update the knowledge base after each commit
ruminate sync --repo "$(git rev-parse --show-toplevel)"
`

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Manage git hooks for automatic sync",
	Long: `Install or uninstall git post-commit hooks that automatically trigger
"ruminate sync" after each commit in a source repository.

The hook is installed as a post-commit hook — after each commit in the source
repo, "ruminate sync" is called to incrementally update the knowledge base.

Subcommands:
  install    Install the post-commit hook
  uninstall  Remove the ruminate-managed post-commit hook`,
}

var hookInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install post-commit hook to auto-trigger ruminate sync",
	Long: `Install a post-commit git hook in the specified repository.

The hook runs "ruminate sync --repo <repo_path>" after each commit, keeping
your knowledge base up to date automatically.

If a post-commit hook already exists and was not installed by ruminate,
the command will refuse to overwrite it. Merge the scripts manually in
that case.

Example:
  ruminate hook install --repo ~/notes`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		repo, _ := cmd.Flags().GetString("repo")
		if repo == "" {
			var err error
			repo, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("getting current directory: %w", err)
			}
		}
		repo = config.ExpandPath(repo)

		hookPath := filepath.Join(repo, ".git", "hooks", "post-commit")

		// Check if hook already exists
		existing, err := os.ReadFile(hookPath)
		if err == nil {
			// Hook exists — check if it's ours
			if strings.Contains(string(existing), ruminateHookMarker) {
				fmt.Println("Hook already installed by ruminate — reinstalling...")
			} else {
				return fmt.Errorf(
					"post-commit hook already exists at %s and was not installed by ruminate.\n"+
						"Please merge the following script manually:\n\n%s\n\n"+
						"Existing hook content:\n%s",
					hookPath, hookScript, string(existing),
				)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("checking hook file: %w", err)
		}

		// Ensure hooks directory exists
		hooksDir := filepath.Dir(hookPath)
		if err := os.MkdirAll(hooksDir, 0755); err != nil {
			return fmt.Errorf("creating hooks directory: %w", err)
		}

		// Write hook script
		if err := os.WriteFile(hookPath, []byte(hookScript), 0755); err != nil {
			return fmt.Errorf("writing hook: %w", err)
		}

		fmt.Printf("✓ post-commit hook installed at %s\n", hookPath)
		fmt.Println("  ruminate sync will run automatically after each commit.")
		return nil
	},
}

var hookUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove the ruminate post-commit hook",
	Long: `Remove the post-commit git hook if it was installed by ruminate.

Only removes hooks that contain the ruminate marker comment.
If the hook was not installed by ruminate, it will not be touched.

Example:
  ruminate hook uninstall --repo ~/notes`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		repo, _ := cmd.Flags().GetString("repo")
		if repo == "" {
			var err error
			repo, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("getting current directory: %w", err)
			}
		}
		repo = config.ExpandPath(repo)

		hookPath := filepath.Join(repo, ".git", "hooks", "post-commit")

		// Check if hook exists
		existing, err := os.ReadFile(hookPath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No post-commit hook found — nothing to uninstall.")
				return nil
			}
			return fmt.Errorf("reading hook file: %w", err)
		}

		// Verify it's a ruminate hook
		if !strings.Contains(string(existing), ruminateHookMarker) {
			errmsg := "post-commit hook at %s was not installed by ruminate — refusing to remove.\n" +
				"Remove it manually if needed."
			return fmt.Errorf(errmsg, hookPath)
		}

		// Remove the hook
		if err := os.Remove(hookPath); err != nil {
			return fmt.Errorf("removing hook: %w", err)
		}

		fmt.Printf("✓ post-commit hook removed from %s\n", hookPath)
		return nil
	},
}

func init() {
	hookCmd.AddCommand(hookInstallCmd)
	hookCmd.AddCommand(hookUninstallCmd)

	hookInstallCmd.Flags().StringP("repo", "r", "", "Path to source repository (defaults to current directory)")
	hookUninstallCmd.Flags().StringP("repo", "r", "", "Path to source repository (defaults to current directory)")
}
