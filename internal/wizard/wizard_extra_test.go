package wizard

import (
	"bytes"
	"fmt"
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
	}
	runBoolPromptTest(t, "PromptProjectType", PromptProjectType, cases)
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
		{"yes", "yes\n", false},
		{"n", "n\n", false},
		{"N", "N\n", false},
		{"no", "no\n", false},
		{"whitespace y", "  y  \n", true},
	}
	runBoolPromptTest(t, "ConfirmDefaults", ConfirmDefaults, cases)
}

func TestRunItems(t *testing.T) {
	tests := []struct {
		name   string
		items  []Item
		dryRun bool
	}{
		{
			name: "dry run",
			items: []Item{
				{Name: "file1.md", Action: ActionCreate, Apply: func() error { return nil }},
			},
			dryRun: true,
		},
		{
			name: "skip item",
			items: []Item{
				{Name: "file1.md", Action: ActionSkip, Apply: func() error { return nil }},
			},
			dryRun: false,
		},
		{
			name: "apply success",
			items: []Item{
				{Name: "file1.md", Action: ActionCreate, Apply: func() error { return nil }},
			},
			dryRun: false,
		},
		{
			name: "apply error non-optional",
			items: []Item{
				{Name: "file1.md", Action: ActionCreate, Apply: func() error { return fmt.Errorf("fail") }},
			},
			dryRun: false,
		},
		{
			name: "apply error optional",
			items: []Item{
				{Name: "file1.md", Action: ActionCreate, Optional: true, Apply: func() error { return fmt.Errorf("fail") }},
			},
			dryRun: false,
		},
		{
			name:   "empty items",
			items:  []Item{},
			dryRun: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RunItems(tt.items, tt.dryRun)
			if err != nil {
				t.Errorf("RunItems() unexpected error: %v", err)
			}
		})
	}
}

func TestRunInteractive(t *testing.T) {
	tests := []struct {
		name   string
		items  []Item
		input  string
		dryRun bool
	}{
		{
			name: "accept all",
			items: []Item{
				{Name: "file1.md", Action: ActionCreate, Apply: func() error { return nil }},
				{Name: "file2.md", Action: ActionCreate, Apply: func() error { return nil }},
			},
			input:  "y\ny\n",
			dryRun: false,
		},
		{
			name: "skip one",
			items: []Item{
				{Name: "file1.md", Action: ActionCreate, Apply: func() error { return nil }},
				{Name: "file2.md", Action: ActionCreate, Apply: func() error { return nil }},
			},
			input:  "y\nn\n",
			dryRun: false,
		},
		{
			name: "skip by default",
			items: []Item{
				{Name: "file1.md", Action: ActionSkip, Apply: func() error { return nil }},
				{Name: "file2.md", Action: ActionCreate, Apply: func() error { return nil }},
			},
			input:  "y\n",
			dryRun: false,
		},
		{
			name: "dry run with accept",
			items: []Item{
				{Name: "file1.md", Action: ActionCreate, Apply: func() error { return nil }},
			},
			input:  "y\n",
			dryRun: true,
		},
		{
			name: "error on optional",
			items: []Item{
				{Name: "file1.md", Action: ActionCreate, Optional: true, Apply: func() error { return fmt.Errorf("fail") }},
			},
			input:  "y\n",
			dryRun: false,
		},
		{
			name: "empty accept",
			items: []Item{
				{Name: "file1.md", Action: ActionCreate, Apply: func() error { return nil }},
			},
			input:  "\n",
			dryRun: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			err := RunInteractive(tt.items, tt.dryRun, r)
			if err != nil {
				t.Errorf("RunInteractive() unexpected error: %v", err)
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
