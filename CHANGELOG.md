# Changelog

All notable changes to this project will be documented in this file.

## [0.1.0] - 2026-05-17

### Added
- MCP server exposing 5 tools: `list_tasks`, `create_task`, `task_action`, `list_projects`, `list_statuses`
- Flexible Webasyst hash-syntax filters for `list_tasks` (inbox, outbox, project/N, status/*, search/*, id/N, number/P.N)
- `full_number` field on tasks (e.g. `P.123`) and number-based lookup support in `task_action`
- Bearer token authentication with constant-time comparison
- `.env` file support for configuration
- GoReleaser config for multi-platform builds (Windows, macOS, Linux × amd64/arm64)
- GitHub Actions release workflow triggered on version tags
