package templates

// FileAliases maps a rendered target path to known case/path variants that
// count as already present (legacy root placements, case variants, .yml vs .md).
func FileAliases() map[string][]string {
	return map[string][]string{
		".github/CODEOWNERS": {
			".github/CODEOWNERS",
			"CODEOWNERS",
		},
		".github/CONTRIBUTING.md": {
			".github/CONTRIBUTING.md",
			"CONTRIBUTING.md",
		},
		".github/CODE_OF_CONDUCT.md": {
			".github/CODE_OF_CONDUCT.md",
			"CODE_OF_CONDUCT.md",
		},
		".github/SECURITY.md": {
			".github/SECURITY.md",
			"SECURITY.md",
		},
		".github/PULL_REQUEST_TEMPLATE.md": {
			".github/PULL_REQUEST_TEMPLATE.md",
			".github/pull_request_template.md",
		},
		".codacy.yml": {
			".codacy.yml",
			".codacy.yaml",
			".codacy/codacy.yaml",
			".codacy/codacy.yml",
		},
		".github/ISSUE_TEMPLATE/bug_report.yml": {
			".github/ISSUE_TEMPLATE/bug_report.yml",
			".github/ISSUE_TEMPLATE/bug_report.md",
		},
		".github/ISSUE_TEMPLATE/feature_request.yml": {
			".github/ISSUE_TEMPLATE/feature_request.yml",
			".github/ISSUE_TEMPLATE/feature_request.md",
		},
		".github/workflows/octopus.yml": {
			".github/workflows/octopus.yml",
			".github/workflows/octopus-review.yml",
		},
	}
}

// FileCheckPaths returns the paths that count as the rendered file existing.
func FileCheckPaths(path string) []string {
	if variants, ok := FileAliases()[path]; ok {
		return variants
	}
	return []string{path}
}
