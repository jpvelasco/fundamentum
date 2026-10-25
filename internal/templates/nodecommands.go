package templates

import (
	"encoding/json"
)

// defaultNodeLintCommand / defaultNodeTestCommand are the run: lines the node
// CI pack falls back to when package.json has no matching script. They FAIL
// the job with a configure-me annotation instead of passing vacuously — a
// required check that always passes is false confidence.
const (
	defaultNodeLintCommand = "echo ::error::package.json has no lint script - add one or edit .github/workflows/ci.yml && exit 1"
	defaultNodeTestCommand = "echo ::error::package.json has no test script - add one or edit .github/workflows/ci.yml && exit 1"
)

// NodeCommands builds the run: lines for the node CI pack from a package.json
// body. A script present in package.json runs it; a missing script keeps the
// failing configure-me command so the required Lint/Test contexts stay honest.
func NodeCommands(packageJSON []byte) (lint, test string) {
	lint, test = defaultNodeLintCommand, defaultNodeTestCommand
	if len(packageJSON) == 0 {
		return lint, test
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(packageJSON, &pkg); err != nil {
		return lint, test
	}
	if pkg.Scripts["lint"] != "" {
		lint = "npm run lint"
	}
	if pkg.Scripts["test"] != "" {
		test = "npm test"
	}
	return lint, test
}

// ReadmeVariants are the README filenames checked, in priority order.
var ReadmeVariants = []string{"README.md", "README.rst", "README.txt", "README"}

// DetectReadmePath picks the repo's README filename from the detected
// variants, defaulting to README.md when none exists (the PR template's
// documentation line still needs a plausible target).
func DetectReadmePath(readmeVariants []string) string {
	for _, name := range readmeVariants {
		if name != "" {
			return name
		}
	}
	return "README.md"
}

// DetectAgentGuideLine builds the PR-template documentation line for the
// repo's AI agent guide. AGENTS.md is the standard; CLAUDE.md is legacy and
// only referenced when it exists and AGENTS.md does not. With neither, the
// line still points at AGENTS.md so new repos converge on the standard.
func DetectAgentGuideLine(agentsExists, claudeExists bool) string {
	switch {
	case agentsExists:
		return agentGuideLine("AGENTS.md")
	case claudeExists:
		return agentGuideLine("CLAUDE.md")
	default:
		return agentGuideLine("AGENTS.md")
	}
}

// DetectDocData resolves the detection-dependent RepoData fields: the README
// filename, the agent-guide checklist line, and — for the node pack — the
// lint/test commands from package.json scripts. exists probes a repo path;
// readPackageJSON fetches package.json (ok=false when absent). Callers pass
// client-bound closures so this stays transport-agnostic (the same pattern
// as DetectManifests).
func DetectDocData(exists func(path string) (bool, error), readPackageJSON func() ([]byte, bool, error), pack string) (readmePath, agentGuideLine, nodeLintCommand, nodeTestCommand string, err error) {
	detected := map[string]bool{}
	for _, name := range ReadmeVariants {
		ok, err := exists(name)
		if err != nil {
			return "", "", "", "", err
		}
		detected[name] = ok
	}
	var found []string
	for _, name := range ReadmeVariants {
		if detected[name] {
			found = append(found, name)
		}
	}
	readmePath = DetectReadmePath(found)

	agentsExists, err := exists("AGENTS.md")
	if err != nil {
		return "", "", "", "", err
	}
	claudeExists := false
	if !agentsExists {
		claudeExists, err = exists("CLAUDE.md")
		if err != nil {
			return "", "", "", "", err
		}
	}
	agentGuideLine = DetectAgentGuideLine(agentsExists, claudeExists)

	if pack == CIPackNode {
		body, ok, err := readPackageJSON()
		if err != nil {
			return "", "", "", "", err
		}
		if ok {
			nodeLintCommand, nodeTestCommand = NodeCommands(body)
		}
	}
	return readmePath, agentGuideLine, nodeLintCommand, nodeTestCommand, nil
}
