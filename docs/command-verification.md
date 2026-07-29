# Command Verification Inventory

This inventory links every public command family to an automated check. It is
the record required when a command is documented but is not part of the
end-to-end fixture.

| Command family | Automated check |
| --- | --- |
| `auth`, `server-info`, `projects`, `meta`, `users`, `create`, `assign`, comment add/list, `transitions`, `transition`, link types/add/list, attachment upload/list, `changelog`, `get` | `internal/cli/e2e_agent_workflow_test.go` |
| `search`, profiles, stable JSON envelopes, raw issue JSON, JSON errors, exit codes | `internal/cli/cli_test.go`, `internal/cli/json_test.go` |
| `update`, structured input, dry-run | `internal/cli/input_test.go` |
| `fields`, `issue-fields`, `triage` | `internal/cli/cli_test.go` |
| project, issue type, edit metadata, and user pagination/disambiguation | `internal/cli/discovery_test.go`, `internal/jira/discovery_test.go` |
| comments and comment visibility | `internal/cli/comments_test.go`, `internal/jira/comments_test.go` |
| issue links | `internal/cli/relationships_test.go`, `internal/jira/relationships_test.go` |
| attachment list and upload | `internal/cli/e2e_agent_workflow_test.go`, `internal/jira/attachments_test.go` |
| attachment download and remove | `internal/jira/attachments_test.go`; manual release check verifies the CLI receipt and downloaded bytes against a disposable attachment |
| changelog filtering and bounded pagination | `internal/cli/collaboration_test.go`, `internal/jira/changelog_test.go` |
| worklog list, add, and update | `internal/cli/collaboration_test.go`, `internal/jira/collaboration_test.go` |
| watcher list, add, and remove | `internal/cli/collaboration_test.go`, `internal/jira/collaboration_test.go` |
| bulk create, update, transition, dry-run, and partial failure | `internal/cli/bulk_test.go` |
| boards, sprints, backlog, rank, and estimate | `internal/cli/planning_test.go`, `internal/jira/software_test.go` |
| constrained raw API reads, writes, headers, and redirects | `internal/cli/api_test.go`, `internal/jira/raw_api_test.go` |
| JSM service desks, queues, request types, requests, comments, participants, and SLAs | `internal/cli/jsm_test.go`, `internal/jira/jsm_test.go` |
| hierarchy and subtasks | `internal/cli/hierarchy_test.go`, `internal/jira/hierarchy_test.go` |
| configuration, environment overrides, profiles, deployment, retries, and capability gates | `internal/config/config_test.go`, `internal/jira/client_test.go`, `internal/jira/retry_test.go`, `internal/jira/software_test.go`, `internal/jira/jsm_test.go` |
| help and shell syntax in Markdown examples | `internal/cli/documentation_test.go` |

The inventory names test files instead of Jira instances. Tests use local HTTP
fixtures, contain no credentials, and run without network access.
