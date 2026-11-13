// Package revert implements the "fundamentum revert" command.
package revert

import (
	"encoding/base64"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/cmd/util"
	"github.com/jpvelasco/fundamentum/internal/github"
)

// newClient creates the GitHub API client; a package-level var so tests can
// inject a mock-server client.
var newClient = github.NewClient

// Mode selects what revert undoes.
type Mode string

const (
	ModeFiles    Mode = "files"    // restore files only
	ModeSettings Mode = "settings" // files + repo settings + security toggles
	ModeFull     Mode = "full"     // files + settings + rulesets + recorded labels (the default)
)

// NewCmd returns the revert subcommand.
func NewCmd() *cobra.Command {
	var mode string
	cmd := &cobra.Command{
		Use:   "revert OWNER/REPO",
		Short: "Undo the last managed harden apply",
		Long: `Undo the last fundamentum apply using the snapshot written to
.github/fundamentum-baseline.json at apply time.

Modes:
  full     restore files, repo settings, security toggles, and the managed
           rulesets and recorded labels to their pre-apply state (default)
  files    restore only the files apply wrote (delete what it created,
           restore what it overwrote)
  settings restore files + repo settings + security toggles (leave the
           rulesets as they are)

--dry-run prints the plan without changing anything. Revert is idempotent:
a snapshot field that already matches is skipped.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			owner, repo, err := util.ParseOwnerRepo(args[0])
			if err != nil {
				return err
			}
			token, err := util.RequireToken(globals.Token)
			if err != nil {
				return err
			}
			return run(newClient(token, globals.Verbose), owner, repo, Mode(mode), globals.DryRun, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&mode, "mode", string(ModeFull), "revert scope: full, files, or settings")
	return cmd
}

// run executes the revert: read the snapshot, plan the undo, apply it.
func run(c *github.Client, owner, repo string, mode Mode, dryRun bool, stdout io.Writer) error {
	if mode != ModeFiles && mode != ModeSettings && mode != ModeFull {
		return fmt.Errorf("invalid --mode %q: use full, files, or settings", mode)
	}
	content, _, ok, err := c.GetFileContent(owner, repo, BaselinePath, "")
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}
	if !ok {
		return fmt.Errorf("no snapshot found at %s — nothing to revert (apply first, or the repo was hardened before snapshots were added)", BaselinePath)
	}
	raw, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return fmt.Errorf("decode snapshot: %w", err)
	}
	snap, err := DecodeSnapshot(raw)
	if err != nil {
		return err
	}

	plan, err := plan(c, owner, repo, snap, mode)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "fundamentum revert %s/%s (%s)\n\n", owner, repo, mode)
	for _, item := range plan {
		_, _ = fmt.Fprintf(stdout, "  %s\n", item)
	}
	if dryRun {
		_, _ = fmt.Fprintln(stdout, "\n  Dry run complete — no changes made.")
		return nil
	}
	return execute(c, owner, repo, snap, mode, stdout)
}

// plan lists what revert will change (for dry-run / confirmation).
func plan(c *github.Client, owner, repo string, snap *Snapshot, mode Mode) ([]string, error) {
	var items []string
	for _, f := range snap.Files {
		if f.Present {
			items = append(items, "restore "+f.Path)
		} else {
			items = append(items, "delete "+f.Path)
		}
	}
	if mode == ModeFiles {
		return items, nil
	}
	items = append(items, "restore repo settings (merge methods, auto-delete)")
	items = append(items, "restore security toggles")
	if mode == ModeSettings {
		return items, nil
	}
	if snap.Rulesets.ProtectMain.Existed {
		items = append(items, "restore protect-main ruleset")
	} else {
		items = append(items, "delete protect-main ruleset")
	}
	if snap.Rulesets.ProtectVersionTags.Existed {
		items = append(items, "restore protect-version-tags ruleset")
	} else {
		items = append(items, "delete protect-version-tags ruleset")
	}
	for _, label := range snap.Labels {
		action := "restore label "
		if !label.Present {
			action = "delete label (removes issue assignments) "
		}
		items = append(items, action+label.Name)
	}
	return items, nil
}

// execute applies the revert for the given mode.
func execute(c *github.Client, owner, repo string, snap *Snapshot, mode Mode, stdout io.Writer) error {
	// Files first.
	for _, f := range snap.Files {
		if f.Present {
			if _, err := c.UpsertFile(owner, repo, f.Path, mustDecode(f.Content)); err != nil {
				return fmt.Errorf("restore %s: %w", f.Path, err)
			}
			_, _ = fmt.Fprintf(stdout, "  ✓ restored %s\n", f.Path)
		} else {
			if err := c.DeleteFile(owner, repo, f.Path); err != nil {
				return fmt.Errorf("delete %s: %w", f.Path, err)
			}
			_, _ = fmt.Fprintf(stdout, "  ✓ deleted %s\n", f.Path)
		}
	}
	if mode == ModeFiles {
		_, _ = fmt.Fprintln(stdout, "\n  ✓ Done — files reverted")
		return nil
	}
	if err := c.UpdateRepoSettings(owner, repo, snap.Settings); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(stdout, "  ✓ restored repo settings")
	if err := c.DisableSecurity(owner, repo, snap.Security); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(stdout, "  ✓ restored security toggles")
	if mode == ModeSettings {
		_, _ = fmt.Fprintln(stdout, "\n  ✓ Done — files + settings reverted")
		return nil
	}
	if snap.Rulesets.ProtectMain.Existed {
		id, err := c.RulesetIDForUpdate(owner, repo, "protect-main")
		if err != nil {
			return err
		}
		if err := c.UpdateRuleset(owner, repo, id, snap.Rulesets.ProtectMain.Body); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, "  ✓ restored protect-main ruleset")
	} else {
		if err := c.DeleteRuleset(owner, repo, "protect-main"); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, "  ✓ deleted protect-main ruleset")
	}
	if snap.Rulesets.ProtectVersionTags.Existed {
		id, err := c.RulesetIDForUpdate(owner, repo, "protect-version-tags")
		if err != nil {
			return err
		}
		if err := c.UpdateRuleset(owner, repo, id, snap.Rulesets.ProtectVersionTags.Body); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, "  ✓ restored protect-version-tags ruleset")
	} else {
		if err := c.DeleteRuleset(owner, repo, "protect-version-tags"); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, "  ✓ deleted protect-version-tags ruleset")
	}
	if err := restoreLabels(c, owner, repo, snap.Labels, stdout); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(stdout, "\n  ✓ Done — full revert applied")
	return nil
}

// mustDecode base64-decodes snapshot content; it panics only on corrupt data.
func mustDecode(b64 string) []byte {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		panic("corrupt snapshot content: " + err.Error())
	}
	return b
}
