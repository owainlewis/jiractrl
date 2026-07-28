# Agent Guide

This guide shows how agents should use `jiractrl` safely and predictably.

## Configuration

Default config path:

```text
~/.config/jiractrl/config.toml
```

Use a custom config for automation:

```sh
jiractrl --config ./config.toml auth check
```

Minimal config:

```toml
[jira]
base_url = "https://jira.example.com"
token = "your-personal-access-token"
deployment = "auto"
# Jira Cloud only:
# email = "you@example.com"

[profiles.my_open]
jql = "assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC"
fields = ["summary", "status", "assignee", "priority", "issuetype", "created", "updated"]
max_results = 50
```

`deployment = "auto"` calls Jira's `serverInfo` endpoint before other
operations. Set it to `cloud` or `data_center` when that endpoint is blocked.
Use `jiractrl server-info --json` to inspect the selected deployment and known
product capabilities.

## Read Operations

Read operations do not mutate Jira.

```sh
jiractrl auth check
jiractrl server-info --json
jiractrl profiles list --json
jiractrl profiles show my_open --json
jiractrl search --profile my_open --json
jiractrl get MYPROJ-123 --json
jiractrl fields --json
jiractrl issue-fields MYPROJ-123 --json
jiractrl transitions MYPROJ-123 --json
jiractrl comments list MYPROJ-123 --all --limit 500 --json
jiractrl links types --json
jiractrl links list MYPROJ-123 --json
jiractrl attachments list MYPROJ-123 --json
jiractrl changelog MYPROJ-123 --all --limit 500 --json
jiractrl triage --jql 'project = MYPROJ AND statusCategory != Done' --json
```

All `--json` results use `{"ok":true,"data":...}`. Read errors and write
errors use `{"ok":false,"error":...}` on stderr. Branch on `error.kind`, not
message text. Rate limits use `rate_limited` and exit 6. Conflicts use
`conflict` and exit 7.

Safe reads retry bounded 429 and retryable 5xx responses. Mutations are never
retried automatically, so an agent should inspect Jira before deciding whether
to repeat a failed create, update, comment add/update/remove, or transition.

Use `get --raw-json` or single-page `search --raw-json` only when exact Jira
response fields are required. Raw output is intentionally not wrapped.

Search JSON includes an opaque `page.next` cursor and `page.hasMore`. Pass the
cursor back without interpreting it:

```sh
jiractrl search --profile my_open --cursor 'CONTINUATION' --json
```

For bounded multi-page reads, use:

```sh
jiractrl search --profile my_open --all --limit 500 --json
```

`--max` controls each Jira request. `--limit` is the total issue budget and
defaults to 1000. Jira Cloud uses enhanced token pagination; Data Center uses
offset pagination behind the same opaque cursor field.

After a Jira Cloud write, repeat `--reconcile` with numeric issue IDs when the
following search must include those writes. One search accepts at most 50
reconciliation IDs:

```sh
jiractrl search --jql 'key = MYPROJ-123' --reconcile 10001 --json
```

## Write Operations

Write operations mutate Jira. Use them only with explicit intent.

Discover valid values before constructing a write:

```sh
jiractrl projects get MYPROJ --json
jiractrl projects issue-types MYPROJ --json
jiractrl meta create --project MYPROJ --type 10001 --json
jiractrl meta edit MYPROJ-123 --json
jiractrl users assignable --issue MYPROJ-123 --query owain --json
```

Use field and issue-type IDs from metadata. Do not choose silently when an
issue-type name returns multiple candidates. Use Cloud `accountId` values on
Cloud and Data Center `name` or `key` identities on Data Center; they are not
interchangeable.

```sh
jiractrl create --project MYPROJ --type Task --summary "Short summary" --description "Details" --json
jiractrl update MYPROJ-123 --summary "Updated summary"
jiractrl update MYPROJ-123 --field customfield_12345=value
jiractrl comments add MYPROJ-123 --body "Follow-up context."
jiractrl transition MYPROJ-123 --to "In Progress"
```

Assign with the identity form returned by the current deployment:

```sh
# Cloud
jiractrl users assignable --issue MYPROJ-123 --query alex --json
jiractrl assign MYPROJ-123 --account-id ACCOUNT_ID --json

# Data Center
jiractrl assign MYPROJ-123 --user exact_username --json

jiractrl assign MYPROJ-123 --unassign --json
```

Both identity forms are validated before mutation. Cloud `--user` resolves an
exact display name or email and never chooses among duplicates. Data Center
`--user` resolves an exact username only. Use the stable Cloud account ID when
available.

For subtasks, discover the valid subtask type and pass a parent:

```sh
jiractrl projects issue-types MYPROJ --json
jiractrl create --project MYPROJ --type SUBTASK_TYPE_ID \
  --summary "Child work" --parent MYPROJ-123 --json
```

The convenience command validates that the type is a subtask and sends the
discovered type ID plus `parent.key`. Structured create input can supply exact
`issuetype` and `parent` objects. `update ISSUE --parent PARENT` is a Cloud
high-level operation; Data Center fails before mutation because parent editing
varies by version and configuration.

For typed custom fields and compound mutations, pass one Jira-compatible JSON
object through `--input FILE` or `--input -`. Use `--dry-run` first to inspect
the exact method, path, and body without calling a mutation endpoint:

```sh
jiractrl update MYPROJ-123 --input update.json --dry-run
jiractrl update MYPROJ-123 --input update.json --json
```

Create input accepts `fields`, `update`, `transition`, `properties`, and
`historyMetadata`. Update input accepts `fields`, `update`, `properties`, and
`historyMetadata`. Transition input accepts `transition`, `fields`, `update`,
`properties`, and `historyMetadata`. Use a string transition ID:

```json
{
  "transition": {"id": "31"},
  "fields": {"resolution": {"id": "1"}}
}
```

Do not combine structured create or update input with convenience flags. A
transition can use `--to` with input fields only; do not also include a
`transition` object. Input is capped at 1 MiB.

## Common Agent Recipes

### Search and summarize open work

```sh
jiractrl search --profile my_open --json
```

Then summarize:

- issue key
- summary
- status
- assignee
- priority
- updated date if requested in profile fields

### Safely update a custom field

```sh
jiractrl fields --json
jiractrl issue-fields MYPROJ-123 --json
jiractrl update MYPROJ-123 --field customfield_12345=value
```

Do not infer custom field IDs from display names unless `fields --json` confirms them.

### Comment on an issue from a generated note

Read comments through the dedicated paged endpoint. Do not rely on the partial
comment field returned by an issue read:

```sh
jiractrl comments list MYPROJ-123 --all --limit 500 --json
```

Prefer a file for longer generated comments:

```sh
jiractrl comments add MYPROJ-123 --body-file comment.md
```

On Cloud, body text supports headings, paragraphs, ordered and unordered
lists, links, fenced code, inline code, bold, and italic Markdown and is
converted to ADF. Data Center receives a string. For exact Cloud ADF, supply
`{"body":{...}}` with `--input FILE|-`.

Restrict a comment when explicitly requested:

```sh
jiractrl comments add MYPROJ-123 --body-file comment.md \
  --visibility-type role --visibility-value Developers
```

Updating and removing comments are distinct writes:

```sh
jiractrl comments update MYPROJ-123 10042 --body-file corrected.md
jiractrl comments remove MYPROJ-123 10042
```

### Inspect relationships and history

Discover link types before adding a relationship:

```sh
jiractrl links types --json
jiractrl links list MYPROJ-123 --json
jiractrl links add --type Blocks \
  --outward MYPROJ-123 --inward MYPROJ-456 --json
```

List output and JSON state `inward` or `outward` explicitly. Jira accepts a
duplicate link request as successful and returns no created link ID. Treat the
add receipt as acceptance, then list links when an exact ID is required.

Use the dedicated changelog endpoint instead of a partial issue expansion:

```sh
jiractrl changelog MYPROJ-123 --all --limit 500 --json
jiractrl changelog MYPROJ-123 --field status,assignee --json
```

The limit bounds raw histories scanned even when field filtering removes
entries.

### Exchange attachments safely

```sh
jiractrl attachments list MYPROJ-123 --json
jiractrl attachments upload MYPROJ-123 --file ./evidence.txt --json
jiractrl attachments download 10001 --output ./downloads/evidence.txt
```

Downloads require an explicit non-traversing path and do not overwrite by
default. Use `--overwrite` only with explicit intent. Uploads accept one
regular file and stream it. Remove by attachment ID only when explicitly
requested:

```sh
jiractrl attachments remove 10001
```

### Move an issue through workflow

```sh
jiractrl transitions MYPROJ-123 --json
jiractrl transition MYPROJ-123 --to "In Progress"
```

Transition by ID if names are ambiguous.

## Error Handling

Agents should treat any non-zero exit as failure and avoid follow-up write commands until the error is understood.

Common failures:

- missing token: configure `config.toml` or `JIRACTRL_TOKEN`
- unknown profile: run `jiractrl profiles list`
- unknown transition: run `jiractrl transitions ISSUE`
- invalid custom field: run `jiractrl fields --json`

## Help Menu

Use:

```sh
jiractrl help
jiractrl help search
jiractrl help update
```
