package wizard

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// PrintSummaryTable writes the action plan table to w.
// live=false uses dry-run labels; live=true uses live labels.
func PrintSummaryTable(w io.Writer, items []Item, live bool) {
	_, _ = fmt.Fprintf(w, "%-45s  %s\n", "Setting / File", "Action")
	_, _ = fmt.Fprintf(w, "%-45s  %s\n", strings.Repeat("-", 45), strings.Repeat("-", 20))
	for _, item := range items {
		label := item.DryRunLabel()
		if live {
			label = item.LiveLabel()
		}
		_, _ = fmt.Fprintf(w, "%-45s  %s\n", item.Name, label)
	}
}

// PromptProjectType asks whether the repo is solo or team and returns true for solo.
// Only called when branch protection will actually be applied (ActionCreate or ActionUpgrade).
func PromptProjectType(r io.Reader, w io.Writer) bool {
	_, _ = fmt.Fprint(w, "Project type? [solo/team] (default: solo): ")
	scanner := bufio.NewScanner(r)
	scanner.Scan()
	input := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return input == "" || input == "solo" || input == "s"
}

// PromptAdvancedSecurity asks whether to enable paid GHAS features on a
// private/internal repo. Default is no.
func PromptAdvancedSecurity(r io.Reader, w io.Writer) bool {
	_, _ = fmt.Fprint(w, "Enable GitHub Advanced Security (secret scanning, push protection)? Requires a paid license. [y/N]: ")
	scanner := bufio.NewScanner(r)
	scanner.Scan()
	input := strings.TrimSpace(scanner.Text())
	return strings.EqualFold(input, "y") || strings.EqualFold(input, "yes")
}

// ConfirmDefaults prompts "Apply all defaults? [Y/n]" and returns true if the
// user accepts (empty input or 'y'/'Y').
func ConfirmDefaults(r io.Reader, w io.Writer) bool {
	_, _ = fmt.Fprint(w, "\nApply all defaults? [Y/n]: ")
	scanner := bufio.NewScanner(r)
	scanner.Scan()
	input := strings.TrimSpace(scanner.Text())
	return input == "" || strings.EqualFold(input, "y")
}

// ShouldSkip reports whether item should be skipped rather than applied
// (already exists) and prints a "skipped" line so the user sees the decision.
func ShouldSkip(item Item) bool {
	if item.IsSkip() {
		fmt.Printf("  %-45s  skipped\n", item.Name)
		return true
	}
	return false
}

// PlanSummary returns a human-readable counts summary of the item plan,
// e.g. "3 to create, 1 to update, 2 already exist". Returns "nothing to do"
// when the plan is entirely skips.
func PlanSummary(items []Item) string {
	var toCreate, toUpdate, toUpgrade, alreadyExist int
	for _, it := range items {
		switch it.Action {
		case ActionSkip:
			alreadyExist++
		case ActionUpdate:
			toUpdate++
		case ActionUpgrade:
			toUpgrade++
		default:
			toCreate++
		}
	}
	var parts []string
	if toCreate > 0 {
		parts = append(parts, fmt.Sprintf("%d to create", toCreate))
	}
	if toUpdate > 0 {
		parts = append(parts, fmt.Sprintf("%d to update", toUpdate))
	}
	if toUpgrade > 0 {
		parts = append(parts, fmt.Sprintf("%d to upgrade", toUpgrade))
	}
	if alreadyExist > 0 {
		parts = append(parts, fmt.Sprintf("%d already exist", alreadyExist))
	}
	if len(parts) == 0 {
		return "nothing to do"
	}
	return strings.Join(parts, ", ")
}

// SelectInteractive walks through each non-skipped item asking for confirmation.
// Declined items are marked ActionSkip so applyItems (including --pr and 409
// fallback) can run the remaining plan. Apply is not called here.
func SelectInteractive(items []Item, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for i := range items {
		if items[i].IsSkip() {
			fmt.Printf("  %-45s  already exists — skip\n", items[i].Name)
			continue
		}
		fmt.Printf("\n[%d/%d] %s (%s)\n", i+1, len(items), items[i].Name, items[i].LiveLabel())
		fmt.Print("  Apply? [Y/n]: ")
		scanner.Scan()
		input := strings.TrimSpace(scanner.Text())
		if input != "" && !strings.EqualFold(input, "y") {
			fmt.Printf("  %-45s  skipped by user\n", items[i].Name)
			items[i].Action = ActionSkip
		}
	}
}

// PrintItemError formats an error for a wizard item to w.
// Optional items show a warning; required items show an error. The real error
// is always surfaced so the cause isn't guessed for the user.
func PrintItemError(w io.Writer, item Item, err error) {
	if item.Optional {
		_, _ = fmt.Fprintf(w, "  %-45s  ⚠ optional — failed: %v\n", item.Name, err)
	} else {
		_, _ = fmt.Fprintf(w, "  %-45s  ✗ %v\n", item.Name, err)
	}
}
