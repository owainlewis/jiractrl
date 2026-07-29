# jiractrl Tasks

## Shipped

- [x] TOML configuration, environment overrides, profiles, and `--config`.
- [x] Jira Cloud and Data Center detection with capability reporting.
- [x] Stable JSON success and error envelopes, raw JSON reads, and exit codes.
- [x] Bounded pagination, safe-read retries, rate-limit metadata, and Cloud
  read-after-write reconciliation.
- [x] Issue search, get, create, update, assignment, comments, and
  transitions.
- [x] Project, issue type, field, edit metadata, and assignable-user discovery.
- [x] Structured mutation input and dry-run planning.
- [x] Bounded bulk create, update, and transition with ordered partial results.
- [x] Subtasks and parent relationships.
- [x] Issue links, attachments, changelog, worklogs, and watchers.
- [x] Jira Software boards, sprints, backlog, rank, and estimate.
- [x] Constrained same-origin raw API requests.
- [x] Jira Service Management discovery, requests, queues, SLAs, comments, and
  participants.
- [x] End-to-end agent workflow fixture and public compatibility guide.
- [x] Local config/profile tests, command tests, Jira client tests, CI, release
  binaries, checksums, install script, `go install` instructions, and license.

## Later

- [ ] Optional Homebrew tap.
- [ ] Markdown report output.
- [ ] Configurable aliases for site-specific custom fields.
- [ ] Additional typed commands when real workflows justify them.

Organization-specific workflows remain in profiles, wrapper scripts, or
downstream agent instructions rather than the generic core.
