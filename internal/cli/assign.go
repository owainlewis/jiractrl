package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/owainlewis/jiractrl/internal/jira"
)

func (a App) runAssign(args []string, configPath string) error {
	fs := newFlagSet("assign")
	accountID := fs.String("account-id", "", "exact assignable Jira Cloud account ID")
	user := fs.String("user", "", "exact Cloud display name/email or Data Center username")
	unassign := fs.Bool("unassign", false, "remove the current assignee")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 {
		return errors.New("usage: jiractrl assign ISSUE (--account-id ID | --user USER | --unassign) [--json]")
	}
	choices := 0
	if strings.TrimSpace(*accountID) != "" {
		choices++
	}
	if strings.TrimSpace(*user) != "" {
		choices++
	}
	if *unassign {
		choices++
	}
	if choices != 1 {
		return errors.New("use exactly one of --account-id, --user, or --unassign")
	}

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	deployment, err := client.Deployment(context.Background())
	if err != nil {
		return err
	}
	resolvedAccountID := strings.TrimSpace(*accountID)
	resolvedUser := ""
	if resolvedAccountID != "" {
		resolved, err := client.ResolveAssignableAccountID(context.Background(), fs.Arg(0), resolvedAccountID)
		if err != nil {
			return err
		}
		resolvedAccountID = resolved.AccountID
	}
	if strings.TrimSpace(*user) != "" {
		resolved, err := client.ResolveAssignableUser(context.Background(), fs.Arg(0), *user)
		if err != nil {
			return err
		}
		if deployment == jira.DeploymentCloud {
			resolvedAccountID = resolved.AccountID
		} else {
			resolvedUser = resolved.Name
		}
	}

	receipt, err := client.AssignIssue(
		context.Background(), fs.Arg(0), resolvedAccountID, resolvedUser, *unassign,
	)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, receipt)
	}
	if receipt.Unassigned {
		fmt.Fprintf(a.Stdout, "Unassigned %s\n", fs.Arg(0))
		return nil
	}
	identity := firstNonEmpty(receipt.AccountID, receipt.User)
	fmt.Fprintf(a.Stdout, "Assigned %s to %s\n", fs.Arg(0), identity)
	return nil
}
