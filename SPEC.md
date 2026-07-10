# jiractrl Specification

`jiractrl` is a command line control plane for Jira, designed for AI agents and humans who need predictable, scriptable Jira operations.

The project should stay small, portable, and open-source friendly. It should not contain company-specific assumptions, private Jira URLs, or workflow rules that only make sense for one organization.

## Goals

- Provide a clean CLI for common Jira operations.
- Make every useful operation available in machine-readable JSON.
- Support reusable profiles for saved JQL and defaults.
- Use `config.toml` instead of `.env` for auth and connection settings.
- Be pleasant for humans and reliable for agents.
- Ship as a single Go binary with one-line install support.

## Non-Goals

- Replace Jira.
- Encode one company's process into the core CLI.
- Require a database or background service.
- Require AI APIs. The CLI should provide clean data and useful heuristics, not depend on a model.

## Name

Binary name: `jiractrl`

Working description:

> A control plane for Jira for AI agents.

## Configuration

Default config path:

```text
~/.config/jiractrl/config.toml
```

Optional override:

```sh
jiractrl --config ./config.toml ...
```

Initial TOML shape:

```toml
[jira]
base_url = "https://jira.example.com"
token = "..."

[defaults]
max_results = 50
output = "text"

[profiles.my_open]
jql = "assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC"
fields = ["summary", "status", "assignee", "priority", "issuetype", "created", "updated"]
max_results = 50

[profiles.project_recent]
jql = "project = MYPROJ AND updated >= -7d ORDER BY updated DESC"
max_results = 100
```

Environment variables may override config for CI:

- `JIRACTRL_BASE_URL`
- `JIRACTRL_TOKEN`
- `JIRA_BASE_URL`
- `JIRA_PAT`
- `JIRA_TOKEN`

## Core Commands

### Auth

```sh
jiractrl auth check
```

Checks the configured Jira connection and token.

### Search

```sh
jiractrl search --jql 'project = MYPROJ ORDER BY updated DESC'
jiractrl search --profile project_recent
jiractrl search --profile project_recent --json
```

Search issues using explicit JQL or a configured profile.

### Get

```sh
jiractrl get MYPROJ-123
jiractrl get MYPROJ-123 --json
```

Fetch a single issue.

### Create

```sh
jiractrl create --project MYPROJ --type Task --summary "Fix thing" --description "Details"
jiractrl create --json-input issue.json
```

Create an issue.

### Update

```sh
jiractrl update MYPROJ-123 --summary "New summary"
jiractrl update MYPROJ-123 --description-file body.md
jiractrl update MYPROJ-123 --field customfield_12345=value
```

Update issue fields.

### Comment

```sh
jiractrl comment MYPROJ-123 --body "Follow-up note"
jiractrl comment MYPROJ-123 --body-file comment.md
```

Add a comment.

### Transition

```sh
jiractrl transitions MYPROJ-123
jiractrl transition MYPROJ-123 --to "In Progress"
```

List and apply workflow transitions.

### Profiles

```sh
jiractrl profiles list
jiractrl profiles show project_recent
jiractrl search --profile project_recent
```

Profiles are read from `config.toml`.

### Fields

```sh
jiractrl fields
jiractrl issue-fields MYPROJ-123
```

Support discovery of custom fields and issue shape.

## Agent-Friendly Output

Every command that reads or mutates Jira should support `--json`.

JSON should be normalized where useful. Raw Jira JSON can be exposed with `--raw-json`.

Text output should be concise and stable enough for humans, but agents should prefer JSON.

## Installation

Target one-line install:

```sh
curl -fsSL https://raw.githubusercontent.com/owainlewis/jiractrl/main/install.sh | sh
```

Package options:

- GitHub Releases with Darwin/Linux binaries.
- Homebrew tap later if the project becomes useful.
- `go install github.com/owainlewis/jiractrl@latest` once published.

## CI/CD

Use GitHub Actions for:

- `go test ./...`
- `go vet ./...`
- `gofmt` check
- build on Linux and macOS
- release binaries on tagged versions

Release workflow:

- Tags use `vX.Y.Z`.
- Build `jiractrl` for at least:
  - `darwin/arm64`
  - `darwin/amd64`
  - `linux/amd64`
  - `linux/arm64`
- Attach checksums.

## Release Polish

Before public release:

- Rename binary and docs from `jira` to `jiractrl`.
- Remove private defaults from docs and code.
- Add license.
- Add examples.
- Add config sample.
- Add install script.
- Add CI.
- Add a small test suite around config, profile resolution, and command parsing.
- Avoid writing secrets to logs or output.

## Design Principles

- Generic core, workflow-specific profiles.
- Configuration over hardcoded defaults.
- JSON-first for agents.
- Text output for humans.
- Clear errors.
- No destructive operation without an explicit command.
- Safe by default: read operations first, write operations obvious.
