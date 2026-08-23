package wizard

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// runBoolPromptTest consolidates the test loop for prompt functions that return bool.
// fn is the function under test, cases are the test table rows.
func runBoolPromptTest(t *testing.T, name string, fn func(io.Reader, io.Writer) bool, cases []struct {
	name  string
	input string
	want  bool
}) {
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			var buf bytes.Buffer
			got := fn(r, &buf)
			if got != tt.want {
				t.Errorf("%s(%q) = %v, want %v", name, tt.input, got, tt.want)
			}
		})
	}
}

func TestPromptProjectType(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool // true = solo
	}{
		{"empty input", "\n", true},
		{"solo", "solo\n", true},
		{"s", "s\n", true},
		{"SOLO", "SOLO\n", true},
		{"team", "team\n", false},
		{"t", "t\n", false},
		{"TEAM", "TEAM\n", false},
		{"whitespace solo", "  solo  \n", true},
		{"whitespace team", "  team  \n", false},
		// Typos and garbage must fall back to the advertised solo default,
		// never silently enable team-only requirements.
		{"typo soloo", "soloo\n", true},
		{"garbage", "1\n", true},
		{"yes", "yes\n", true},
	}
	runBoolPromptTest(t, "PromptProjectType", PromptProjectType, cases)
}

func TestPromptAdvancedSecurity(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty input", "\n", false},
		{"n", "n\n", false},
		{"N", "N\n", false},
		{"y", "y\n", true},
		{"Y", "Y\n", true},
		{"yes", "yes\n", true},
		{"no", "no\n", false},
	}
	runBoolPromptTest(t, "PromptAdvancedSecurity", PromptAdvancedSecurity, cases)
}

func TestConfirmDefaults(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty input", "\n", true},
		{"y", "y\n", true},
		{"Y", "Y\n", true},
		{"yes", "yes\n", true},
		{"YES", "YES\n", true},
		{"n", "n\n", false},
		{"N", "N\n", false},
		{"no", "no\n", false},
		{"whitespace y", "  y  \n", true},
	}
	runBoolPromptTest(t, "ConfirmDefaults", ConfirmDefaults, cases)
}

func TestSelectInteractive(t *testing.T) {
	tests := []struct {
		name      string
		items     []Item
		input     string
		wantActs  []Action
		wantApply int
	}{
		{
			name: "accept all",
			items: []Item{
				{Name: "file1.md", Action: ActionCreate},
				{Name: "file2.md", Action: ActionCreate},
			},
			input:    "y\ny\n",
			wantActs: []Action{ActionCreate, ActionCreate},
		},
		{
			name: "skip one",
			items: []Item{
				{Name: "file1.md", Action: ActionCreate},
				{Name: "file2.md", Action: ActionCreate},
			},
			input:    "y\nn\n",
			wantActs: []Action{ActionCreate, ActionSkip},
		},
		{
			name: "already skip stays skip",
			items: []Item{
				{Name: "file1.md", Action: ActionSkip},
				{Name: "file2.md", Action: ActionCreate},
			},
			input:    "y\n",
			wantActs: []Action{ActionSkip, ActionCreate},
		},
		{
			name: "empty accept",
			items: []Item{
				{Name: "file1.md", Action: ActionCreate},
			},
			input:    "\n",
			wantActs: []Action{ActionCreate},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applied := 0
			for i := range tt.items {
				tt.items[i].Apply = func() error { applied++; return nil }
			}
			SelectInteractive(tt.items, strings.NewReader(tt.input))
			if applied != 0 {
				t.Errorf("SelectInteractive must not apply, got %d calls", applied)
			}
			if len(tt.items) != len(tt.wantActs) {
				t.Fatalf("item count %d, want %d", len(tt.items), len(tt.wantActs))
			}
			for i, want := range tt.wantActs {
				if tt.items[i].Action != want {
					t.Errorf("item %d Action=%v, want %v", i, tt.items[i].Action, want)
				}
			}
		})
	}
}

func TestIsSkip(t *testing.T) {
	tests := []struct {
		action Action
		want   bool
	}{
		{ActionSkip, true},
		{ActionCreate, false},
		{ActionUpdate, false},
		{ActionUpgrade, false},
	}
	for _, tt := range tests {
		item := Item{Action: tt.action}
		if item.IsSkip() != tt.want {
			t.Errorf("isSkip(%v) = %v, want %v", tt.action, item.IsSkip(), tt.want)
		}
	}
}

// TestShouldSkip verifies the skip decision logic: skip items are always
// reported skip, live items are not skipped.
func TestShouldSkip(t *testing.T) {
	tests := []struct {
		name string
		item Item
		want bool
	}{
		{"skip item", Item{Action: ActionSkip}, true},
		{"live create", Item{Action: ActionCreate}, false},
		{"live update", Item{Action: ActionUpdate}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldSkip(tt.item); got != tt.want {
				t.Errorf("ShouldSkip() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanSummary(t *testing.T) {
	items := []Item{
		{Name: "a.md", Action: ActionCreate},
		{Name: "b.md", Action: ActionCreate},
		{Name: "c.md", Action: ActionUpdate},
		{Name: "d.md", Action: ActionUpgrade},
		{Name: "e.md", Action: ActionSkip},
	}
	if got := PlanSummary(items); got != "2 to create, 1 to update, 1 to upgrade, 1 already exist" {
		t.Errorf("PlanSummary() = %q", got)
	}
	if got := PlanSummary([]Item{{Name: "f.md", Action: ActionSkip}}); got != "1 already exist" {
		t.Errorf("PlanSummary() = %q", got)
	}
	if got := PlanSummary(nil); got != "nothing to do" {
		t.Errorf("PlanSummary() = %q", got)
	}
}

func TestLiveLabel(t *testing.T) {
	tests := []struct {
		action Action
		want   string
	}{
		{ActionCreate, "create"},
		{ActionUpdate, "update"},
		{ActionSkip, "already exists — skip"},
		{ActionUpgrade, "upgrade classic → ruleset"},
	}
	for _, tt := range tests {
		item := Item{Action: tt.action}
		got := item.LiveLabel()
		if got != tt.want {
			t.Errorf("LiveLabel(%v) = %q, want %q", tt.action, got, tt.want)
		}
	}
}

// Ensure PromptProjectType writes the prompt.
func TestPromptProjectType_Output(t *testing.T) {
	r := strings.NewReader("solo\n")
	var buf bytes.Buffer
	PromptProjectType(r, &buf)
	if !strings.Contains(buf.String(), "Project type") {
		t.Error("expected prompt output to contain 'Project type'")
	}
}

// Ensure ConfirmDefaults writes the prompt.
func TestConfirmDefaults_Output(t *testing.T) {
	r := strings.NewReader("\n")
	var buf bytes.Buffer
	ConfirmDefaults(r, &buf)
	if !strings.Contains(buf.String(), "Apply all defaults") {
		t.Error("expected prompt output to contain 'Apply all defaults'")
	}
}

// Given an optional item failure, the reported message must surface the actual
// error — not a hardcoded guess about the cause (e.g. GitHub Pro limits).
func TestPrintItemError_ShowsActualError(t *testing.T) {
	tests := []struct {
		name      string
		item      Item
		wantErrIn string
	}{
		{"optional item failure shows real error", Item{Name: "tag ruleset", Optional: true}, "plan limit exceeded"},
		{"required item failure shows real error", Item{Name: "settings", Optional: false}, "rate limit hit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			PrintItemError(&buf, tt.item, errors.New(tt.wantErrIn))
			out := buf.String()
			if !strings.Contains(out, tt.item.Name) {
				t.Errorf("expected output to name the item, got: %q", out)
			}
			if !strings.Contains(out, tt.wantErrIn) {
				t.Errorf("expected output to contain the real error %q, got: %q", tt.wantErrIn, out)
			}
		})
	}
}
