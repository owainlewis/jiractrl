# jiractrl

`jiractrl` is a small command line control plane for Jira, designed for AI agents and humans who need predictable, scriptable Jira operations.

It aims to be:

- generic, with no company-specific workflow assumptions
- JSON-friendly for agents
- readable for humans
- configurable through `config.toml`
- easy to install as a single Go binary

## Install

From source:

```sh
go build -o jiractrl .
```

Planned one-line install after the GitHub repository is published:

```sh
curl -fsSL https://raw.githubusercontent.com/owainlewis/jiractrl/main/install.sh | sh
```

Planned Go install:

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

Use a custom config:

```sh
jiractrl --config ./config.toml auth check
```

Environment variables can override config values:

```sh
export JIRACTRL_BASE_URL="https://jira.example.com"
export JIRACTRL_TOKEN="your-personal-access-token"
```

`JIRA_BASE_URL`, `JIRA_PAT`, and `JIRA_TOKEN` are also supported as fallbacks.

## Commands

Check auth:

```sh
jiractrl auth check
```

Search with JQL:

```sh
jiractrl search --jql 'project = MYPROJ ORDER BY updated DESC' --max 20
```

Search with a profile:

```sh
jiractrl search --profile project_recent --json
```

Get an issue:

```sh
jiractrl get MYPROJ-123
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
