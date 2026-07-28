package triage

import (
	"strings"

	"github.com/owainlewis/jiractrl/internal/jira"
)

const Fields = "summary,description,status,assignee,priority,issuetype,labels,created,updated,comment"

type Result struct {
	Issue            string   `json:"issue"`
	Summary          string   `json:"summary"`
	Status           string   `json:"status"`
	Assignee         string   `json:"assignee"`
	Classification   string   `json:"classification"`
	Confidence       string   `json:"confidence"`
	Reason           string   `json:"reason"`
	Missing          []string `json:"missing,omitempty"`
	SuggestedLabels  []string `json:"suggested_labels"`
	SuggestedComment string   `json:"suggested_comment"`
}

func Classify(issue jira.Issue) Result {
	description := issue.Fields.Description.PlainText()
	text := strings.ToLower(issue.Fields.Summary + "\n" + description)
	missing := missingSignals(issue, text)
	labels := []string{"ai-triaged"}
	classification := "ready-for-engineering"
	confidence := "medium"
	reason := "Issue has a usable description and enough routing context for an engineer to start."

	if containsAny(text, "sev", "incident", "outage", "customer impact", "mitigation", "rollback", "postmortem", "alert") {
		classification = "incident-followup"
		labels = append(labels, "incident-followup")
		reason = "Issue appears to be related to an incident, alert, outage, mitigation, or customer-impacting follow-up."
		if len(missing) > 0 {
			confidence = "medium"
		} else {
			confidence = "high"
		}
	} else if len(missing) >= 3 || len(description) < 120 {
		classification = "needs-more-info"
		labels = append(labels, "needs-info")
		confidence = "high"
		reason = "Issue is missing several signals usually needed for incident or engineering triage."
	} else if issue.Fields.Assignee.DisplayName == "" && issue.Fields.Comment.Total == 0 {
		classification = "stale-or-duplicate-risk"
		labels = append(labels, "stale-review")
		reason = "Issue is unassigned and has limited visible discussion, so it may need ownership or duplicate review."
	}

	return Result{
		Issue:            issue.Key,
		Summary:          issue.Fields.Summary,
		Status:           issue.Fields.Status.Name,
		Assignee:         firstNonEmpty(issue.Fields.Assignee.DisplayName, "unassigned"),
		Classification:   classification,
		Confidence:       confidence,
		Reason:           reason,
		Missing:          missing,
		SuggestedLabels:  labels,
		SuggestedComment: suggestedComment(classification, missing),
	}
}

func missingSignals(issue jira.Issue, text string) []string {
	var missing []string
	if issue.Fields.Description.PlainText() == "" {
		missing = append(missing, "description")
	}
	if !containsAny(text, "impact", "customer", "severity", "sev", "blast radius", "affected") {
		missing = append(missing, "impact")
	}
	if !containsAny(text, "region", "realm", "environment", "prod", "dev", "staging", "tenant") {
		missing = append(missing, "environment")
	}
	if !containsAny(text, "repro", "steps", "expected", "actual", "observed") {
		missing = append(missing, "reproduction_or_expected_actual")
	}
	if !containsAny(text, "log", "dashboard", "alarm", "metric", "trace", "opc-request-id", "runbook") {
		missing = append(missing, "evidence_or_logs")
	}
	if issue.Fields.Assignee.DisplayName == "" {
		missing = append(missing, "owner")
	}
	return missing
}

func suggestedComment(classification string, missing []string) string {
	switch classification {
	case "needs-more-info":
		return "Dry-run suggestion: please add the missing triage context: " + strings.Join(missing, ", ") + ". Useful details include impact, affected environment/region, reproduction or expected-vs-actual behavior, and links to logs, alarms, dashboards, or incidents."
	case "incident-followup":
		return "Dry-run suggestion: please confirm the incident/customer impact, mitigation status, affected regions, and links to alarms, dashboards, runbooks, or postmortem notes."
	case "stale-or-duplicate-risk":
		return "Dry-run suggestion: please confirm whether this still needs work, assign an owner, or link the duplicate/canonical issue."
	default:
		return "Dry-run suggestion: this looks ready for engineering triage. Confirm priority, owner, and next action."
	}
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
