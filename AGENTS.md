# AGENTS.md

Guidance for AI agents using or modifying `jiractrl`.

## Purpose

`jiractrl` is a command line control plane for Jira. It gives agents a small, predictable interface for reading and mutating Jira without embedding organization-specific workflow rules in the tool.

Prefer JSON output for agent workflows.

## Safety Rules

- Treat read commands as safe: `auth check`, `search`, `get`, `fields`, `issue-fields`, `transitions`, `profiles`, `projects`, `meta`, `users`, `comments list`, `links types`, `links list`, `attachments list`, `attachments download`, `changelog`, `worklogs list`, `watchers list`, `boards list`, `boards get`, `boards issues`, `boards backlog`, `sprints list`, `sprints get`, `sprints issues`, `triage`.
- Treat write commands as mutating Jira: `create`, `update`, `assign`, `comments add`, `comments update`, `comments remove`, `comment`, `links add`, `links remove`, `attachments upload`, `attachments remove`, `worklogs add`, `worklogs update`, `watchers add`, `watchers remove`, `transition`, `bulk create`, `bulk update`, `bulk transition`, `sprints move`, `backlog move`, `rank`, `estimate`.
- Before writing custom fields, run `jiractrl fields --json` and, when possible, `jiractrl issue-fields ISSUE --json`.
- Do not guess custom field IDs.
- Do not store tokens in prompts, logs, commits, or generated docs.
- Prefer profiles for repeated JQL instead of hardcoding queries into scripts.
- Use `--config PATH` when operating in a non-default environment.

## Recommended Agent Flow

1. Check auth:

   ```sh
   jiractrl auth check
   ```

2. Discover profiles:

   ```sh
   jiractrl profiles list
   jiractrl profiles show NAME
   ```

3. Read with JSON:

   ```sh
   jiractrl search --profile NAME --json
   jiractrl get ISSUE-123 --json
   ```

4. Inspect fields before custom updates:

   ```sh
   jiractrl fields --json
   jiractrl issue-fields ISSUE-123 --json
   ```

5. Write only with explicit user intent:

   ```sh
   jiractrl comments add ISSUE-123 --body "..."
   jiractrl update ISSUE-123 --field customfield_12345=value
   jiractrl transition ISSUE-123 --to "In Progress"
   jiractrl bulk update --input updates.jsonl --dry-run
   jiractrl sprints move 42 --issue ISSUE-123 --json
   ```

## Output Expectations

- `--json` success uses `{"ok":true,"data":...}` on stdout.
- `--json` failure uses `{"ok":false,"error":...}` on stderr.
- A partial `bulk ... --json` write is the exception: it exits 1 and writes
  `ok:false`, summary counts, and every item result to stdout. Inspect results
  before retrying.
- A Jira Software HTTP 207 response also exits 1 and writes exact partial
  details to stdout when `--json` is set.
- Rate limits exit 6 and conflicts exit 7. Other exit codes are documented in `README.md`.
- Human text output is concise and intended for terminals.
- Errors print through the CLI caller and should be treated as failed operations.

## Project Structure

- `main.go`: process entrypoint only.
- `internal/cli`: command parsing, help text, text/JSON output.
- `internal/config`: config file, environment overrides, profiles.
- `internal/jira`: Jira REST client and response types.
- `internal/triage`: read-only issue quality heuristics.
- `docs/agent-guide.md`: usage guide for agents and automation.

## Development

Run:

```sh
go test ./...
go build -o jiractrl .
```

Keep the core generic. Organization-specific workflows should live in config profiles, wrapper scripts, or downstream agent instructions.
