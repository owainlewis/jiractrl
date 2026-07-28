package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/owainlewis/jiractrl/internal/jira"
)

func (a App) runWatchers(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl watchers list|add|remove")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("watchers list")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil || fs.NArg() != 1 {
			return errors.New("usage: jiractrl watchers list ISSUE [--json]")
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		result, err := client.Watchers(context.Background(), fs.Arg(0))
		if err != nil {
			return err
		}
		if *jsonOutput {
			return writeSuccessJSON(a.Stdout, result)
		}
		fmt.Fprintf(a.Stdout, "%d watchers; current user watching=%t\n", result.WatchCount, result.IsWatching)
		for _, watcher := range result.Watchers {
			identity := firstNonEmpty(watcher.AccountID, watcher.Name, watcher.Key)
			fmt.Fprintf(a.Stdout, "%s  %s\n", identity, watcher.DisplayName)
		}
		return nil
	case "add", "remove":
		return a.runWatchersWrite(args[0], args[1:], configPath)
	default:
		return errors.New("usage: jiractrl watchers list|add|remove")
	}
}

func (a App) runWatchersWrite(operation string, args []string, configPath string) error {
	fs := newFlagSet("watchers " + operation)
	self := fs.Bool("self", false, "change the calling user's watch state")
	accountID := fs.String("account-id", "", "Jira Cloud account ID")
	user := fs.String("user", "", "Jira Data Center username")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 {
		return errors.New("usage: jiractrl watchers add|remove ISSUE (--self | --account-id ID | --user USER) [--json]")
	}
	choices := 0
	if *self {
		choices++
	}
	if strings.TrimSpace(*accountID) != "" {
		choices++
	}
	if strings.TrimSpace(*user) != "" {
		choices++
	}
	if choices != 1 {
		return errors.New("use exactly one of --self, --account-id, or --user")
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	deployment, err := client.Deployment(context.Background())
	if err != nil {
		return err
	}
	identity := ""
	if deployment == jira.DeploymentCloud {
		if strings.TrimSpace(*user) != "" {
			return &jira.ValidationError{Field: "user", Message: "Jira Cloud watchers require --account-id"}
		}
		identity = strings.TrimSpace(*accountID)
	} else {
		if strings.TrimSpace(*accountID) != "" {
			return &jira.ValidationError{Field: "account-id", Message: "Jira Data Center watchers require --user"}
		}
		identity = strings.TrimSpace(*user)
	}
	var receipt *jira.WatcherReceipt
	if operation == "add" {
		receipt, err = client.AddWatcher(context.Background(), fs.Arg(0), identity, *self)
	} else {
		receipt, err = client.RemoveWatcher(context.Background(), fs.Arg(0), identity, *self)
	}
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, receipt)
	}
	action := "Watching"
	if operation == "remove" {
		action = "No longer watching"
	}
	fmt.Fprintf(a.Stdout, "%s %s\n", action, fs.Arg(0))
	return nil
}
