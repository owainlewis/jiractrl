package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/owainlewis/jiractrl/internal/jira"
)

func (a App) runProjects(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl projects list|get|issue-types")
	}
	switch args[0] {
	case "list":
		fs := newFlagSet("projects list")
		query := fs.String("query", "", "filter by project key or name")
		start := fs.Int("start", 0, "zero-based page offset")
		maxResults := fs.Int("max", 50, "maximum projects to return")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *start < 0 || *maxResults < 1 || *maxResults > 100 {
			return errors.New("usage: jiractrl projects list [--query TEXT] [--start N] [--max 50] [--json]")
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		result, err := client.Projects(context.Background(), *query, *start, *maxResults)
		if err != nil {
			return err
		}
		if *jsonOutput {
			return writeSuccessJSON(a.Stdout, result)
		}
		for _, project := range result.Projects {
			fmt.Fprintf(a.Stdout, "%s  %s\n", project.Key, project.Name)
		}
		if result.Page.HasMore {
			fmt.Fprintf(a.Stdout, "More results: --start %d\n", result.Page.Next)
		}
		return nil
	case "get":
		fs := newFlagSet("projects get")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil || fs.NArg() != 1 {
			return errors.New("usage: jiractrl projects get KEY [--json]")
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		project, err := client.Project(context.Background(), fs.Arg(0))
		if err != nil {
			return err
		}
		if *jsonOutput {
			return writeSuccessJSON(a.Stdout, project)
		}
		fmt.Fprintf(a.Stdout, "%s  %s  %s\n", project.ID, project.Key, project.Name)
		return nil
	case "issue-types":
		fs := newFlagSet("projects issue-types")
		start := fs.Int("start", 0, "zero-based page offset")
		maxResults := fs.Int("max", 50, "maximum issue types to return")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil ||
			fs.NArg() != 1 || *start < 0 || *maxResults < 1 || *maxResults > 100 {
			return errors.New("usage: jiractrl projects issue-types KEY [--start N] [--max 50] [--json]")
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		result, err := client.ProjectIssueTypes(context.Background(), fs.Arg(0), *start, *maxResults)
		if err != nil {
			return err
		}
		if *jsonOutput {
			return writeSuccessJSON(a.Stdout, result)
		}
		for _, issueType := range result.IssueTypes {
			fmt.Fprintf(a.Stdout, "%s  %s\n", issueType.ID, issueType.Name)
		}
		return nil
	default:
		return errors.New("usage: jiractrl projects list|get|issue-types")
	}
}

func (a App) runMeta(args []string, configPath string) error {
	if len(args) == 0 {
		return errors.New("usage: jiractrl meta create|edit")
	}
	switch args[0] {
	case "create":
		fs := newFlagSet("meta create")
		project := fs.String("project", "", "project key or ID")
		issueType := fs.String("type", "", "issue type ID or exact name")
		start := fs.Int("start", 0, "zero-based field offset")
		maxResults := fs.Int("max", 50, "maximum fields to return")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 ||
			strings.TrimSpace(*project) == "" || strings.TrimSpace(*issueType) == "" ||
			*start < 0 || *maxResults < 1 || *maxResults > 100 {
			return errors.New("usage: jiractrl meta create --project KEY --type ID_OR_NAME [--start N] [--max 50] [--json]")
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		result, err := client.CreateMetadata(context.Background(), *project, *issueType, *start, *maxResults)
		if err != nil {
			return err
		}
		return a.printMetadata(result, *jsonOutput)
	case "edit":
		fs := newFlagSet("meta edit")
		jsonOutput := fs.Bool("json", false, "print JSON response")
		if err := fs.Parse(flagsBeforeLeadingPositional(args[1:])); err != nil || fs.NArg() != 1 {
			return errors.New("usage: jiractrl meta edit ISSUE [--json]")
		}
		client, err := a.client(configPath, 30*time.Second)
		if err != nil {
			return err
		}
		result, err := client.EditMetadata(context.Background(), fs.Arg(0))
		if err != nil {
			return err
		}
		return a.printMetadata(result, *jsonOutput)
	default:
		return errors.New("usage: jiractrl meta create|edit")
	}
}

func (a App) printMetadata(result *jira.MetadataResponse, jsonOutput bool) error {
	if jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	for _, field := range result.Fields {
		required := ""
		if field.Required {
			required = " required"
		}
		fmt.Fprintf(a.Stdout, "%s  %s%s\n", field.ID, field.Name, required)
	}
	return nil
}

func (a App) runUsers(args []string, configPath string) error {
	if len(args) == 0 || args[0] != "assignable" {
		return errors.New("usage: jiractrl users assignable (--project KEY | --issue ISSUE) [--query TEXT] [--json]")
	}
	fs := newFlagSet("users assignable")
	project := fs.String("project", "", "scope users to a project")
	issue := fs.String("issue", "", "scope users to an issue")
	query := fs.String("query", "", "user name, email, or identity query")
	start := fs.Int("start", 0, "zero-based page offset")
	maxResults := fs.Int("max", 50, "maximum users to return")
	jsonOutput := fs.Bool("json", false, "print JSON response")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 ||
		(strings.TrimSpace(*project) == "") == (strings.TrimSpace(*issue) == "") ||
		*start < 0 || *maxResults < 1 || *maxResults > 100 {
		return errors.New("usage: jiractrl users assignable (--project KEY | --issue ISSUE) [--query TEXT] [--start N] [--max 50] [--json]")
	}
	client, err := a.client(configPath, 30*time.Second)
	if err != nil {
		return err
	}
	result, err := client.AssignableUsers(context.Background(), *project, *issue, *query, *start, *maxResults)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return writeSuccessJSON(a.Stdout, result)
	}
	for _, user := range result.Users {
		identity := firstNonEmpty(user.AccountID, user.Name, user.Key)
		fmt.Fprintf(a.Stdout, "%s  %s\n", identity, user.DisplayName)
	}
	return nil
}
