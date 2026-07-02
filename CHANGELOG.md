# Changelog

All notable changes to this project will be documented in this file.

## [0.4.0] - 2026-07-03

### Added
- `update_task` tool — edit properties of an existing task (name, text, assigned contact, project, milestone, priority, status, due date), addressed by `id` or `full_number`. Unspecified fields are left unchanged.

## [0.3.0] - 2026-06-26

### Added
- `add_comment` tool — add a comment to a task (by `task_id` or `full_number`)
- `update_comment` tool — edit an existing comment by log entry ID

## [0.2.0] - 2026-06-26

### Added
- In-memory cache for `list_statuses` and `list_projects` (TTL 5 min) to reduce redundant API calls

## [0.1.0] - 2026-05-17

### Added
- MCP server exposing 5 tools: `list_tasks`, `create_task`, `task_action`, `list_projects`, `list_statuses`
- Flexible Webasyst hash-syntax filters for `list_tasks` (inbox, outbox, project/N, status/*, search/*, id/N, number/P.N)
- `full_number` field on tasks (e.g. `P.123`) and number-based lookup support in `task_action`
- Bearer token authentication with constant-time comparison
- `.env` file support for configuration
- GoReleaser config for multi-platform builds (Windows, macOS, Linux × amd64/arm64)
- GitHub Actions release workflow triggered on version tags
