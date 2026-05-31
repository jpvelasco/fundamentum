// Package wizard drives the interactive summary-table and Y/N apply flow.
package wizard

import "fmt"

// Action describes what fundamentum will do for a given item.
type Action int

const (
	ActionCreate Action = iota
	ActionUpdate
	ActionSkip
)

func (a Action) String() string {
	switch a {
	case ActionCreate:
		return "create"
	case ActionUpdate:
		return "update"
	case ActionSkip:
		return "skip"
	default:
		return "unknown"
	}
}

// Item is a single setting or file that fundamentum manages.
type Item struct {
	Name   string
	Action Action
	Apply  func() error
}

// DryRunLabel returns the human-readable action for --dry-run output.
func (i Item) DryRunLabel() string {
	if i.Action == ActionSkip {
		return "already exists — skip"
	}
	return fmt.Sprintf("would %s", i.Action)
}

// LiveLabel returns the human-readable action for live output.
func (i Item) LiveLabel() string {
	if i.Action == ActionSkip {
		return "already exists — skip"
	}
	return i.Action.String()
}
