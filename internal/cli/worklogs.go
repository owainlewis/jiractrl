package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/owainlewis/jiractrl/internal/adf"
	"github.com/owainlewis/jiractrl/internal/jira"
)

var jiraDuration = regexp.MustCompile(`^(?:\s*[1-9]\d*\s*[wdhm]\s*)+$`)

func (a App) runWorklogs(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl worklogs list|add|update")
	}
	switch args[0] {
	case "list":
		return a.runWorklogsList(args[1:], configPath)
	case "add", "update":
		return a.runWorklogsWrite(args[0], args[1:], configPath)
	default:
		return errors.New("usage: jiractrl worklogs list|add|update")
	}
}

func (a App) runWorklogsList(args []string, configPath string) error {
	fs := newFlagSet("worklogs list")
	startAt := fs.Int("start", 0, "zero-based worklog offset")
	maxResults := fs.Int("max", 50, "maximum worklogs per Jira request")
	allResults := fs.Bool("all", false, "fetch pages up to --limit")
	limit := fs.Int("limit", 1000, "hard worklog limit when --all is set")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 {
		return errors.New("usage: jiractrl worklogs list ISSUE [--start N] [--max 50] [--all --limit 1000] [--json]")
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
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := collectWorklogs(context.Background(), client, fs.Arg(0), *startAt, *maxResults, *allResults, *limit)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	for _, worklog := range result.Worklogs {
		author := firstNonEmpty(worklog.Author.DisplayName, worklog.Author.AccountID, worklog.Author.Name, "unknown")
		fmt.Fprintf(a.Stdout, "%s  %s  %s  %s\n", worklog.ID, author, worklog.Started, worklog.TimeSpent)
		if comment := worklog.Comment.PlainText(); comment != "" {
			fmt.Fprintf(a.Stdout, "  %s\n", strings.ReplaceAll(comment, "\n", "\n  "))
		}
	}
	if result.Page.HasMore {
		fmt.Fprintf(a.Stdout, "More worklogs available at --start %d\n", result.Page.Next)
	}
	return nil
}

func collectWorklogs(ctx context.Context, client *jira.Client, issue string, startAt, maxResults int, allResults bool, limit int) (*jira.WorklogPage, error) {
	requestMax := maxResults
	if allResults {
		requestMax = min(requestMax, limit)
	}
	first := startAt
	result := &jira.WorklogPage{}
	for {
		page, err := client.Worklogs(ctx, issue, startAt, requestMax)
		if err != nil {
			return nil, err
		}
		result.Worklogs = append(result.Worklogs, page.Worklogs...)
		result.Page = page.Page
		if !allResults || !page.Page.HasMore || len(result.Worklogs) >= limit {
			break
		}
		if page.Page.Next <= startAt {
			return nil, errors.New("Jira returned a non-advancing worklog page")
		}
		startAt = page.Page.Next
		requestMax = min(maxResults, limit-len(result.Worklogs))
	}
	result.Page.StartAt = first
	result.Page.MaxResults = maxResults
	result.Page.Returned = len(result.Worklogs)
	if result.Page.HasMore {
		result.Page.Next = first + len(result.Worklogs)
	}
	return result, nil
}

func (a App) runWorklogsWrite(operation string, args []string, configPath string) error {
	fs := newFlagSet("worklogs " + operation)
	timeSpent := fs.String("time-spent", "", "Jira duration such as 1h 30m")
	started := fs.String("started", "", "RFC3339 start time")
	comment := fs.String("comment", "", "worklog comment as Markdown on Cloud")
	visibilityType := fs.String("visibility-type", "", "restriction type: role or group")
	visibilityValue := fs.String("visibility-value", "", "restriction value")
	adjust := fs.String("adjust", "", "estimate adjustment: auto, leave, new, or manual")
	newEstimate := fs.String("new-estimate", "", "new remaining estimate duration")
	reduceBy := fs.String("reduce-by", "", "manual remaining-estimate reduction")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	positionals := 1
	usage := "usage: jiractrl worklogs add ISSUE --time-spent DURATION [flags]"
	if operation == "update" {
		positionals = 2
		usage = "usage: jiractrl worklogs update ISSUE WORKLOG_ID [flags]"
	}
	reordered := args
	for range positionals {
		reordered = flagsBeforeLeadingPositional(reordered)
	}
	if err := fs.Parse(reordered); err != nil || fs.NArg() != positionals {
		return errors.New(usage)
	}
	if operation == "add" && strings.TrimSpace(*timeSpent) == "" {
		return errors.New("worklogs add requires --time-spent")
	}
	if *timeSpent != "" {
		if err := validateJiraDuration("--time-spent", *timeSpent); err != nil {
			return err
		}
	}
	query, err := worklogAdjustment(*adjust, *newEstimate, *reduceBy, operation == "add")
	if err != nil {
		return err
	}
	payload := map[string]any{}
	if *timeSpent != "" {
		payload["timeSpent"] = strings.TrimSpace(*timeSpent)
	}
	if strings.TrimSpace(*started) != "" {
		value, err := time.Parse(time.RFC3339, *started)
		if err != nil {
			return fmt.Errorf("--started must be RFC3339: %w", err)
		}
		payload["started"] = value.Format("2006-01-02T15:04:05.000-0700")
	}
	visibility, err := commentVisibility(*visibilityType, *visibilityValue)
	if err != nil {
		return err
	}
	if visibility != nil {
		payload["visibility"] = visibility
	}
	if operation == "update" && len(payload) == 0 && strings.TrimSpace(*comment) == "" && len(query) == 0 {
		return errors.New("no worklog updates requested")
	}

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	deployment, err := client.Deployment(context.Background())
	if err != nil {
		return err
	}
	if strings.TrimSpace(*comment) != "" {
		payload["comment"] = *comment
		if deployment == jira.DeploymentCloud {
			payload["comment"] = adf.FromMarkdown(*comment)
		}
	}

	var result *jira.Worklog
	if operation == "add" {
		result, err = client.AddWorklog(context.Background(), fs.Arg(0), payload, query)
	} else {
		result, err = client.UpdateWorklog(context.Background(), fs.Arg(0), fs.Arg(1), payload, query)
	}
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	action := "Added"
	if operation == "update" {
		action = "Updated"
	}
	fmt.Fprintf(a.Stdout, "%s worklog %s on %s\n", action, result.ID, fs.Arg(0))
	return nil
}

func validateJiraDuration(flagName, value string) error {
	if !jiraDuration.MatchString(strings.TrimSpace(value)) {
		return fmt.Errorf("%s must be a positive Jira duration such as 1h 30m", flagName)
	}
	return nil
}

func worklogAdjustment(adjust, newEstimate, reduceBy string, allowManual bool) (url.Values, error) {
	adjust = strings.ToLower(strings.TrimSpace(adjust))
	newEstimate = strings.TrimSpace(newEstimate)
	reduceBy = strings.TrimSpace(reduceBy)
	if adjust == "" {
		if newEstimate != "" || reduceBy != "" {
			return nil, errors.New("--new-estimate and --reduce-by require --adjust")
		}
		return url.Values{}, nil
	}
	if adjust != "auto" && adjust != "leave" && adjust != "new" && adjust != "manual" {
		return nil, errors.New("--adjust must be auto, leave, new, or manual")
	}
	if adjust == "manual" && !allowManual {
		return nil, errors.New("--adjust manual is not supported for worklog update")
	}
	if adjust == "new" {
		if newEstimate == "" || reduceBy != "" {
			return nil, errors.New("--adjust new requires only --new-estimate")
		}
		if err := validateJiraDuration("--new-estimate", newEstimate); err != nil {
			return nil, err
		}
	} else if adjust == "manual" {
		if reduceBy == "" || newEstimate != "" {
			return nil, errors.New("--adjust manual requires only --reduce-by")
		}
		if err := validateJiraDuration("--reduce-by", reduceBy); err != nil {
			return nil, err
		}
	} else if newEstimate != "" || reduceBy != "" {
		return nil, fmt.Errorf("--adjust %s cannot use --new-estimate or --reduce-by", adjust)
	}
	query := url.Values{"adjustEstimate": {adjust}}
	if newEstimate != "" {
		query.Set("newEstimate", newEstimate)
	}
	if reduceBy != "" {
		query.Set("reduceBy", reduceBy)
	}
	return query, nil
}
