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
  auth check [--json]        Check Jira credentials
  server-info                Detect Jira deployment and capabilities
  search --jql JQL           Search issues with JQL
  search --profile NAME      Search using a configured profile
  get ISSUE                  Fetch one issue
  fields                     List Jira fields
  issue-fields ISSUE         List populated fields on one issue
  transitions ISSUE          List available workflow transitions
  profiles list [--json]     List configured profiles
  profiles show NAME [--json] Show one configured profile
  projects list              Page through visible projects
  projects get KEY           Look up one project
  projects issue-types KEY   List valid issue types
  meta create                Inspect create fields and allowed values
  meta edit ISSUE            Inspect editable fields
  users assignable           Find valid assignees
  comments list ISSUE        Page through all issue comments
  links types                Discover issue link types and directions
  links list ISSUE           List normalized inward/outward links
  attachments list ISSUE     List issue attachments
  changelog ISSUE            Page and filter issue history
  triage --jql JQL           Dry-run issue quality triage

Write:
  create (--project KEY --summary TEXT | --input FILE|-)
  update ISSUE [--summary TEXT | --input FILE|-]
  comments add ISSUE (--body TEXT | --input FILE|-)
  comments update ISSUE ID (--body TEXT | --input FILE|-)
  comments remove ISSUE ID
  comment ISSUE --body TEXT  Alias for comments add
  links add --type NAME --outward ISSUE --inward ISSUE
  links remove LINK_ID
  attachments upload ISSUE --file PATH
  attachments download ID --output PATH
  attachments remove ID
  transition ISSUE (--to NAME_OR_ID | --input FILE|-)

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
  jiractrl auth check --json
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
  jiractrl auth check [--json]

Checks the configured Jira base URL and token.
`)
	case "search", "list":
		fmt.Fprint(w, `Usage:
  jiractrl search --jql 'project = MYPROJ ORDER BY updated DESC' [--max 20] [--fields summary,status] [--json|--raw-json]
  jiractrl search --profile my_open [--json]
  jiractrl search --profile my_open --all --limit 500 [--json]
  jiractrl search --jql 'project = MYPROJ' --cursor CURSOR
  jiractrl search --jql 'key = MYPROJ-123' --reconcile 10001

Use --json for agent workflows. Profiles come from config.toml.
--max controls the Jira page size. --all follows continuation cursors up to
the hard --limit, which defaults to 1000 and cannot exceed 10000.
Pass the opaque page.next value back through --cursor to continue a search.
Repeat --reconcile with numeric Jira Cloud issue IDs when a search must see
recent writes. Jira Cloud accepts at most 50 IDs. Reconciliation is not
available on Data Center.
--raw-json returns one exact Jira page and cannot be combined with --all.
`)
	case "server-info":
		fmt.Fprint(w, `Usage:
  jiractrl server-info [--json]

Reports the detected or configured Jira deployment and known product
capabilities. Set jira.deployment when automatic detection is blocked.
`)
	case "get":
		fmt.Fprint(w, `Usage:
  jiractrl get ISSUE-123 [--fields summary,description,status] [--json|--raw-json]

Fetches one issue. Use --json for the stable envelope or --raw-json for the
exact Jira response.
`)
	case "create":
		fmt.Fprint(w, `Usage:
  jiractrl create --project MYPROJ --type Task --summary 'Summary' [--description 'Body'] [--description-file body.md] [--dry-run] [--json]
  jiractrl create --input issue.json [--dry-run] [--json]
  jiractrl create --input - [--dry-run] [--json]

Creates a Jira issue. Structured input accepts fields, update, transition,
properties, and historyMetadata. Do not combine --input with convenience flags.
--dry-run prints the resolved request and does not send the mutation.
`)
	case "update":
		fmt.Fprint(w, `Usage:
  jiractrl update ISSUE-123 [--summary 'New summary'] [--description 'Body'] [--description-file body.md] [--field customfield_12345=value] [--dry-run] [--json]
  jiractrl update ISSUE-123 --input update.json [--dry-run] [--json]
  jiractrl update ISSUE-123 --input - [--dry-run] [--json]

Updates issue fields. Repeat --field for multiple raw field assignments.
Structured input accepts fields, update, properties, and historyMetadata.
Do not combine --input with convenience flags. --dry-run does not mutate Jira.
`)
	case "comment":
		fmt.Fprint(w, `Usage:
  jiractrl comment ISSUE-123 --body 'Comment'
  jiractrl comment ISSUE-123 --body-file comment.md [--json]

Alias for comments add.
`)
	case "comments":
		fmt.Fprint(w, `Usage:
  jiractrl comments list ISSUE [--start N] [--max 50] [--all --limit 1000] [--json]
  jiractrl comments add ISSUE (--body TEXT|--body-file FILE|--input FILE|-) [--visibility-type role|group --visibility-value VALUE] [--json]
  jiractrl comments update ISSUE COMMENT_ID (--body TEXT|--body-file FILE|--input FILE|-) [--visibility-type role|group --visibility-value VALUE] [--json]
  jiractrl comments remove ISSUE COMMENT_ID [--json]

Cloud convenience bodies support headings, paragraphs, ordered and unordered
lists, links, fenced code blocks, inline code, bold, and italic Markdown. They
are converted to ADF. Data Center bodies remain strings. Structured input
accepts body plus optional visibility; use it to supply exact Cloud ADF.
Comment listing is independently paged. --all follows pages only up to --limit.
The legacy comment command is an alias for comments add.
`)
	case "links":
		fmt.Fprint(w, `Usage:
  jiractrl links types [--json]
  jiractrl links list ISSUE [--json]
  jiractrl links add --type NAME --outward ISSUE --inward ISSUE [--json]
  jiractrl links remove LINK_ID [--json]

List output names inward or outward direction explicitly. Discover exact type
names and labels before adding. Jira returns success for duplicate links and
does not return the new link ID; add receipts expose those semantics.
`)
	case "attachments":
		fmt.Fprint(w, `Usage:
  jiractrl attachments list ISSUE [--json]
  jiractrl attachments upload ISSUE --file PATH [--json]
  jiractrl attachments download ATTACHMENT_ID --output PATH [--overwrite] [--json]
  jiractrl attachments remove ATTACHMENT_ID [--json]

Uploads stream one regular file as Jira multipart field "file". Downloads
require an explicit destination, reject parent traversal, and refuse an
existing destination unless --overwrite is set. Remove is a one-shot write.
`)
	case "changelog":
		fmt.Fprint(w, `Usage:
  jiractrl changelog ISSUE [--field FIELD] [--start N] [--max 50] [--all --limit 1000] [--json]

Reads the dedicated paged changelog. Repeat --field or use comma-separated
field names or IDs to retain matching items. --all scans at most --limit raw
histories even when filtering removes entries.
`)
	case "transitions":
		fmt.Fprint(w, `Usage:
  jiractrl transitions ISSUE-123 [--json]

Lists available workflow transitions for an issue.
`)
	case "transition":
		fmt.Fprint(w, `Usage:
  jiractrl transition ISSUE-123 --to 'In Progress' [--dry-run] [--json]
  jiractrl transition ISSUE-123 --input transition.json [--dry-run] [--json]
  jiractrl transition ISSUE-123 --to 31 --input transition-fields.json [--dry-run] [--json]

Structured input accepts transition, fields, update, properties, and
historyMetadata. transition.id must be a string. --to may be combined with
input that omits transition, but conflicts with input that already sets it.
--dry-run does not send the transition.
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
  jiractrl profiles list [--json]
  jiractrl profiles show NAME [--json]

Profiles are saved JQL/default bundles in config.toml.
`)
	case "projects":
		fmt.Fprint(w, `Usage:
  jiractrl projects list [--query TEXT] [--start N] [--max 50] [--json]
  jiractrl projects get KEY [--json]
  jiractrl projects issue-types KEY [--start N] [--max 50] [--json]

Discovers visible projects and valid issue types. JSON pages include
startAt, maxResults, returned, total when Jira supplies it, next, and hasMore.
`)
	case "meta":
		fmt.Fprint(w, `Usage:
  jiractrl meta create --project KEY --type ID_OR_NAME [--start N] [--max 50] [--json]
  jiractrl meta edit ISSUE [--json]

Returns field IDs, names, required flags, schemas, defaults, and allowed
values. Duplicate issue-type names return candidates; pass the issue-type ID.
`)
	case "users":
		fmt.Fprint(w, `Usage:
  jiractrl users assignable --project KEY [--query TEXT] [--start N] [--max 50] [--json]
  jiractrl users assignable --issue ISSUE [--query TEXT] [--start N] [--max 50] [--json]

Finds assignable users in one scope. Cloud returns accountId. Data Center
returns its distinct key and name identities.
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
