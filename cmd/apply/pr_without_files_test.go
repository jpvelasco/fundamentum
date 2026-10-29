package apply

import (
	"io"
	"testing"

	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

func TestApplyItems_ViaPRWithoutFilesKeepsRequiredChecks(t *testing.T) {
	opts := &github.BranchProtectionOptions{BotManaged: true}
	applied := false
	items := []wizard.Item{{
		Name:   "Branch protection",
		Action: wizard.ActionUpdate,
		Apply: func() error {
			applied = true
			if opts.SkipStatusChecks {
				t.Error("required checks were deferred without a file PR")
			}
			return nil
		},
	}}

	err := applyItems(github.NewClient("", false), "owner", "repo", "main", items, true, opts, io.Discard)
	if err != nil {
		t.Fatalf("applyItems() error: %v", err)
	}
	if !applied {
		t.Error("branch protection was not applied")
	}
}
