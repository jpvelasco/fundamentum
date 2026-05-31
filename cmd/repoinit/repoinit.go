// Package repoinit implements the "fundamentum init" command.
package repoinit

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jpvelasco/fundamentum/cmd/apply"
	"github.com/jpvelasco/fundamentum/cmd/globals"
	"github.com/jpvelasco/fundamentum/internal/github"
)

// NewCmd returns the init subcommand.
func NewCmd() *cobra.Command {
	var private bool
	cmd := &cobra.Command{
		Use:   "init OWNER/REPO",
		Short: "Create a new GitHub repo and harden it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(args[0], private)
		},
	}
	cmd.Flags().BoolVar(&private, "private", true, "create as a private repo (default: true)")
	return cmd
}

func run(ownerRepo string, private bool) error {
	_, repo, err := parseOwnerRepo(ownerRepo)
	if err != nil {
		return err
	}

	if !globals.DryRun {
		client := github.NewClient(globals.Token, globals.Verbose)
		fmt.Printf("Creating repo %s...\n", ownerRepo)
		if err := client.CreateRepo(repo, private); err != nil {
			return fmt.Errorf("create repo: %w", err)
		}
		fmt.Printf("Repo created.\n\n")
	} else {
		fmt.Printf("would create repo %s\n\n", ownerRepo)
	}

	applyCmd := apply.NewCmd()
	applyCmd.SetArgs([]string{ownerRepo})
	return applyCmd.Execute()
}

func parseOwnerRepo(arg string) (string, string, error) {
	for i, c := range arg {
		if c == '/' {
			return arg[:i], arg[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid OWNER/REPO %q: expected a slash separator", arg)
}
