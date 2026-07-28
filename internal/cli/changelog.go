package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/owainlewis/jiractrl/internal/jira"
)

func (a App) runChangelog(args []string, configPath string) error {
	fs := newFlagSet("changelog")
	startAt := fs.Int("start", 0, "zero-based history offset")
	maxResults := fs.Int("max", 50, "maximum histories per Jira request")
	allResults := fs.Bool("all", false, "fetch pages up to --limit")
	limit := fs.Int("limit", 1000, "hard history scan limit when --all is set")
	fieldValues := multiFlag{}
	fs.Var(&fieldValues, "field", "field name or ID to retain; may be repeated or comma-separated")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 {
		return errors.New("usage: jiractrl changelog ISSUE [--field FIELD] [--start N] [--max 50] [--all --limit 1000] [--json]")
	}
	if *startAt < 0 || *maxResults < 1 || *maxResults > 100 {
		return errors.New("--start must be non-negative and --max must be between 1 and 100")
	}
	if *limit < 1 || *limit > 10000 {
		return errors.New("--limit must be between 1 and 10000")
	}
	if flagWasSet(fs, "limit") && !*allResults {
		return errors.New("--limit requires --all")
	}
	var fields []string
	for _, value := range fieldValues {
		fields = append(fields, splitCommaValues(value)...)
	}

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := collectChangelog(context.Background(), client, fs.Arg(0), *startAt, *maxResults, *allResults, *limit, fields)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	for _, history := range result.Histories {
		author := firstNonEmpty(history.Author.DisplayName, history.Author.AccountID, history.Author.Name, history.Author.Key, "unknown")
		fmt.Fprintf(a.Stdout, "%s  %s  %s\n", history.ID, author, history.Created)
		for _, item := range history.Items {
			field := firstNonEmpty(item.FieldID, item.Field)
			fmt.Fprintf(a.Stdout, "  %s: %q -> %q\n", field, item.FromValue, item.ToValue)
		}
	}
	if result.Page.HasMore {
		fmt.Fprintf(a.Stdout, "More histories available at --start %d\n", result.Page.Next)
	}
	return nil
}

func collectChangelog(ctx context.Context, client *jira.Client, issue string, startAt, maxResults int, allResults bool, limit int, fields []string) (*jira.ChangelogPage, error) {
	requestMax := maxResults
	if allResults {
		requestMax = min(requestMax, limit)
	}
	first := startAt
	result := &jira.ChangelogPage{Fields: fields}
	for {
		page, err := client.Changelog(ctx, issue, startAt, requestMax, fields)
		if err != nil {
			return nil, err
		}
		result.Histories = append(result.Histories, page.Histories...)
		result.Scanned += page.Scanned
		result.Page = page.Page
		if !allResults || !page.Page.HasMore || result.Scanned >= limit {
			break
		}
		if page.Page.Next <= startAt || page.Scanned == 0 {
			return nil, errors.New("Jira returned a non-advancing changelog page")
		}
		startAt = page.Page.Next
		requestMax = min(maxResults, limit-result.Scanned)
	}
	result.Page.StartAt = first
	result.Page.MaxResults = maxResults
	result.Page.Returned = len(result.Histories)
	if result.Page.HasMore {
		result.Page.Next = first + result.Scanned
	}
	return result, nil
}
