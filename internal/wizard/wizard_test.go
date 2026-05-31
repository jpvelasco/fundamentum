package wizard

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintSummaryTable(t *testing.T) {
	items := []Item{
		{Name: "CONTRIBUTING.md", Action: ActionCreate},
		{Name: "SECURITY.md", Action: ActionSkip},
		{Name: ".github/dependabot.yml", Action: ActionUpdate},
	}
	var buf bytes.Buffer
	PrintSummaryTable(&buf, items, false)
	out := buf.String()
	if !strings.Contains(out, "CONTRIBUTING.md") {
		t.Error("expected CONTRIBUTING.md in table")
	}
	if !strings.Contains(out, "would create") {
		t.Error("expected 'would create' in dry-run table")
	}
	if !strings.Contains(out, "already exists") {
		t.Error("expected skip label in table")
	}
}

func TestPrintSummaryTable_Live(t *testing.T) {
	items := []Item{
		{Name: "CONTRIBUTING.md", Action: ActionCreate},
	}
	var buf bytes.Buffer
	PrintSummaryTable(&buf, items, true)
	out := buf.String()
	if strings.Contains(out, "would") {
		t.Error("live mode should not show 'would' prefix")
	}
}
