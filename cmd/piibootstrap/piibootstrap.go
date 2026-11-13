// Package piibootstrap writes a local, unreviewed policy from a pinned remote tree.
package piibootstrap

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/cmd/util"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/pii"
	"github.com/spf13/cobra"
)

var newClient = github.NewClient

func NewCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "pii-bootstrap OWNER/REPO",
		Short: "Write a local cloud-ID allowlist for operator review",
		Long:  "Scan every tracked default-branch blob. Review each candidate before committing the allowlist: scrub inappropriate values from the source or retain legitimate exact exemptions. Existing policy is not applied to candidate collection. No remote changes are made.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			owner, repo, err := util.ParseOwnerRepo(args[0])
			if err != nil {
				return err
			}
			token, err := util.RequireToken(globals.Token)
			if err != nil {
				return err
			}
			return run(newClient(token, globals.Verbose), owner, repo, output, globals.DryRun, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&output, "output", pii.AllowlistPath, "local output path (must not exist)")
	return cmd
}

func run(client *github.Client, owner, repo, output string, dryRun bool, out io.Writer) error {
	info, err := client.GetRepo(owner, repo)
	if err != nil {
		return fmt.Errorf("read repository: verify access and retry")
	}
	sha, blobs, err := client.TrackedTree(owner, repo, info.DefaultBranch)
	if err != nil {
		return err
	}
	candidates := map[string]bool{}
	for _, blob := range blobs {
		matches := pii.ScanBytes(blob.Data, nil)
		pii.WriteMatches(out, blob.Path, matches)
		for _, match := range matches {
			candidates[match.Value] = true
		}
	}
	_, _ = fmt.Fprintf(out, "%d distinct candidates; destination %s\n", len(candidates), pii.SafePath(output))
	if dryRun {
		_, _ = fmt.Fprintln(out, "Dry run: no local file or directories created.")
		return nil
	}
	values := make([]string, 0, len(candidates))
	for value := range candidates {
		values = append(values, value)
	}
	sort.Strings(values)
	var body bytes.Buffer
	_, _ = fmt.Fprintf(&body, "# UNREVIEWED candidates from commit %s\n# Review every entry before committing. Scrub inappropriate source values;\n# retain only legitimate exact exemptions. A committed valid policy signals review.\n", sha)
	for _, value := range values {
		_, _ = fmt.Fprintln(&body, value)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return fmt.Errorf("cannot create local output directory")
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("cannot create output %s; choose a new path (existing files are never overwritten)", pii.SafePath(output))
	}
	_, writeErr := file.Write(body.Bytes())
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(output)
		return fmt.Errorf("cannot write complete local policy")
	}
	return nil
}
