package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/owainlewis/jiractrl/internal/adf"
	"github.com/owainlewis/jiractrl/internal/jira"
)

const commentsUsage = "usage: jiractrl comments list|add|update|remove ..."

func (a App) runComments(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New(commentsUsage)
	}
	switch args[0] {
	case "list":
		return a.runCommentsList(args[1:], configPath)
	case "add":
		return a.runCommentsWrite("add", args[1:], configPath)
	case "update":
		return a.runCommentsWrite("update", args[1:], configPath)
	case "remove":
		return a.runCommentsRemove(args[1:], configPath)
	default:
		return errors.New(commentsUsage)
	}
}

func (a App) runCommentsList(args []string, configPath string) error {
	fs := newFlagSet("comments list")
	startAt := fs.Int("start", 0, "zero-based result offset")
	maxResults := fs.Int("max", 50, "maximum comments per Jira request")
	allResults := fs.Bool("all", false, "fetch pages up to --limit")
	limit := fs.Int("limit", 1000, "hard comment limit when --all is set")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(flagsBeforeLeadingPositional(args)); err != nil || fs.NArg() != 1 {
		return errors.New("usage: jiractrl comments list ISSUE [--start N] [--max 50] [--all --limit 1000] [--json]")
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
	result, err := collectComments(context.Background(), client, fs.Arg(0), *startAt, *maxResults, *allResults, *limit)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	for _, comment := range result.Comments {
		author := firstNonEmpty(comment.Author.DisplayName, comment.Author.Name, comment.Author.Key, "unknown")
		fmt.Fprintf(a.Stdout, "%s  %s  %s\n", comment.ID, author, comment.Created)
		if comment.Visibility != nil {
			fmt.Fprintf(a.Stdout, "  visibility: %s=%s\n", comment.Visibility.Type, comment.Visibility.Value)
		}
		if body := comment.Body.PlainText(); body != "" {
			fmt.Fprintf(a.Stdout, "  %s\n", strings.ReplaceAll(body, "\n", "\n  "))
		}
	}
	if result.Page.HasMore {
		fmt.Fprintf(a.Stdout, "More comments available at --start %d\n", result.Page.Next)
	}
	return nil
}

func collectComments(ctx context.Context, client *jira.Client, issue string, startAt, maxResults int, allResults bool, limit int) (*jira.CommentPage, error) {
	requestMax := maxResults
	if allResults && requestMax > limit {
		requestMax = limit
	}
	first := startAt
	result := &jira.CommentPage{}
	for {
		page, err := client.Comments(ctx, issue, startAt, requestMax)
		if err != nil {
			return nil, err
		}
		result.Comments = append(result.Comments, page.Comments...)
		result.Page = page.Page
		if !allResults || !page.Page.HasMore || len(result.Comments) >= limit {
			break
		}
		if page.Page.Next <= startAt {
			return nil, errors.New("Jira returned a non-advancing comment page")
		}
		startAt = page.Page.Next
		requestMax = min(maxResults, limit-len(result.Comments))
	}
	result.Page.StartAt = first
	result.Page.MaxResults = maxResults
	result.Page.Returned = len(result.Comments)
	if result.Page.HasMore {
		result.Page.Next = first + len(result.Comments)
	}
	return result, nil
}

func (a App) runCommentsWrite(operation string, args []string, configPath string) error {
	fs := newFlagSet("comments " + operation)
	body := fs.String("body", "", "comment body as Markdown on Cloud")
	bodyFile := fs.String("body-file", "", "path to comment body file")
	input := fs.String("input", "", "structured JSON request path or - for stdin")
	visibilityType := fs.String("visibility-type", "", "comment restriction type: role or group")
	visibilityValue := fs.String("visibility-value", "", "comment restriction value")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	positionals := 1
	usage := "usage: jiractrl comments add ISSUE (--body TEXT|--body-file FILE|--input FILE|-) [--visibility-type TYPE --visibility-value VALUE] [--json]"
	if operation == "update" {
		positionals = 2
		usage = "usage: jiractrl comments update ISSUE COMMENT_ID (--body TEXT|--body-file FILE|--input FILE|-) [--visibility-type TYPE --visibility-value VALUE] [--json]"
	}
	reordered := args
	for range positionals {
		reordered = flagsBeforeLeadingPositional(reordered)
	}
	if err := fs.Parse(reordered); err != nil || fs.NArg() != positionals {
		return errors.New(usage)
	}
	if strings.TrimSpace(fs.Arg(0)) == "" || (positionals == 2 && strings.TrimSpace(fs.Arg(1)) == "") {
		return errors.New(usage)
	}

	usingConvenience := flagWasSet(fs, "body", "body-file", "visibility-type", "visibility-value")
	if *input != "" && usingConvenience {
		return errors.New("--input cannot be combined with body or visibility flags")
	}
	if *input == "" && !flagWasSet(fs, "body", "body-file") {
		return errors.New("missing required --body, --body-file, or --input")
	}

	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	deployment, err := client.Deployment(context.Background())
	if err != nil {
		return err
	}

	var payload map[string]any
	if *input != "" {
		payload, err = a.readMutationInput(*input)
		if err != nil {
			return err
		}
		if err := validateCommentPayload(payload, deployment); err != nil {
			return err
		}
	} else {
		text, err := readInlineOrFile(*body, *bodyFile)
		if err != nil {
			return err
		}
		if strings.TrimSpace(text) == "" {
			return errors.New("comment body cannot be empty")
		}
		payload = map[string]any{"body": text}
		if deployment == jira.DeploymentCloud {
			payload["body"] = adf.FromMarkdown(text)
		}
		visibility, err := commentVisibility(*visibilityType, *visibilityValue)
		if err != nil {
			return err
		}
		if visibility != nil {
			payload["visibility"] = visibility
		}
	}

	var result *jira.Comment
	if operation == "add" {
		result, err = client.AddCommentWithPayload(context.Background(), fs.Arg(0), payload)
	} else {
		result, err = client.UpdateCommentWithPayload(context.Background(), fs.Arg(0), fs.Arg(1), payload)
	}
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	if operation == "add" {
		fmt.Fprintf(a.Stdout, "Added comment %s to %s\n", result.ID, fs.Arg(0))
	} else {
		fmt.Fprintf(a.Stdout, "Updated comment %s on %s\n", fs.Arg(1), fs.Arg(0))
	}
	return nil
}

func validateCommentPayload(payload map[string]any, deployment jira.Deployment) error {
	for key := range payload {
		if key != "body" && key != "visibility" {
			return fmt.Errorf("--input field %q is not allowed for comments", key)
		}
	}
	body, ok := payload["body"]
	if !ok {
		return errors.New("--input requires body")
	}
	switch value := body.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return errors.New("--input body cannot be empty")
		}
		if deployment == jira.DeploymentCloud {
			return errors.New("--input Cloud body must be an ADF object; use --body for Markdown")
		}
	case map[string]any:
		if deployment != jira.DeploymentCloud {
			return errors.New("--input body must be a string on Jira Data Center")
		}
		if value["type"] != "doc" {
			return errors.New("--input Cloud body must be an ADF document")
		}
	default:
		return errors.New("--input body must be a string or ADF object")
	}
	if raw, ok := payload["visibility"]; ok {
		visibility, ok := raw.(map[string]any)
		if !ok {
			return errors.New("--input visibility must be an object")
		}
		kind, kindOK := visibility["type"].(string)
		value, valueOK := visibility["value"].(string)
		if !kindOK || !valueOK {
			return errors.New("--input visibility requires string type and value")
		}
		if _, err := commentVisibility(kind, value); err != nil {
			return err
		}
		if identifier, ok := visibility["identifier"]; ok {
			if _, ok := identifier.(string); !ok {
				return errors.New("--input visibility.identifier must be a string")
			}
		}
		for key := range visibility {
			if key != "type" && key != "value" && key != "identifier" {
				return fmt.Errorf("--input visibility field %q is not allowed", key)
			}
		}
	}
	return nil
}

func commentVisibility(kind, value string) (map[string]any, error) {
	kind = strings.TrimSpace(kind)
	value = strings.TrimSpace(value)
	if kind == "" && value == "" {
		return nil, nil
	}
	if kind == "" || value == "" {
		return nil, errors.New("--visibility-type and --visibility-value must be used together")
	}
	if kind != "role" && kind != "group" {
		return nil, errors.New("--visibility-type must be role or group")
	}
	return map[string]any{"type": kind, "value": value}, nil
}

func (a App) runCommentsRemove(args []string, configPath string) error {
	fs := newFlagSet("comments remove")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	reordered := flagsBeforeLeadingPositional(flagsBeforeLeadingPositional(args))
	if err := fs.Parse(reordered); err != nil || fs.NArg() != 2 {
		return errors.New("usage: jiractrl comments remove ISSUE COMMENT_ID [--json]")
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	if err := client.RemoveComment(context.Background(), fs.Arg(0), fs.Arg(1)); err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, map[string]any{
			"issue":     fs.Arg(0),
			"commentId": fs.Arg(1),
			"removed":   true,
		})
	}
	fmt.Fprintf(a.Stdout, "Removed comment %s from %s\n", fs.Arg(1), fs.Arg(0))
	return nil
}
