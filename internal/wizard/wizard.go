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

// ConfirmDefaults prompts "Apply all defaults? [Y/n]" and returns true if the
// user accepts (empty input or 'y'/'Y').
func ConfirmDefaults(r io.Reader, w io.Writer) bool {
	_, _ = fmt.Fprint(w, "\nApply all defaults? [Y/n]: ")
	scanner := bufio.NewScanner(r)
	scanner.Scan()
	input := strings.TrimSpace(scanner.Text())
	return input == "" || strings.EqualFold(input, "y")
}

// RunItems applies each item's Apply func, printing status as it goes.
func RunItems(items []Item, dryRun bool) error {
	for _, item := range items {
		if item.Action == ActionSkip {
			fmt.Printf("  %-45s  skipped\n", item.Name)
			continue
		}
		if dryRun {
			fmt.Printf("  %-45s  %s\n", item.Name, item.DryRunLabel())
			continue
		}
		fmt.Printf("  %-45s  applying...", item.Name)
		if err := item.Apply(); err != nil {
			fmt.Printf("\r  %-45s  ✗ %v\n", item.Name, err)
			continue
		}
		fmt.Printf("\r  %-45s  ✓\n", item.Name)
	}
	return nil
}

// RunInteractive walks through each non-skipped item asking for confirmation.
func RunInteractive(items []Item, dryRun bool) error {
	for i, item := range items {
		if item.Action == ActionSkip {
			fmt.Printf("  %-45s  already exists — skip\n", item.Name)
			continue
		}
		fmt.Printf("\n[%d/%d] %s (%s)\n", i+1, len(items), item.Name, item.LiveLabel())
		fmt.Print("  Apply? [Y/n]: ")
		scanner := bufio.NewScanner(os.Stdin)
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
		fmt.Printf("  %-45s  applying...", item.Name)
		if err := item.Apply(); err != nil {
			fmt.Printf("\r  %-45s  ✗ %v\n", item.Name, err)
			continue
		}
		fmt.Printf("\r  %-45s  ✓\n", item.Name)
	}
	return nil
}
