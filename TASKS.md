# jiractrl Tasks

## Now

- [x] Choose `jiractrl` as the user-facing CLI name.
- [x] Add `SPEC.md`.
- [x] Add `TASKS.md`.
- [x] Start replacing `.env`-first configuration with `config.toml`.
- [x] Support `--config` global flag.
- [x] Add profile support from TOML.
- [x] Support `search --profile NAME`.
- [x] Update README for public/open-source usage.
- [x] Keep legacy environment variable support for convenience.
- [ ] Add tests for config and profile parsing.
- [ ] Replace the minimal TOML parser with a proper TOML library if needed.

## Core CLI

- [x] `auth check`
- [x] `search --jql`
- [x] `search --profile`
- [x] `get ISSUE`
- [x] `create`
- [x] `update`
- [x] `comment`
- [x] `transitions`
- [x] `transition`
- [x] `fields`
- [x] `issue-fields`

## Agent Experience

- [ ] Stable normalized JSON output.
- [ ] Raw Jira JSON option.
- [ ] Clear exit codes.
- [x] Concise text output.
- [x] Good error messages for missing config, token, profile, or JQL.

## Install / Release

- [x] Add `install.sh` for one-line install.
- [x] Add GitHub Actions CI for Go.
- [x] Add release workflow with multi-platform binaries.
- [x] Add checksums.
- [x] Add license.
- [x] Add example config.
- [ ] Add `go install` instructions once GitHub repo exists.
- [ ] Push to GitHub.

## Later

- [ ] Optional Homebrew tap.
- [ ] Workflow-specific commands as external recipes or profiles, not core assumptions.
- [ ] Markdown output for reports.
- [ ] Configurable field aliases for custom Jira fields.
