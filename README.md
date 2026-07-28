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
up to `max_delay_ms`. Create, update, comment, and transition requests are
never retried automatically.

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

Update an issue:

```sh
jiractrl update MYPROJ-123 --summary "Updated summary"
```

Comment:

```sh
jiractrl comment MYPROJ-123 --body "Adding follow-up context."
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
