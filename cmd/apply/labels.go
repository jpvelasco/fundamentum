package apply

import (
	"fmt"
	"io"

	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/steps"
	"github.com/jpvelasco/fundamentum/internal/wizard"
)

func labelItems(c *github.Client, owner, repo string, selector steps.Selector, stdout io.Writer) ([]wizard.Item, error) {
	if !selector.IncludeStep(steps.Labels) {
		return []wizard.Item{optedOutItem("Priority labels", steps.Labels, selector)}, nil
	}
	var live []github.Label
	if c != nil {
		var err error
		live, err = c.ListLabels(owner, repo)
		if err != nil {
			return nil, err
		}
	}
	for _, advice := range github.PriorityMigrationAdvice(live) {
		_, _ = fmt.Fprintln(stdout, "  ⚠ "+advice)
	}
	var items []wizard.Item
	for _, wanted := range github.PriorityLabels() {
		prior, present := github.FindLabel(live, wanted.Name)
		item := wizard.Item{Name: "Label " + wanted.Name, LabelName: wanted.Name, Action: wizard.ActionCreate}
		if present {
			item.Detail = github.LabelDrift(prior, wanted)
			item.Action = wizard.ActionSkip
			if item.Detail != "" {
				item.Action = wizard.ActionUpdate
				item.Detail = "drift: " + item.Detail
			}
			// Preserve the existing name's casing; only metadata is managed.
			wanted.Name = prior.Name
		}
		item.Apply = func() error { return c.WriteLabel(owner, repo, wanted, present) }
		items = append(items, item)
	}
	return items, nil
}
