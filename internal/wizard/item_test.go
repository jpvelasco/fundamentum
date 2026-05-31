package wizard

import (
	"testing"
)

func TestItemActionString(t *testing.T) {
	tests := []struct {
		action Action
		want   string
	}{
		{ActionCreate, "create"},
		{ActionUpdate, "update"},
		{ActionSkip, "skip"},
	}
	for _, tt := range tests {
		if got := tt.action.String(); got != tt.want {
			t.Errorf("action %v: got %q, want %q", tt.action, got, tt.want)
		}
	}
}

func TestItemDryRunLabel(t *testing.T) {
	item := Item{Name: "CONTRIBUTING.md", Action: ActionCreate}
	if item.DryRunLabel() != "would create" {
		t.Errorf("unexpected dry-run label: %q", item.DryRunLabel())
	}
	item.Action = ActionSkip
	if item.DryRunLabel() != "already exists — skip" {
		t.Errorf("unexpected dry-run label for skip: %q", item.DryRunLabel())
	}
}
