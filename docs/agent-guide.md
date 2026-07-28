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
jiractrl profiles list
jiractrl profiles show my_open
jiractrl search --profile my_open --json
jiractrl get MYPROJ-123 --json
jiractrl fields --json
jiractrl issue-fields MYPROJ-123 --json
jiractrl transitions MYPROJ-123 --json
jiractrl triage --jql 'project = MYPROJ AND statusCategory != Done' --json
```

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

```sh
jiractrl create --project MYPROJ --type Task --summary "Short summary" --description "Details" --json
jiractrl update MYPROJ-123 --summary "Updated summary"
jiractrl update MYPROJ-123 --field customfield_12345=value
jiractrl comment MYPROJ-123 --body "Follow-up context."
jiractrl transition MYPROJ-123 --to "In Progress"
```

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

Prefer a file for longer generated comments:

```sh
jiractrl comment MYPROJ-123 --body-file comment.md
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
