# Agent Guide

This guide is the supported workflow for an agent entering an unfamiliar Jira
project. Prefer `--json`, carry identifiers forward from discovery, and place a
hard limit on every multi-page read.

## Product compatibility

| Surface | Jira Cloud | Jira Data Center | Product requirement |
| --- | --- | --- | --- |
| Platform issues, projects, fields, users, comments, links, attachments, worklogs, watchers, and changelog | Supported | Supported | Jira platform |
| Jira Software boards, sprints, backlog, rank, and estimate | Supported | Supported, except board-scoped backlog moves | Jira Software |
| Jira Service Management requests, queues, SLAs, participants, and customer comments | Supported | Supported where the REST resource exists | Jira Service Management |
| Cloud rich text and identity | ADF and `accountId` | Not applicable | Jira Cloud |
| Data Center rich text and identity | Not applicable | String bodies and exact username | Jira Data Center |

Run `server-info` before product-specific work. A capability can be
`available`, `unavailable`, or `unknown`. `unknown` means the probe was
inconclusive, often because the current user lacks permission.

```sh
jiractrl server-info --json
```

`jiractrl` selects Cloud REST v3 where needed and Data Center REST v2. The raw
API escape hatch does not translate paths, so choose a path supported by the
target deployment.

## Configure without exposing credentials

The default config is `~/.config/jiractrl/config.toml`. Use `--config PATH` for
automation or another environment.

```toml
[jira]
base_url = "https://jira.example.com"
token = "read-from-a-private-config-or-environment"
deployment = "auto"
# Jira Cloud only:
# email = "agent@example.com"

[retry]
max_attempts = 3
base_delay_ms = 500
max_delay_ms = 30000

[profiles.my_open]
jql = "assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC"
fields = ["summary", "status", "assignee", "priority", "issuetype", "created", "updated"]
max_results = 50
```

Never put tokens in prompts, command arguments, logs, fixtures, or versioned
files. Environment variables may override config values. Use
`JIRACTRL_BASE_URL`, `JIRACTRL_TOKEN`, `JIRACTRL_EMAIL`, and
`JIRACTRL_DEPLOYMENT`.

## End-to-end workflow for an unfamiliar project

### 1. Authenticate and inspect the deployment

```sh
jiractrl --config ./config.toml auth check --json
jiractrl --config ./config.toml server-info --json
```

With `deployment = "auto"`, Jira's `serverInfo` endpoint selects `cloud` or
`data_center`. Set an explicit deployment only when detection is blocked.

### 2. Discover the project and issue shape

Project discovery is offset-paged. Continue while `data.page.hasMore` is true,
using `data.page.next` as the next `--start` value.

```sh
jiractrl projects list --query engineering --start 0 --max 50 --json
jiractrl projects get MYPROJ --json
jiractrl projects issue-types MYPROJ --start 0 --max 50 --json
jiractrl meta create --project MYPROJ --type ISSUE_TYPE_ID --json
```

Use the returned issue type ID, field IDs, and allowed-value IDs. Never guess a
custom field ID. Before updating an existing issue, inspect its editable
fields:

```sh
jiractrl meta edit MYPROJ-123 --json
jiractrl fields --json
jiractrl issue-fields MYPROJ-123 --json
```

### 3. Discover a stable assignee identity

```sh
jiractrl users assignable --project MYPROJ --query alex --json
jiractrl users assignable --issue MYPROJ-123 --query alex --json
```

Cloud writes use an exact `accountId`. Data Center writes use an exact
username. Display names are not stable identifiers.

### 4. Plan and create the issue

Use structured input when Jira fields need IDs, objects, arrays, numbers,
booleans, or nulls:

```json
{
  "fields": {
    "project": {"key": "MYPROJ"},
    "issuetype": {"id": "10001"},
    "summary": "Investigate queue latency",
    "customfield_10042": {"id": "20001"},
    "labels": ["agent-created"]
  }
}
```

Preview the exact method, path, and request body, then perform the write:

```sh
jiractrl create --input create.json --dry-run --json
jiractrl create --input create.json --json
```

Dry-run performs no mutation. The simple flags remain useful when metadata
shows no special field requirements:

```sh
jiractrl create --project MYPROJ --type Task \
  --summary "Investigate queue latency" --description "Initial evidence" --json
```

### 5. Attach evidence and link related work

List link types before choosing the exact type name.

```sh
jiractrl attachments upload MYPROJ-123 --file ./evidence.txt --json
jiractrl attachments list MYPROJ-123 --json
jiractrl links types --json
jiractrl links add --type Blocks \
  --outward MYPROJ-123 --inward MYPROJ-456 --json
jiractrl links list MYPROJ-123 --json
```

Downloads require an explicit destination and refuse to overwrite it unless
`--overwrite` is present:

```sh
jiractrl attachments download ATTACHMENT_ID \
  --output ./downloads/evidence.txt --json
```

### 6. Assign, comment, and transition

Use the identity discovered in step 3:

```sh
# Jira Cloud
jiractrl assign MYPROJ-123 --account-id ACCOUNT_ID --json

# Jira Data Center
jiractrl assign MYPROJ-123 --user exact_username --json
```

Comments accept Markdown on Cloud and convert it to ADF. Data Center sends a
plain string body.

```sh
jiractrl comments add MYPROJ-123 \
  --body "Evidence is attached and the owner is confirmed." --json
jiractrl comments list MYPROJ-123 --all --limit 500 --json
```

Discover the allowed transition, then pass its returned ID or exact name:

```sh
jiractrl transitions MYPROJ-123 --json
jiractrl transition MYPROJ-123 --to TRANSITION_ID --json
```

### 7. Verify the result and audit history

Do not treat a successful write response as the final verification.

```sh
jiractrl get MYPROJ-123 --json
jiractrl changelog MYPROJ-123 --all --limit 500 --json
jiractrl changelog MYPROJ-123 \
  --field status --field assignee --all --limit 500 --json
```

After a Jira Cloud write, a following search can use numeric issue IDs with
`--reconcile` to request read-after-write reconciliation:

```sh
jiractrl search --jql 'key = MYPROJ-123' \
  --reconcile NUMERIC_ISSUE_ID --json
```

## Pagination and read budgets

Never assume that one page is complete.

- Search, board issues, backlog, and sprint issues expose an opaque
  `data.page.next` cursor. Pass it back unchanged with `--cursor`.
- Project, issue type, user, board list, sprint list, comment, worklog, and
  changelog commands expose `data.page.hasMore` and a next offset where Jira
  provides one.
- Prefer `--all --limit N` when supported. `--max` is the per-request page
  size. `--limit` is the total scan budget.
- If a command has no `--all`, repeat it with `--start data.page.next` until
  `hasMore` is false.
- JSM list commands preserve Jira's page object directly under `data`. While
  `data.isLastPage` is false, advance `--start` by the returned `data.size`.
  Keep a separate total-item budget and stop on a missing, zero, or
  non-advancing size instead of assuming completion.

```sh
jiractrl search --profile my_open --all --limit 500 --json
jiractrl comments list MYPROJ-123 --all --limit 500 --json
jiractrl worklogs list MYPROJ-123 --all --limit 500 --json
jiractrl boards issues BOARD_ID --all --limit 500 --json
```

An agent should stop and report truncation when its budget is reached while
`hasMore` remains true or a JSM response has `isLastPage: false`.

## Mutation safety

Mutations require explicit user intent. Use these boundaries:

- Discover IDs and allowed values before writing. Do not guess custom field,
  issue type, transition, user, board, sprint, service desk, or request type
  identifiers.
- A labels field update replaces the entire array. Read and preserve any
  labels that should remain.
- Use `--dry-run` for create, update, transition, and bulk plans when the
  request is not already obvious.
- Re-read the affected issue, comments, links, attachments, or changelog
  after a mutation.
- Do not automatically retry writes. A timeout or transport failure can leave
  the result unknown.

```sh
jiractrl update MYPROJ-123 --input update.json --dry-run --json
jiractrl update MYPROJ-123 --input update.json --json
```

### Bounded bulk writes and partial failure

Bulk create, update, and transition accept a JSON array or JSONL file. Add a
stable `identity` to each item for reconciliation.

```sh
jiractrl bulk create --input creates.jsonl --dry-run --json
jiractrl bulk update --input updates.json --max-items 100 --json
jiractrl bulk transition --input transitions.jsonl --max-items 100 --json
```

The full input is validated before the first write. Execution is sequential
and preserves input order. An ordinary Jira 4xx is recorded for that item and
processing continues. A transport error, timeout, 429, or 5xx stops the batch
because the write result may be ambiguous. Later items are marked `skipped`.
A partial batch exits 1 and returns the full ordered result. Never retry the
whole file. Inspect Jira and create a new input containing only safe writes.

### Rate limits and retries

Safe reads retry bounded 429, 500, 502, 503, and 504 responses. The configured
attempt and delay budgets apply, and `Retry-After` is honored up to
`max_delay_ms`. Mutations are sent once.

JSON errors use `{"ok":false,"error":...}` on stderr. Branch on `error.kind`,
not message text. `rate_limited` exits 6, `conflict` exits 7, and JSM
permission failures use `permission`.

## Jira Software workflow

These commands require the Jira Software capability:

```sh
jiractrl boards list --project MYPROJ --json
jiractrl boards get BOARD_ID --json
jiractrl boards issues BOARD_ID --all --limit 500 --json
jiractrl boards backlog BOARD_ID --all --limit 500 --json
jiractrl sprints list BOARD_ID --state active,future \
  --all --limit 200 --json
jiractrl sprints get SPRINT_ID --json
jiractrl sprints issues SPRINT_ID --all --limit 500 --json
```

Planning writes are explicit and never retried:

```sh
jiractrl sprints move SPRINT_ID --issue MYPROJ-3,MYPROJ-2 --json
jiractrl backlog move --board BOARD_ID \
  --issue MYPROJ-3,MYPROJ-2 --json
jiractrl rank --issue MYPROJ-3,MYPROJ-2 --after MYPROJ-1 --json
jiractrl estimate MYPROJ-3 --board BOARD_ID --value 8.0 --json
```

Moves and ranks accept at most 50 issues. HTTP 207 is a partial operation: the
command exits 1 and preserves Jira's details. Board-scoped backlog moves are
Cloud-only. On Data Center, omit `--board`.

## Jira Service Management workflow

These commands require the Jira Service Management capability:

```sh
jiractrl jsm service-desks list --json
jiractrl jsm queues list SERVICE_DESK_ID --include-count --json
jiractrl jsm request-types list SERVICE_DESK_ID --json
jiractrl jsm request-types fields \
  --service-desk SERVICE_DESK_ID --request-type REQUEST_TYPE_ID --json
jiractrl jsm requests list --service-desk SERVICE_DESK_ID \
  --status OPEN_REQUESTS --json
jiractrl jsm requests get MYPROJ-123 --json
jiractrl jsm slas list MYPROJ-123 --json
```

Discover request fields before constructing a create payload. Public customer
comments and internal notes are different writes:

```sh
jiractrl jsm requests create --service-desk SERVICE_DESK_ID \
  --request-type REQUEST_TYPE_ID --input request.json --json
jiractrl jsm comments add MYPROJ-123 \
  --body "Customer update" --visibility public --json
jiractrl jsm comments add MYPROJ-123 \
  --body "Agent-only note" --visibility internal --json
jiractrl jsm participants add MYPROJ-123 \
  --account-id CLOUD_ACCOUNT_ID --json
```

Use `--username` for Data Center participants. JSM writes are never retried.
The current user and deployment can expose different resources, so treat
`unavailable`, `unknown`, and `permission` as distinct outcomes.

## Raw API restrictions

Prefer typed commands. Use `api` only for an uncommon resource that has no
typed command.

```sh
jiractrl api get '/rest/api/3/RESOURCE?expand=names' --json
jiractrl api request /rest/api/3/RESOURCE --method PATCH \
  --input request.json --allow-write --json
```

The path must be same-origin and relative, beginning with one slash. Absolute
URLs, scheme-relative paths, traversal, fragments, backslashes, and redirects
are blocked. Authentication, cookie, host, forwarding, connection, origin,
browser-security, and method-override headers cannot be supplied. Input is one
JSON value up to 1 MiB. GET and HEAD may use safe-read retries. POST, PUT,
PATCH, and DELETE require `--allow-write` and are never retried. Raw API
writes do not have a dry-run mode. Inspect the JSON input and prefer a typed
command with dry-run support when one exists.

## Machine contract and verification

Successful `--json` output uses `{"ok":true,"data":...}`. Errors use
`{"ok":false,"error":...}` on stderr. `get --raw-json` and single-page
`search --raw-json` intentionally return Jira's exact response without an
envelope.

The fixture-backed end-to-end test follows this guide from auth through
changelog verification and asserts discovered identifiers and complete pages.
The command verification inventory records the automated check for every
documented command family:

- [Command verification inventory](command-verification.md)
- [`TestAgentWorkflowDiscoversIdentifiersAndExhaustsPages`](../internal/cli/e2e_agent_workflow_test.go)

Run the same gates used by the project:

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./...
```
