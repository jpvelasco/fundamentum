package audit

import (
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/steps"
)

func labelChecks(c *github.Client, owner, repo string, selector steps.Selector) ([]check, error) {
	if !selector.IncludeStep(steps.Labels) {
		return []check{skipCheck(steps.Labels, selector)}, nil
	}
	live, err := c.ListLabels(owner, repo)
	if err != nil {
		return nil, err
	}
	var results []check
	for _, wanted := range github.PriorityLabels() {
		row := check{Name: wanted.Name, Status: statusPass}
		prior, present := github.FindLabel(live, wanted.Name)
		drift := github.LabelDrift(prior, wanted)
		switch {
		case !present:
			row.Status, row.Detail = statusFail, "missing label — re-apply"
		case drift != "":
			row.Status, row.Detail = statusWarn, "drift: "+drift
		}
		results = append(results, row)
	}
	for _, advice := range github.PriorityMigrationAdvice(live) {
		results = append(results, check{Name: "priority migration", Status: statusWarn, Detail: advice})
	}
	return results, nil
}
