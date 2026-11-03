package revert

import (
	"fmt"

	"github.com/jpvelasco/fundamentum/internal/github"
)

// CaptureSnapshotForApply snapshots the pre-apply state of owner/repo for the
// given managed file paths and returns it. Called by apply before any writes
// so a later revert can restore the exact prior state.
func CaptureSnapshotForApply(c *github.Client, owner, repo, branch string, filePaths []string) (*Snapshot, error) {
	return CaptureSnapshot(c, owner, repo, branch, filePaths)
}

// WriteSnapshot upserts the snapshot to .github/fundamentum-baseline.json so
// revert can read it back. Written like any other managed file.
func WriteSnapshot(c *github.Client, owner, repo, branch string, snap *Snapshot) error {
	b, err := snap.Encode()
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	if _, err := c.UpsertFile(owner, repo, BaselinePath, b); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}
