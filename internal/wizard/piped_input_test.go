package wizard

import (
	"bytes"
	"strings"
	"testing"
)

// TestPipedAnswersSurviveAcrossPrompts guards the scanner-lookahead fix: each
// prompt must consume exactly its own line so answers piped in one write reach
// the prompts they were written for. Previously every prompt built a fresh
// bufio.Scanner; the first scanner buffered the whole input and later prompts
// saw EOF, silently taking defaults — declined items got applied.
func TestPipedAnswersSurviveAcrossPrompts(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		decline  bool  // ConfirmDefaults answer
		wantActs []Action
	}{
		{
			name:     "decline then decline both items",
			input:    "n\nn\nn\n",
			decline:  false,
			wantActs: []Action{ActionSkip, ActionSkip},
		},
		{
			name:     "accept then accept both items",
			input:    "\n\n\n",
			decline:  true,
			wantActs: []Action{ActionCreate, ActionCreate},
		},
		{
			name:     "decline then mixed answers",
			input:    "n\ny\nn\n",
			decline:  false,
			wantActs: []Action{ActionCreate, ActionSkip},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := []Item{
				{Name: "a.md", Action: ActionCreate},
				{Name: "b.md", Action: ActionCreate},
			}
			r := strings.NewReader(tt.input)
			var out bytes.Buffer
			if got := ConfirmDefaults(r, &out); got != tt.decline {
				t.Fatalf("ConfirmDefaults()=%v, want %v", got, tt.decline)
			}
			SelectInteractive(items, r)
			for i, want := range tt.wantActs {
				if items[i].Action != want {
					t.Errorf("item %d Action=%v, want %v", i, items[i].Action, want)
				}
			}
		})
	}
}

// TestAnswersSharedAcrossDistinctPrompts verifies line consumption across
// prompts that are not part of the same call chain (project type → security →
// confirm → select), as sequenced by cmd/apply.
func TestAnswersSharedAcrossDistinctPrompts(t *testing.T) {
	r := strings.NewReader("team\ny\nn\ny\n")
	var out bytes.Buffer
	if PromptProjectType(r, &out) {
		t.Error("PromptProjectType: expected team (false)")
	}
	if !PromptAdvancedSecurity(r, &out) {
		t.Error("PromptAdvancedSecurity: expected yes (true)")
	}
	if ConfirmDefaults(r, &out) {
		t.Error("ConfirmDefaults: expected n (false)")
	}
	items := []Item{{Name: "a.md", Action: ActionCreate}}
	SelectInteractive(items, r)
	if items[0].Action != ActionCreate {
		t.Errorf("SelectInteractive: expected item accepted, got %v", items[0].Action)
	}
}
