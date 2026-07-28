# jiractrl

`jiractrl` is a small command line control plane for Jira, designed for AI agents and humans who need predictable, scriptable Jira operations.

It aims to be:

- generic, with no company-specific workflow assumptions
- JSON-friendly for agents
- readable for humans
- configurable through `config.toml`
- easy to install as a single Go binary

## Install

Install the latest release on macOS or Linux. The installer verifies the archive's SHA-256 checksum before installing it:

```sh
curl -fsSL https://github.com/owainlewis/jiractrl/releases/latest/download/install.sh | sh
```

By default, this installs `jiractrl` to `$HOME/.local/bin`. Add that directory to your `PATH` if needed. Set `JIRACTRL_INSTALL_DIR` to use another directory:

```sh
curl -fsSL https://github.com/owainlewis/jiractrl/releases/latest/download/install.sh | JIRACTRL_INSTALL_DIR="$HOME/.local/bin" sh
```

Install a specific version by setting `JIRACTRL_VERSION`, including the leading `v`:

```sh
curl -fsSL https://github.com/owainlewis/jiractrl/releases/download/v0.1.0/install.sh | JIRACTRL_VERSION=v0.1.0 sh
```

Build from source:

```sh
go build -o jiractrl .
```

Go install:

```sh
go install github.com/owainlewis/jiractrl@latest
```

## Configure

Default config path:

```text
~/.config/jiractrl/config.toml
```

Example:

```toml
[jira]
base_url = "https://jira.example.com"
token = "your-personal-access-token"
deployment = "auto"
# For Jira Cloud, set email and use an API token:
# email = "you@example.com"

[defaults]
max_results = 50
output = "text"

[retry]
# Applies only to safe reads, including JQL search.
max_attempts = 3
base_delay_ms = 500
max_delay_ms = 30000

[profiles.my_open]
jql = "assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC"
fields = ["summary", "status", "assignee", "priority", "issuetype", "created", "updated"]
max_results = 50

[profiles.project_recent]
jql = "project = MYPROJ AND updated >= -7d ORDER BY updated DESC"
max_results = 100
```

Use a custom config:

```sh
jiractrl --config ./config.toml auth check
```

Environment variables can override config values:

```sh
export JIRACTRL_BASE_URL="https://jira.example.com"
export JIRACTRL_TOKEN="your-personal-access-token"
```

Set `JIRACTRL_DEPLOYMENT` to `cloud` or `data_center` when Jira's
`serverInfo` endpoint is unavailable. The default is `auto`.

`JIRA_BASE_URL`, `JIRA_PAT`, `JIRA_TOKEN`, and `JIRA_EMAIL` are also supported
as fallbacks.

Safe reads retry HTTP 429, 500, 502, 503, and 504 responses within the
configured attempt and delay budgets. Jira's `Retry-After` header is honored
up to `max_delay_ms`. Create, update, comment add/update/remove, and transition
requests are never retried automatically.

## JSON contract

Every command accepts `--json`. Successful output has one stable envelope:

```json
{"ok":true,"data":{"key":"MYPROJ-123"}}
```

Failures are written to stderr:

```json
{
  "ok": false,
  "error": {
    "kind": "rate_limited",
    "message": "jira request failed: 429 Too Many Requests",
    "status": 429,
    "retry": {
      "attempts": 3,
      "retryable": true,
      "retryAfterSeconds": 5
    }
  }
}
```

Error kinds include `local`, `transport`, `validation`, `auth`, `not_found`,
`rate_limited`, `conflict`, `server`, and `unsupported`. Jira field errors and
rate-limit metadata are included when the server provides them. Credentials
are redacted from Jira error data.

`get --raw-json` and single-page `search --raw-json` return the exact Jira
response without the envelope. Use this only when the normalized contract is
not sufficient. It cannot be combined with `search --all`.

Exit codes:

| Code | Meaning |
| ---: | --- |
| 0 | Success |
| 1 | Local, usage, config, transport, or unrecognized error |
| 2 | Jira rejected credentials, 401 or 403 |
| 3 | Resource not found, 404 |
| 4 | Jira rejected the request, other 4xx |
| 5 | Jira server failure, 5xx |
| 6 | Rate limited, 429 |
| 7 | Conflict, 409 |

## Commands

For the full help menu:

```sh
jiractrl help
jiractrl help search
```

Check auth:

```sh
jiractrl auth check --json
```

Inspect the detected deployment and known capabilities:

```sh
jiractrl server-info --json
```

Search with JQL:

```sh
jiractrl search --jql 'project = MYPROJ ORDER BY updated DESC' --max 20
```

Search with a profile:

```sh
jiractrl search --profile project_recent --json
```

Fetch multiple pages within a hard issue budget:

```sh
jiractrl search --profile project_recent --all --limit 500 --json
```

Continue from the opaque `page.next` value returned by JSON output:

```sh
jiractrl search --profile project_recent --cursor 'CONTINUATION'
```

`search` uses Jira Cloud enhanced JQL search and Data Center's supported
offset search behind the same cursor interface. `--max` is the page size;
`--all` never returns more than `--limit` issues.

Get an issue:

```sh
jiractrl get MYPROJ-123
```

Return the exact Jira response:

```sh
jiractrl get MYPROJ-123 --raw-json
```

Create an issue:

```sh
jiractrl create --project MYPROJ --type Task --summary "Fix the thing" --description "Details"
```

Use typed Jira JSON when fields need booleans, numbers, nulls, arrays, or
objects:

```json
{
  "fields": {
    "project": {"key": "MYPROJ"},
    "issuetype": {"id": "10001"},
    "summary": "Typed issue",
    "customfield_10042": {"accountId": "abc123"},
    "customfield_10043": [{"id": "20001"}, {"id": "20002"}],
    "customfield_10044": 3.5,
    "parent": {"key": "MYPROJ-10"},
    "duedate": "2026-08-01"
  },
  "properties": [
    {"key": "created-by", "value": {"agent": true}}
  ]
}
```

Pass the object from a file or stdin:

```sh
jiractrl create --input issue.json --json
jiractrl create --input - --json < issue.json
```

Create accepts `fields`, `update`, `transition`, `properties`, and
`historyMetadata`. Update accepts `fields`, `update`, `properties`, and
`historyMetadata`. Transition accepts those keys plus `transition`, whose
`id` must be a string. Unknown top-level keys and incorrectly typed envelope
members fail locally. Input is limited to 1 MiB.

Structured input conflicts with create or update convenience flags. For a
transition, `--to` may be combined with input containing transition-screen
fields, but it conflicts when the input also contains `transition`.

Preview the exact method, deployment-aware path, and body without sending a
mutation:

```sh
jiractrl create --input issue.json --dry-run
jiractrl update MYPROJ-123 --input update.json --dry-run
jiractrl transition MYPROJ-123 --input transition.json --dry-run
```

Update an issue:

```sh
jiractrl update MYPROJ-123 --summary "Updated summary"
```

Assign with a deployment-native identity or unassign:

```sh
# Jira Cloud
jiractrl assign MYPROJ-123 --account-id 5b10ac8d82e05b22cc7d4ef5 --json

# Data Center exact username
jiractrl assign MYPROJ-123 --user fred --json

jiractrl assign MYPROJ-123 --unassign --json
```

Both forms are checked against Jira's assignable-user data before mutation.
On Cloud, `--user` resolves one exact display name or email and returns
candidates instead of choosing an ambiguous match. On Data Center, it resolves
an exact username only. Prefer `--account-id` on Cloud once discovery has
supplied it.

Create a subtask with a validated subtask issue type:

```sh
jiractrl create --project MYPROJ --type Sub-task \
  --summary "Verify the fix" --parent MYPROJ-123 --json
```

The parent is sent as `{"key":"MYPROJ-123"}` and the issue type is sent by its
discovered ID. Structured input can instead supply Jira's exact `parent` and
`issuetype` objects. High-level reparenting is available on Cloud:

```sh
jiractrl update MYPROJ-456 --parent MYPROJ-123 --json
```

Data Center high-level reparenting fails with `unsupported` before mutation.
Structured input remains available for deployment-specific fields when the
site's edit metadata explicitly exposes them.

List comments independently of the partial comment field returned by issue
reads:

```sh
jiractrl comments list MYPROJ-123 --all --limit 500 --json
```

Add, update, or remove a comment:

```sh
jiractrl comments add MYPROJ-123 --body "Adding **important** context."
jiractrl comments update MYPROJ-123 10042 --body-file corrected.md
jiractrl comments remove MYPROJ-123 10042
```

`jiractrl comment ISSUE --body TEXT` remains an alias for `comments add`.
On Jira Cloud, convenience bodies convert a documented Markdown subset to
ADF: headings, paragraphs, ordered and unordered lists, links, fenced code
blocks, inline code, bold, and italic. Data Center sends string bodies.

Use structured input to preserve an exact Cloud ADF document and optional
visibility restriction:

```json
{
  "body": {
    "type": "doc",
    "version": 1,
    "content": [
      {
        "type": "paragraph",
        "content": [{"type": "text", "text": "Restricted context"}]
      }
    ]
  },
  "visibility": {"type": "role", "value": "Developers"}
}
```

```sh
jiractrl comments add MYPROJ-123 --input comment.json --json
```

List transitions:

```sh
jiractrl transitions MYPROJ-123
```

Transition:

```sh
jiractrl transition MYPROJ-123 --to "In Progress"
```

List fields:

```sh
jiractrl fields
```

Inspect populated fields on an issue:

```sh
jiractrl issue-fields MYPROJ-123
```

Discover valid projects, issue types, fields, and assignees before a write:

```sh
jiractrl projects list --query platform --json
jiractrl projects get MYPROJ --json
jiractrl projects issue-types MYPROJ --json
jiractrl meta create --project MYPROJ --type 10001 --json
jiractrl meta edit MYPROJ-123 --json
jiractrl users assignable --project MYPROJ --query owain --json
jiractrl users assignable --issue MYPROJ-123 --query owain --json
```

Project, issue-type, create-field, and user results include normalized page
metadata. Create and edit metadata includes field IDs, names, required flags,
schemas, default values, and allowed values. An empty `allowedValues` array is
preserved. If an issue-type name matches more than one ID, the CLI returns an
`ambiguous` error with candidates instead of choosing one.

Jira Cloud discovery keeps `accountId`; Data Center discovery keeps `name` and
`key`. Agents should copy the identity supplied by their deployment and never
guess or translate one form into another. Cloud create metadata uses the
current project and issue-type scoped endpoints.

Discover and inspect issue links:

```sh
jiractrl links types --json
jiractrl links list MYPROJ-123 --json
```

Link direction is always explicit. For example, `--outward MYPROJ-123
--inward MYPROJ-456` supplies the source and target sides of the Jira link:

```sh
jiractrl links add --type Blocks \
  --outward MYPROJ-123 --inward MYPROJ-456 --json
jiractrl links remove 10042
```

Jira reports success when an add duplicates an existing link and does not
return a created link ID. The JSON receipt exposes both facts. List the issue
again when the exact link ID is needed.

List and transfer attachments:

```sh
jiractrl attachments list MYPROJ-123 --json
jiractrl attachments upload MYPROJ-123 --file ./evidence.txt --json
jiractrl attachments download 10001 --output ./downloads/evidence.txt
jiractrl attachments remove 10001
```

Uploads stream the file as Jira's required multipart `file` field with
`X-Atlassian-Token: no-check`. Downloads require an explicit path, reject
parent traversal, and refuse to replace a file unless `--overwrite` is set.
Link and attachment removals are explicit mutations and are never retried.

Read issue history independently of issue expansion:

```sh
jiractrl changelog MYPROJ-123 --all --limit 500 --json
jiractrl changelog MYPROJ-123 --field status --field assignee --json
```

`--max` controls the Jira page size. `--all` scans no more than `--limit` raw
history records. Field filters match either Jira field names or field IDs and
retain only matching change items.

Read and manage worklogs:

```sh
jiractrl worklogs list MYPROJ-123 --all --limit 500 --json
jiractrl worklogs add MYPROJ-123 --time-spent "1h 30m" \
  --started 2026-07-28T12:30:00Z --comment "Implemented **validation**" \
  --adjust new --new-estimate 2d --json
jiractrl worklogs update MYPROJ-123 10010 --time-spent 2h --json
```

Durations use positive Jira units `w`, `d`, `h`, and `m`. Estimate flags are
validated as a set before mutation. Cloud worklog comments convert the same
Markdown subset as issue comments to ADF; Data Center sends strings. Worklog
mutations are never retried.

Inspect and change watcher state:

```sh
jiractrl watchers list MYPROJ-123 --json
jiractrl watchers add MYPROJ-123 --self
jiractrl watchers add MYPROJ-123 --account-id ACCOUNT_ID  # Cloud
jiractrl watchers remove MYPROJ-123 --user fred           # Data Center
```

Cloud accepts account IDs and Data Center accepts usernames. Adding or
removing another user may require Jira's Manage watcher list permission.
Privacy and permission failures use the normal structured JSON error contract.
Watcher mutations are never retried.

## Bounded bulk writes

Run create, update, or transition inputs from a JSON array or JSONL file:

```sh
jiractrl bulk create --input creates.jsonl --json
jiractrl bulk update --input updates.json --dry-run
jiractrl bulk transition --input transitions.jsonl --max-items 100 --json
```

Create items use the same payload as `create --input`. Update and transition
items add an `issue` key:

```json
[
  {
    "identity": "sync-42",
    "issue": "MYPROJ-123",
    "fields": {"summary": "Updated by sync"}
  },
  {
    "identity": "sync-43",
    "issue": "MYPROJ-124",
    "fields": {"summary": "Another update"}
  }
]
```

`identity` is optional and is copied unchanged into the matching result. If it
is omitted, the command assigns `item-0`, `item-1`, and so on. Results always
remain in input order.

The command validates the entire input before sending a mutation. It accepts
50 items by default; `--max-items` can raise or lower that local limit but
cannot exceed the compiled ceiling of 1000. Inputs larger than the selected
limit are rejected, not truncated. `--dry-run` plans every item and sends no
mutation.

Bulk writes use Jira's single-issue endpoints sequentially, so no server bulk
limit or hidden server chunking applies. An ordinary Jira 4xx response fails
that item and processing continues. A transport failure, timeout response,
HTTP 429, or Jira 5xx stops processing because the last mutation may have
completed. Every remaining item is returned as `skipped`.

A mixed or stopped batch exits 1. With `--json`, it writes one object to stdout
with `ok:false`, `error.kind:"partial_failure"`, summary counts, and one exact
result per input item. Text output includes each item's status and failure
detail. Inspect each result before deciding whether to retry. The CLI never
retries a whole batch or an individual write automatically.

## Jira Software planning

`server-info` probes the Jira Software board API. Its `software` capability is
`available`, `unavailable`, or `unknown` when permissions prevent a reliable
answer. Planning commands return the normal structured permission error when
the caller cannot access Jira Software.

Discover Scrum and Kanban boards:

```sh
jiractrl boards list --project MYPROJ --json
jiractrl boards get 7 --json
jiractrl boards issues 7 --all --limit 500 --json
jiractrl boards backlog 7 --all --limit 500 --json
```

Inspect sprints and their ordered issues:

```sh
jiractrl sprints list 7 --state active,future --all --limit 200 --json
jiractrl sprints get 42 --json
jiractrl sprints issues 42 --all --limit 500 --json
```

Cloud board, backlog, and sprint issue reads use enhanced token pagination.
Data Center uses offset pagination behind the same opaque `page.next` and
`--cursor` contract. Board and sprint lists use `--start` offsets. `--max`
cannot exceed 100 and `--all` never reads beyond `--limit`.

Move or rank issues:

```sh
jiractrl sprints move 42 --issue MYPROJ-3 --issue MYPROJ-2 --json
jiractrl backlog move --board 7 --issue MYPROJ-3,MYPROJ-2 --json
jiractrl rank --issue MYPROJ-3,MYPROJ-2 --before MYPROJ-1 --json
jiractrl estimate MYPROJ-3 --board 7 --value 8.0 --json
```

Sprint moves, backlog moves, and ranks accept at most 50 issues and preserve
the supplied ordering. `--rank-field` selects a non-default numeric rank field.
Board-scoped `backlog move --board` is Cloud-only; omit `--board` on Data
Center.
All planning writes are one-shot and are never retried. When Jira returns HTTP
207, the command exits 1 and preserves Jira's exact partial details. With
`--json`, that partial response is an `ok:false` envelope on stdout.

List profiles:

```sh
jiractrl profiles list
```

Show a profile:

```sh
jiractrl profiles show project_recent
```

## Project Docs

- `SPEC.md` defines the product direction.
- `TASKS.md` tracks implementation and release work.
- `AGENTS.md` gives operating guidance for AI agents.
- `docs/agent-guide.md` has practical recipes for using the CLI safely.

## Development

The code is organized into small internal packages:

- `internal/cli`: command parsing, help, and output
- `internal/config`: config file, environment overrides, and profiles
- `internal/jira`: Jira REST client and response types
- `internal/triage`: read-only issue quality heuristics

Run:

```sh
go test ./...
go build -o jiractrl .
```
