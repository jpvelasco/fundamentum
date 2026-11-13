package apply

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
	"github.com/jpvelasco/fundamentum/internal/pii"
	"github.com/jpvelasco/fundamentum/internal/steps"
	"github.com/jpvelasco/fundamentum/internal/templates"
)

// PIIRollout is shared by apply and audit so enrollment has one policy owner.
type PIIRollout struct {
	Ready, OptedOut bool
	Detail          string
}

// ResolvePIIChecks adds the pack-neutral check only after reviewed policy and
// exact baseline assets exist together on a clean, pinned default-branch tree.
// Explicit --require-checks remains a complete override.
func ResolvePIIChecks(client *github.Client, info github.Repo, pack string, files []templates.RenderedFile) ([]string, []string, PIIRollout, error) {
	checks := requiredChecks(pack)
	shipped := []string{}
	if globals.RequireChecks == nil {
		shipped = slices.Clone(checks)
	}
	rollout, err := piiReadiness(client, info, files)
	if err != nil {
		return nil, nil, rollout, err
	}
	if globals.RequireChecks != nil && !slices.Contains(checks, pii.CheckName) {
		rollout.OptedOut = true
		rollout.Detail += "; new enrollment omitted by --require-checks"
	}
	if rollout.Ready && !rollout.OptedOut {
		if globals.RequireChecks == nil {
			checks = append(checks, pii.CheckName)
		}
		if slices.Contains(checks, pii.CheckName) {
			shipped = append(shipped, pii.CheckName)
		}
	} else if slices.Contains(checks, pii.CheckName) {
		return nil, nil, rollout, fmt.Errorf("cannot enroll pii-gate: %s", rollout.Detail)
	}
	return checks, shipped, rollout, nil
}

func piiReadiness(client *github.Client, info github.Repo, files []templates.RenderedFile) (PIIRollout, error) {
	selector := steps.New(globals.SkipTokens(), globals.OnlySteps)
	optedOut := !selector.IncludeStep(steps.ProtectMain) ||
		selector.ExcludeFileReason(pii.WorkflowPath) == "--skip" ||
		selector.ExcludeFileReason(pii.HelperPath) == "--skip"
	result := PIIRollout{OptedOut: optedOut, Detail: "pending review: run pii-bootstrap, review and merge " + pii.AllowlistPath + ", then ship both gate files and re-apply"}
	present, err := client.HasPIIPolicy(info.Owner, info.RepoName)
	if err != nil {
		return result, err
	}
	if !present {
		return result, nil
	}
	_, blobs, err := client.TrackedTree(info.Owner, info.RepoName, info.DefaultBranch)
	if err != nil {
		return result, err
	}
	contents := map[string][]byte{}
	for _, blob := range blobs {
		contents[blob.Path] = blob.Data
	}
	policyBody, ok := contents[pii.AllowlistPath]
	if !ok {
		return result, nil
	}
	policy, err := pii.ParsePolicy(policyBody, pii.AllowlistPath)
	if err != nil {
		return result, err
	}
	for _, path := range []string{pii.WorkflowPath, pii.HelperPath} {
		var expected string
		for _, file := range files {
			if file.Path == path {
				expected = file.Content
				break
			}
		}
		if expected == "" || !bytes.Equal(contents[path], []byte(expected)) {
			result.Detail = "pending gate assets: merge the intended " + pii.WorkflowPath + " and " + pii.HelperPath + "; re-apply after merge"
			return result, nil
		}
	}
	var locations bytes.Buffer
	for _, blob := range blobs {
		matches := pii.ScanBytes(blob.Data, policy)
		pii.WriteMatches(&locations, blob.Path, matches)
	}
	if locations.Len() > 0 {
		result.Detail = "pending scrub or reviewed exact exemptions: " + strings.TrimSpace(locations.String())
		return result, nil
	}
	result.Ready = true
	result.Detail = "reviewed policy and baseline gate files are committed; pinned default tree scans clean"
	if optedOut {
		result.Detail = "enrollment opted out by --skip/--only/keep; existing requirements remain"
	}
	return result, nil
}

// InspectPIIRule never changes live identity or hides a broken existing gate.
func InspectPIIRule(plan github.RulesetPlan, rollout PIIRollout) (bool, string, error) {
	if plan.Live == nil {
		return false, rollout.Detail, nil
	}
	for _, rule := range plan.Live.Rules {
		if rule["type"] != "required_status_checks" {
			continue
		}
		params, _ := rule["parameters"].(map[string]any)
		checks, _ := params["required_status_checks"].([]any)
		for _, raw := range checks {
			check, _ := raw.(map[string]any)
			if check["context"] != pii.CheckName {
				continue
			}
			if pin, ok := check["integration_id"]; ok && pin != nil && fmt.Sprint(pin) != fmt.Sprint(github.GitHubActionsAppID) {
				return true, "", fmt.Errorf("pii-gate context has an incompatible live app pin; verify its producer before enrollment")
			}
			if !rollout.Ready {
				return true, "existing required pii-gate needs repair: " + rollout.Detail, nil
			}
			return true, rollout.Detail, nil
		}
	}
	return false, rollout.Detail, nil
}
