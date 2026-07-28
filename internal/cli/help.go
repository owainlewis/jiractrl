package cli

import (
	"fmt"
	"io"
)

func printUsage(w io.Writer) {
	fmt.Fprint(w, `jiractrl - a control plane for Jira for AI agents

Usage:
  jiractrl [--config config.toml] COMMAND [flags]
  jiractrl help [COMMAND]

Read:
  auth check                 Check Jira credentials
  server-info                Detect Jira deployment and capabilities
  search --jql JQL           Search issues with JQL
  search --profile NAME      Search using a configured profile
  get ISSUE                  Fetch one issue
  fields                     List Jira fields
  issue-fields ISSUE         List populated fields on one issue
  transitions ISSUE          List available workflow transitions
  profiles list              List configured profiles
  profiles show NAME         Show one configured profile
  triage --jql JQL           Dry-run issue quality triage

Write:
  create --project KEY --summary TEXT [--type Task] [--description TEXT]
  update ISSUE [--summary TEXT] [--description TEXT] [--field name=value]
  comment ISSUE --body TEXT
  transition ISSUE --to NAME_OR_ID

Global flags:
  --config PATH              Use a specific config.toml

Agent notes:
  Prefer --json for machine parsing.
  Use profiles to avoid repeating common JQL.
  Use fields and issue-fields before writing custom fields.
  Write commands mutate Jira; read commands do not.

Environment:
  JIRACTRL_CONFIG            Optional config path
  JIRACTRL_BASE_URL          Jira base URL
  JIRACTRL_TOKEN             Jira personal access token
  JIRACTRL_EMAIL             Jira Cloud account email (enables Basic auth)
  JIRACTRL_DEPLOYMENT        auto, cloud, or data_center
  JIRA_BASE_URL              Fallback Jira base URL
  JIRA_PAT / JIRA_TOKEN      Fallback Jira token

Examples:
  jiractrl auth check
  jiractrl search --profile my_open --json
  jiractrl get MYPROJ-123 --json
  jiractrl fields --json
  jiractrl comment MYPROJ-123 --body 'Following up with context.'
`)
}

func printCommandHelp(w io.Writer, command string) {
	switch command {
	case "auth":
		fmt.Fprint(w, `Usage:
  jiractrl auth check

Checks the configured Jira base URL and token.
`)
	case "search", "list":
		fmt.Fprint(w, `Usage:
  jiractrl search --jql 'project = MYPROJ ORDER BY updated DESC' [--max 20] [--fields summary,status] [--json]
  jiractrl search --profile my_open [--json]

Use --json for agent workflows. Profiles come from config.toml.
`)
	case "server-info":
		fmt.Fprint(w, `Usage:
  jiractrl server-info [--json]

Reports the detected or configured Jira deployment and known product
capabilities. Set jira.deployment when automatic detection is blocked.
`)
	case "get":
		fmt.Fprint(w, `Usage:
  jiractrl get ISSUE-123 [--fields summary,description,status] [--json]

Fetches one issue. Use --json for full structured output.
`)
	case "create":
		fmt.Fprint(w, `Usage:
  jiractrl create --project MYPROJ --type Task --summary 'Summary' [--description 'Body'] [--description-file body.md] [--json]

Creates a Jira issue. --project and --summary are required.
`)
	case "update":
		fmt.Fprint(w, `Usage:
  jiractrl update ISSUE-123 [--summary 'New summary'] [--description 'Body'] [--description-file body.md] [--field customfield_12345=value]

Updates issue fields. Repeat --field for multiple raw field assignments.
`)
	case "comment":
		fmt.Fprint(w, `Usage:
  jiractrl comment ISSUE-123 --body 'Comment'
  jiractrl comment ISSUE-123 --body-file comment.md

Adds a comment to an issue.
`)
	case "transitions":
		fmt.Fprint(w, `Usage:
  jiractrl transitions ISSUE-123 [--json]

Lists available workflow transitions for an issue.
`)
	case "transition":
		fmt.Fprint(w, `Usage:
  jiractrl transition ISSUE-123 --to 'In Progress'
  jiractrl transition ISSUE-123 --to 31

Transitions an issue by transition name or ID.
`)
	case "fields":
		fmt.Fprint(w, `Usage:
  jiractrl fields [--json]

Lists Jira fields. Use this to discover custom field IDs.
`)
	case "issue-fields":
		fmt.Fprint(w, `Usage:
  jiractrl issue-fields ISSUE-123 [--json]

Shows populated fields on one issue. Useful before writing custom fields.
`)
	case "profiles":
		fmt.Fprint(w, `Usage:
  jiractrl profiles list
  jiractrl profiles show NAME

Profiles are saved JQL/default bundles in config.toml.
`)
	case "triage":
		fmt.Fprint(w, `Usage:
  jiractrl triage --jql 'project = MYPROJ AND statusCategory != Done' [--max 10] [--json]

Runs read-only heuristics to identify weak issue descriptions, missing owners, and incident follow-up signals.
`)
	default:
		fmt.Fprintf(w, "No help for %q.\n\n", command)
		printUsage(w)
	}
}
