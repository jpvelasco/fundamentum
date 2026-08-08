package wizard

import (
	"bufio"
	"fmt"
	"io"
	"os"
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

// ConfirmDefaults prompts "Apply all defaults? [Y/n]" and returns true if the
// user accepts (empty input or 'y'/'Y').
func ConfirmDefaults(r io.Reader, w io.Writer) bool {
	_, _ = fmt.Fprint(w, "\nApply all defaults? [Y/n]: ")
	scanner := bufio.NewScanner(r)
	scanner.Scan()
	input := strings.TrimSpace(scanner.Text())
	return input == "" || strings.EqualFold(input, "y")
}

// ShouldSkipOrDryRun prints and reports whether item should be skipped rather
// than applied — either because it already exists (IsSkip) or because dryRun
// is set (prints the dry-run label instead of applying).
func ShouldSkipOrDryRun(item Item, dryRun bool) bool {
	if item.IsSkip() {
		fmt.Printf("  %-45s  skipped\n", item.Name)
		return true
	}
	if dryRun {
		fmt.Printf("  %-45s  %s\n", item.Name, item.DryRunLabel())
		return true
	}
	return false
}

// applyAndPrint runs item.Apply and prints the outcome.
func applyAndPrint(item Item) {
	if err := item.Apply(); err != nil {
		PrintItemError(os.Stdout, item, err)
		return
	}
	fmt.Printf("  %-45s  ✓\n", item.Name)
}

// RunInteractive walks through each non-skipped item asking for confirmation.
func RunInteractive(items []Item, dryRun bool, r io.Reader) error {
	scanner := bufio.NewScanner(r)
	for i, item := range items {
		if item.IsSkip() {
			fmt.Printf("  %-45s  already exists — skip\n", item.Name)
			continue
		}
		fmt.Printf("\n[%d/%d] %s (%s)\n", i+1, len(items), item.Name, item.LiveLabel())
		fmt.Print("  Apply? [Y/n]: ")
		scanner.Scan()
		input := strings.TrimSpace(scanner.Text())
		if input != "" && !strings.EqualFold(input, "y") {
			fmt.Printf("  %-45s  skipped by user\n", item.Name)
			continue
		}
		if dryRun {
			fmt.Printf("  %-45s  %s\n", item.Name, item.DryRunLabel())
			continue
		}
		applyAndPrint(item)
	}
	return nil
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
