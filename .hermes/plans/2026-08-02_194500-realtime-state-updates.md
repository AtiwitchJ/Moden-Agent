# Realtime State Updates — Code, Director, Work

> **For implementation agents:** preserve the existing dirty worktree and reconcile it before editing. In particular, inspect the uncommitted Director status, dispatch-trigger, and daemon-log changes; do not overwrite them wholesale.

## Goal

Make state changes feel immediate across every mode using one durable SSE/CDC connection for the app. Keep WebSocket only for interactive terminal input/output.

## Locked decisions

- Use existing SSE `/api/v1/events` plus durable CDC replay for state updates in **Code**, **Director**, and **Work**.
- Keep one app-wide connection mounted at the root; changing modes must not create another stream or lose the current stream.
- Keep WebSocket `/mux` only for terminal I/O in Work. Do not create WebSocket protocols for cards, sessions, or notifications.
- Keep 15-second polling for workspace, Workboard, and Director status as a fallback while SSE reconnects or is unavailable.
- Show one compact connection indicator in `ModeBar`: `Live`, `Reconnecting`, or `Daemon offline`.
- Do not add an activity feed, terminal preview in Code/Director, migration, or new daemon API endpoint for this work.

## Implementation

### 1. Centralize SSE cache refresh

In `frontend/src/renderer/lib/event-transport.ts`:

- Retain the single root-mounted EventSource and its durable replay/reconnect behavior.
- On SSE open and after a daemon/base-URL reconnect, invalidate `workspaceQueryKey`, SCM summary, and the `['workboard']` query prefix. This refetches stale state lost while the stream was disconnected.
- For `work_card_changed`, parse existing `payload.project_id` and `payload.card_id`, then invalidate:
  - global Workboard (`workboardQueryKey()`)
  - the changed project Workboard (`workboardQueryKey(projectId)`)
  - the project Director-status query (use its existing query-key prefix)
  - the changed card's dispatch-failure query
- Keep session/PR invalidations debounced. Keep `session_message_created` restricted to messages.
- Treat malformed CDC payloads as a no-op; never tear down a valid EventSource because one event is malformed.

### 2. Show connection health globally

In `ModeBar` and the existing events-connection store/hook:

- Derive display state from both Electron daemon status and SSE connection state.
- If daemon is not ready: `Daemon offline`.
- If daemon is ready and EventSource is open: `Live`.
- If daemon is ready but the stream is idle/disconnected: `Reconnecting`.
- Render a small colored status dot with tooltip and accessible label. Keep mode navigation, macOS drag/no-drag regions, and layout unchanged.

### 3. Keep mode behavior focused

- **Code:** session/project CDC updates continue to refresh workspace data used by recents and composer project choices.
- **Director:** card CDC updates refresh both global and project boards, WIP/status, and an open card's failure detail. Do not rely only on the 15-second fallback.
- **Work:** session state comes from SSE; `TerminalPane`/`useTerminalSession` retain the existing `/mux` WebSocket for raw PTY bytes and user input.
- Retain the existing 15-second query intervals as a fallback; do not add per-mode EventSources or per-card polling loops.

## Tests and verification

- Extend `event-transport` tests for global/project Workboard, Director-status, and card-failure invalidation from a `work_card_changed` payload.
- Test reconnect/open invalidates the full Workboard prefix once and keeps only one EventSource for an unchanged daemon URL.
- Add ModeBar tests for Live, Reconnecting, and Daemon offline text/accessible status.
- Regression-test Code recents, Director global board, Director project board, and Work terminal mux behavior.
- Run `go test ./...`, `npm run frontend:typecheck`, and focused Vitest for event transport, ModeBar, Workboard, Code Home, and terminal session.

## Acceptance

- A card changed by daemon dispatch appears immediately in Director whether the board is global or project-scoped.
- Code recents/session status and Work session status refresh from the same shared SSE stream.
- Terminal remains interactive over one WebSocket mux and no duplicate sockets/events appear after mode switches.
- If SSE is unavailable, the ModeBar says Reconnecting and mounted data still refreshes within 15 seconds; if daemon is down, it says Daemon offline.
