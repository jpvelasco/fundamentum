package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Label is the repository label metadata managed independently of issue assignments.
type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// PriorityLabels returns a fresh copy of the importance-based managed baseline.
func PriorityLabels() []Label {
	return []Label{
		{"priority:critical", "8b0000", "Code execution, destructive data paths, or silent data corruption — fix first"},
		{"priority:high", "d93f0b", "Broken core flows, state corruption, orphaned paid resources, or unencrypted credential paths"},
		{"priority:medium", "fbca04", "Conditional breakage, wrong output or cost figures, silent truncation"},
		{"priority:low", "c5def5", "Cosmetic inconsistencies, edge cases, minor DX papercuts"},
	}
}

func FindLabel(labels []Label, name string) (Label, bool) {
	for _, label := range labels {
		if strings.EqualFold(label.Name, name) {
			return label, true
		}
	}
	return Label{}, false
}

// LabelDrift compares metadata; GitHub colors have no leading hash and are case-insensitive.
func LabelDrift(live, wanted Label) string {
	var fields []string
	if !strings.EqualFold(strings.TrimPrefix(live.Color, "#"), strings.TrimPrefix(wanted.Color, "#")) {
		fields = append(fields, "color")
	}
	if live.Description != wanted.Description {
		fields = append(fields, "description")
	}
	return strings.Join(fields, ", ")
}

func (c *Client) ListLabels(owner, repo string) ([]Label, error) {
	var labels []Label
	path := repoPath(owner, repo) + "/labels?per_page=100"
	for path != "" {
		resp, err := c.get(path)
		if err != nil {
			return nil, fmt.Errorf("list labels: %w", err)
		}
		if err := expectStatus("list labels", resp, http.StatusOK); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		var page []Label
		err = json.NewDecoder(resp.Body).Decode(&page)
		path = nextLink(resp.Header.Get("Link"))
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode labels: %w", err)
		}
		labels = append(labels, page...)
	}
	return labels, nil
}

// WriteLabel creates or updates metadata without renaming or changing assignments.
func (c *Client) WriteLabel(owner, repo string, label Label, present bool) error {
	method, path := http.MethodPost, repoPath(owner, repo)+"/labels"
	if present {
		method = http.MethodPatch
		path += "/" + url.PathEscape(label.Name)
	}
	resp, err := c.do(method, path, label)
	if err != nil {
		return fmt.Errorf("write label %s: %w", label.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return expectStatus("write label "+label.Name, resp, http.StatusOK, http.StatusCreated)
}

func (c *Client) DeleteLabel(owner, repo, name string) error {
	resp, err := c.do(http.MethodDelete, repoPath(owner, repo)+"/labels/"+url.PathEscape(name), nil)
	if err != nil {
		return fmt.Errorf("delete label %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return expectStatus("delete label "+name, resp, http.StatusNoContent, http.StatusNotFound)
}

// PriorityMigrationAdvice never equates effort with importance or changes legacy labels.
func PriorityMigrationAdvice(labels []Label) []string {
	canonical := PriorityLabels()
	var mappings []string
	for _, label := range canonical {
		mappings = append(mappings, label.Name+" for "+label.Description)
	}
	var advice []string
	for _, label := range labels {
		name := strings.ToLower(label.Name)
		if _, managed := FindLabel(canonical, name); managed {
			continue
		}
		legacyTier := name == "p0" || name == "p1" || name == "p2" || strings.HasPrefix(name, "p0-") || strings.HasPrefix(name, "p1-") || strings.HasPrefix(name, "p2-")
		if !legacyTier && !strings.HasPrefix(name, "priority:") {
			continue
		}
		axis := "importance"
		if legacyTier {
			axis = "importance (legacy tiers do not determine impact; effort and size are separate)"
		}
		advice = append(advice, fmt.Sprintf("legacy label %q: review issue %s; use %s. Owner relabels issues and retires legacy labels; no automatic mapping.", label.Name, axis, strings.Join(mappings, "; ")))
	}
	return advice
}
