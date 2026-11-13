package revert

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/jpvelasco/fundamentum/internal/github"
)

// LabelState records explicit prior presence only for labels selected for change.
// An omitted labels field in a V1 snapshot authorizes no label operations.
type LabelState struct {
	Name        string `json:"name"`
	Present     bool   `json:"present"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

// UnmarshalJSON rejects an incomplete presence record rather than treating
// omitted state as permission to delete a label.
func (s *LabelState) UnmarshalJSON(data []byte) error {
	type labelState LabelState
	var decoded labelState
	raw := struct {
		*labelState
		Present *bool `json:"present"`
	}{labelState: &decoded}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Present == nil {
		return fmt.Errorf("snapshot label %q is missing explicit presence", decoded.Name)
	}
	decoded.Present = *raw.Present
	*s = LabelState(decoded)
	return nil
}

func (s *Snapshot) CaptureLabels(c *github.Client, owner, repo string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	labels, err := c.ListLabels(owner, repo)
	if err != nil {
		return fmt.Errorf("snapshot labels: %w", err)
	}
	for _, name := range names {
		prior, present := github.FindLabel(labels, name)
		state := LabelState{Name: name, Present: present}
		if present {
			state.Name, state.Color, state.Description = prior.Name, prior.Color, prior.Description
		}
		s.Labels = append(s.Labels, state)
	}
	return nil
}

func restoreLabels(c *github.Client, owner, repo string, states []LabelState, stdout io.Writer) error {
	if len(states) == 0 {
		return nil
	}
	live, err := c.ListLabels(owner, repo)
	if err != nil {
		return err
	}
	for _, state := range states {
		current, present := github.FindLabel(live, state.Name)
		wanted := github.Label{Name: state.Name, Color: state.Color, Description: state.Description}
		switch {
		case state.Present && (!present || github.LabelDrift(current, wanted) != ""):
			if present {
				wanted.Name = current.Name
			}
			if err := c.WriteLabel(owner, repo, wanted, present); err != nil {
				return err
			}
		case !state.Present && present:
			if err := c.DeleteLabel(owner, repo, current.Name); err != nil {
				return err
			}
		default:
			continue
		}
		_, _ = fmt.Fprintf(stdout, "  ✓ reverted label %s\n", state.Name)
	}
	return nil
}
