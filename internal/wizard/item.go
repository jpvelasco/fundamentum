// Package wizard drives the interactive summary-table and Y/N apply flow.
package wizard

import "fmt"

// Action describes what fundamentum will do for a given item.
type Action int

const (
	ActionCreate Action = iota
	ActionUpdate
	ActionSkip
	ActionUpgrade
)

func (a Action) String() string {
	switch a {
	case ActionCreate:
		return "create"
	case ActionUpdate:
		return "update"
	case ActionSkip:
		return "skip"
	case ActionUpgrade:
		return "upgrade classic → ruleset"
	default:
		return "unknown"
	}
}

// Item is a single setting or file that fundamentum manages.
type Item struct {
	Name     string
	Action   Action
	Apply    func() error
	Optional bool   // failures show as ⚠ (plan limit) rather than ✗ (error)
	Content  []byte // rendered file content for file items (used by PR mode)
	// Detail explains an update (field drift, diffstat) or a non-obvious skip
	// (plan-unavailable protection). Empty on plain skips.
	Detail string
	// OptOutReason marks the item as excluded by --skip / --only. The reason
	// names the mechanism ("--skip", "--only") and is shown in the plan table
	// and apply progress instead of "already exists".
	OptOutReason string
}

// DryRunLabel returns the human-readable action for --dry-run output.
func (i Item) DryRunLabel() string {
	switch {
	case i.Action == ActionSkip && i.OptOutReason != "":
		return "skipped (" + i.OptOutReason + ")"
	case i.Action == ActionSkip:
		if i.Detail != "" {
			return i.Detail
		}
		return "already exists — skip"
	case i.Detail != "":
		return fmt.Sprintf("would %s (%s)", i.Action, i.Detail)
	default:
		return fmt.Sprintf("would %s", i.Action)
	}
}

// LiveLabel returns the human-readable action for live output.
func (i Item) LiveLabel() string {
	switch {
	case i.Action == ActionSkip && i.OptOutReason != "":
		return "skipped (" + i.OptOutReason + ")"
	case i.Action == ActionSkip:
		return "already exists — skip"
	case i.Detail != "":
		return fmt.Sprintf("%s (%s)", i.Action, i.Detail)
	default:
		return i.Action.String()
	}
}

func (i Item) IsSkip() bool { return i.Action == ActionSkip }
