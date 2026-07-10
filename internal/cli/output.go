package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/owainlewis/jiractrl/internal/jira"
	"github.com/owainlewis/jiractrl/internal/triage"
)

func printIssues(w io.Writer, result *jira.SearchResponse, withDescription bool) {
	if len(result.Issues) == 0 {
		fmt.Fprintln(w, "No issues found.")
		return
	}

	fmt.Fprintf(w, "Found %d issue(s)", len(result.Issues))
	if result.Total > len(result.Issues) {
		fmt.Fprintf(w, " of %d total", result.Total)
	}
	fmt.Fprintln(w, ":")

	for _, issue := range result.Issues {
		assignee := "unassigned"
		if issue.Fields.Assignee.DisplayName != "" {
			assignee = issue.Fields.Assignee.DisplayName
		}

		fmt.Fprintf(
			w,
			"%s  [%s]  %s  priority=%s  assignee=%s\n",
			issue.Key,
			firstNonEmpty(issue.Fields.Status.Name, "unknown"),
			issue.Fields.Summary,
			firstNonEmpty(issue.Fields.Priority.Name, "none"),
			assignee,
		)
		if withDescription {
			description := strings.TrimSpace(issue.Fields.Description)
			if description == "" {
				description = "(no description)"
			}
			fmt.Fprintf(w, "  description: %s\n", oneLine(description))
		}
	}
}

func printIssue(w io.Writer, issue *jira.Issue) {
	assignee := firstNonEmpty(issue.Fields.Assignee.DisplayName, "unassigned")
	fmt.Fprintf(w, "%s  [%s]  %s\n", issue.Key, firstNonEmpty(issue.Fields.Status.Name, "unknown"), issue.Fields.Summary)
	fmt.Fprintf(w, "type: %s\n", firstNonEmpty(issue.Fields.IssueType.Name, "unknown"))
	fmt.Fprintf(w, "priority: %s\n", firstNonEmpty(issue.Fields.Priority.Name, "none"))
	fmt.Fprintf(w, "assignee: %s\n", assignee)
	if issue.Fields.Created != "" {
		fmt.Fprintf(w, "created: %s\n", issue.Fields.Created)
	}
	if issue.Fields.Updated != "" {
		fmt.Fprintf(w, "updated: %s\n", issue.Fields.Updated)
	}
	if len(issue.Fields.Labels) > 0 {
		fmt.Fprintf(w, "labels: %s\n", strings.Join(issue.Fields.Labels, ", "))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Description:")
	description := strings.TrimSpace(issue.Fields.Description)
	if description == "" {
		description = "(no description)"
	}
	fmt.Fprintln(w, description)
	if issue.Fields.Comment.Total > 0 {
		fmt.Fprintf(w, "\nComments: %d\n", issue.Fields.Comment.Total)
		for _, comment := range issue.Fields.Comment.Comments {
			author := firstNonEmpty(comment.Author.DisplayName, comment.Author.Name, "unknown")
			fmt.Fprintf(w, "\n- %s", author)
			if comment.Created != "" {
				fmt.Fprintf(w, " at %s", comment.Created)
			}
			fmt.Fprintf(w, "\n%s\n", strings.TrimSpace(comment.Body))
		}
	}
}

func printTriageReport(w io.Writer, report []triage.Result) {
	if len(report) == 0 {
		fmt.Fprintln(w, "No issues found.")
		return
	}

	fmt.Fprintf(w, "Dry-run triage report for %d issue(s). No Jira updates were made.\n\n", len(report))
	for _, result := range report {
		fmt.Fprintf(w, "%s  [%s]  %s\n", result.Issue, result.Status, result.Summary)
		fmt.Fprintf(w, "  classification: %s (%s confidence)\n", result.Classification, result.Confidence)
		fmt.Fprintf(w, "  assignee: %s\n", result.Assignee)
		fmt.Fprintf(w, "  reason: %s\n", result.Reason)
		if len(result.Missing) > 0 {
			fmt.Fprintf(w, "  missing: %s\n", strings.Join(result.Missing, ", "))
		}
		fmt.Fprintf(w, "  suggested labels: %s\n", strings.Join(result.SuggestedLabels, ", "))
		fmt.Fprintf(w, "  suggested comment: %s\n\n", result.SuggestedComment)
	}
}

func addField(fields, field string) string {
	parts := strings.Split(fields, ",")
	for _, part := range parts {
		if strings.TrimSpace(part) == field {
			return fields
		}
	}
	if strings.TrimSpace(fields) == "" {
		return field
	}
	return fields + "," + field
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
