# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**wa-tasks-mcp** is a local Model Context Protocol (MCP) server that bridges Claude Code, Cursor, and other MCP clients to Webasyst Tasks (a task/project management system). It exposes task management operations as MCP tools over HTTP with Bearer token authentication.

The server runs as a standalone executable and listens on a configurable HTTP port, handling both regular HTTP requests (health checks) and MCP protocol requests over Streamable HTTP with Server-Sent Events (SSE).

## Architecture

### Three-Layer Design

1. **Transport Layer** (`cmd/server/main.go`):
   - HTTP server with `/mcp` endpoint for MCP protocol and `/healthz` for health checks
   - Bearer token authentication middleware (constant-time comparison to prevent timing attacks)
   - Graceful shutdown with 5-second timeout

2. **MCP Server Layer** (`internal/mcpserver/server.go`):
   - Exposes 5 tools via the MCP SDK's `mcp.Server` and tool registration API
   - Converts incoming MCP tool calls to tracker client calls
   - Returns JSON-formatted results
   - Uses strongly-typed argument structs for compile-time safety

3. **Tracker Client Layer** (`internal/tracker/client.go`):
   - Thin HTTP wrapper over Webasyst Tasks REST API (RPC-style at `/api.php/{method}`)
   - Handles authentication via Bearer token in Authorization header
   - Flexible response parsing: supports both array and object-wrapped responses (API is inconsistent across versions)
   - Custom error handling that extracts error messages from JSON responses

### Data Flow

```
MCP Client (Claude Code/Cursor)
    ↓ HTTP POST with Bearer token
HTTP Server + Auth Middleware
    ↓
MCP Protocol Handler (StreamableHTTPHandler)
    ↓
mcpserver.New() → Tool handlers
    ↓
tracker.Client methods
    ↓
Webasyst Tasks API
```

## Building

```powershell
cd C:\mcp\wa-tasks-mcp
go mod tidy
go build -o wa-tasks-mcp.exe ./cmd/server
```

The output is a standalone executable with no runtime dependencies.

## Configuration

All configuration is via environment variables. See README.md for the full list. Key variables:
- `TRACKER_API_BASE` — Webasyst API endpoint (e.g., `https://tracker.example.com/api.php`)
- `TRACKER_API_TOKEN` — Webasyst access token
- `MCP_BEARER_SECRET` — Bearer token for MCP clients (any long random string)
- `MCP_LISTEN_ADDR` — Server address (default: `127.0.0.1:7777`)

## MCP Tools

The server exposes these tools to MCP clients:

1. **list_tasks** — Query tasks with flexible Webasyst hash-syntax filters:
   - `"inbox"` (default), `"outbox"`, `"project/N"`, `"status/inprogress"`, `"search/text"`, `"id/N"`, `"number/P.N"`, etc.
   - Supports pagination via `limit` and `offset`

2. **create_task** — Create a task in a project
   - Required: `project_id`, `name`
   - Optional: `text`, `assigned_contact_id`, `status_id`, `priority`, `due_date`
   - Generates a UUID internally for idempotency

3. **task_action** — Perform workflow transitions
   - Required: task `id`, `action` (one of: `"close"`, `"forward"`, `"return"`)
   - Optional: `status_id`, `text` (comment), `assigned_contact_id`

4. **list_projects** — Get all available projects (ID and name)

5. **list_statuses** — Get all task statuses (ID and name)

## Key Design Decisions

- **Flexible Response Parsing**: The Webasyst API returns task lists in different formats depending on version or endpoint. The client uses `json.RawMessage` and attempts multiple shapes (bare array vs. object wrapper) to handle this gracefully.

- **Bearer Token Auth**: Authentication happens at the HTTP middleware level using constant-time comparison to prevent timing attacks. Protects against random local process access.

- **HTTP/2 Timeout**: 10-second `ReadHeaderTimeout` prevents slowloris attacks; client methods have a 30-second timeout.

- **Graceful Shutdown**: Signal handling (SIGINT, SIGTERM) closes connections cleanly within 5 seconds.

- **Error Handling**: API errors are extracted from JSON `error` and `error_message` fields. Non-JSON errors fall back to HTTP status code.

## Code Organization

- `cmd/server/main.go` — Entry point, HTTP server setup, auth middleware
- `internal/tracker/client.go` — Webasyst API client (DTOs, transport, methods)
- `internal/mcpserver/server.go` — MCP tool definitions and handlers

No tests or linting configuration exist. No dependencies beyond `go-sdk` and `uuid`.

## Future Improvements

The README notes these potential enhancements:
- Cache `list_projects` and `list_statuses` (rarely change, frequently requested)
- Add `update_task` tool for editing task fields beyond status
- Use MCP resources (instead of tools) for reference data
- Add `outputSchema` to tools for structured Claude Code responses
