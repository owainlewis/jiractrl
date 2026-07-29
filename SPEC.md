# jiractrl Specification

`jiractrl` is a small command line control plane for Jira. It gives AI agents
and humans a generic, predictable interface without encoding one
organization's workflow in the binary.

## Goals

- Cover the common Jira issue, collaboration, planning, and service workflows.
- Make normalized JSON available for every Jira operation command.
- Preserve exact Jira JSON where normalization would lose useful data.
- Require discovery before writes that depend on site-specific identifiers.
- Bound pagination, retries, input size, and bulk work.
- Ship as one Go binary for macOS and Linux.

## Non-goals

- Replace Jira or model every administrative feature.
- Hide Jira permissions, workflows, custom fields, or product boundaries.
- Run a database, daemon, browser, or AI API.
- Put organization-specific automation in the generic core.

Profiles, wrapper scripts, and downstream agent instructions own local policy.

## Compatibility

The Jira platform command set supports Jira Cloud and Jira Data Center. Jira
Software commands require the Software REST capability. Jira Service
Management commands require the JSM REST capability. Cloud uses `accountId`
and ADF where Jira requires them. Data Center uses exact usernames and string
rich-text bodies.

The detailed matrix and tested workflow live in
[`docs/agent-guide.md`](docs/agent-guide.md).

## Configuration

The default file is `~/.config/jiractrl/config.toml`. `--config PATH` selects a
different environment.

```toml
[jira]
base_url = "https://jira.example.com"
token = "read-from-private-config"
deployment = "auto"
# Jira Cloud only:
# email = "agent@example.com"

[defaults]
max_results = 50
output = "text"

[retry]
max_attempts = 3
base_delay_ms = 500
max_delay_ms = 30000

[profiles.project_recent]
jql = "project = MYPROJ ORDER BY updated DESC"
fields = ["summary", "status", "assignee", "priority", "issuetype"]
max_results = 50
```

Supported primary overrides are `JIRACTRL_CONFIG`, `JIRACTRL_BASE_URL`,
`JIRACTRL_TOKEN`, `JIRACTRL_EMAIL`, and `JIRACTRL_DEPLOYMENT`. Legacy
`JIRA_BASE_URL`, `JIRA_PAT`, `JIRA_TOKEN`, and `JIRA_EMAIL` remain fallbacks.
Secrets must not appear in prompts, logs, fixtures, or versioned files.

## Public command surface

| Capability | Commands |
| --- | --- |
| Connection | `auth check`, `server-info` |
| Issue reads | `search`, `get`, `fields`, `issue-fields`, `triage` |
| Discovery | `projects`, `meta`, `users assignable`, `transitions`, `profiles` |
| Issue writes | `create`, `update`, `assign`, `transition`, `bulk` |
| Collaboration | `comments`, `links`, `attachments`, `changelog`, `worklogs`, `watchers` |
| Jira Software | `boards`, `sprints`, `backlog`, `rank`, `estimate` |
| Jira Service Management | `jsm service-desks`, `queues`, `request-types`, `requests`, `comments`, `participants`, `slas` |
| Escape hatch | constrained same-origin `api` |

All Jira examples in the public guide use `--json`. Structured mutations use
`--input FILE|-`. Create, update, transition, and bulk operations support
`--dry-run`.

## JSON and exit contract

Normalized success:

```json
{"ok":true,"data":{"key":"MYPROJ-123"}}
```

Normalized failure on stderr:

```json
{
  "ok": false,
  "error": {
    "kind": "validation",
    "message": "field is not valid"
  }
}
```

Stable error kinds include `local`, `transport`, `validation`, `ambiguous`,
`auth`, `permission`, `not_found`, `rate_limited`, `conflict`, `server`, and
`unsupported`. Server field errors and retry metadata are retained when Jira
provides them. Credential material is redacted.

`get --raw-json` and single-page `search --raw-json` return exact Jira bytes
without the envelope.

| Exit | Meaning |
| ---: | --- |
| 0 | Success |
| 1 | Local, usage, config, transport, partial, or unknown failure |
| 2 | Authentication or permission rejection |
| 3 | Not found |
| 4 | Other Jira 4xx validation rejection |
| 5 | Jira 5xx |
| 6 | Rate limited |
| 7 | Conflict |

## Pagination and consistency

JSON page objects expose `hasMore` and either an opaque cursor or an offset.
Opaque cursors must be returned unchanged. Multi-page commands support a hard
`--limit` where practical. Commands without `--all` expose the next offset for
an explicit loop.

Cloud search uses enhanced JQL token pagination. Data Center search uses
offsets behind the same public cursor contract. Cloud search may accept up to
50 numeric issue IDs through `--reconcile` after a write.

No command may silently claim a partial page is complete.

## Mutation safety

- Mutations require a dedicated command or explicit raw `--allow-write`.
- Custom field, issue type, user, transition, planning, and JSM identifiers
  come from discovery. The CLI must not guess.
- Structured input preserves Jira JSON types and is size-limited.
- A labels field update retains Jira's replacement semantics, so callers must
  read and preserve labels that should remain.
- Safe reads may retry bounded 429 and retryable 5xx responses. Writes are
  never retried automatically.
- Bulk input is validated before the first write and has a hard item limit.
  Per-item 4xx failures continue. Ambiguous transport, 429, and 5xx outcomes
  stop later writes and mark them skipped.
- Raw API requests stay on the configured origin, reject dangerous paths,
  redirects, and protected headers, limit JSON input to 1 MiB, and require
  `--allow-write` for mutations.

## Installation and release

Supported installation paths:

```sh
curl -fsSL https://github.com/owainlewis/jiractrl/releases/latest/download/install.sh | sh
go install github.com/owainlewis/jiractrl@latest
```

Tagged releases build checksummed Darwin and Linux binaries for arm64 and
amd64. CI runs formatting, tests, vet, and builds.

## Verification

The end-to-end local Jira fixture runs the public flow from authentication and
discovery through create, attachment, link, assignment, comment, transition,
complete changelog pagination, and final verification. It asserts that
site-specific IDs are discovered and carried into writes.

Every command family is tied to an automated or recorded manual check in
[`docs/command-verification.md`](docs/command-verification.md). Markdown shell
examples are syntax-checked in the Go test suite.
