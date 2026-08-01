# Moden Code Home Design

## Goal

Replace Code mode's current home (CEO Dashboard) and its sidebar
(company/project tree) with a chat-first layout modeled on the Claude Code
desktop app: a slim left nav (New / Recents / More) and a main pane that
opens straight into a composer for starting work, instead of a company/
project card grid.

## Locked decisions

- This replaces the Code-mode sidebar and home **entirely** — not an
  additional view alongside the existing one.
- Project/company selection is no longer a persistent sidebar tree. It moves
  into the composer's workspace picker and the New flow.
- "New" covers both creating a brand-new project and starting a new session
  in an existing project — one picker, not two separate entry points.
- The bottom composer is the primary session-creation surface, not just a
  message box: picking a workspace + typing a prompt + submit spawns a
  session directly. The sidebar's "New" button only focuses this composer;
  it does not open a separate modal.
- Company/HQ concept (companies, CEO Dashboard, "Watch Live", HQ roles) is
  **out of scope** for this spec. It stays exactly as-is in the codebase —
  code untouched, just no longer linked from the new Code-mode home/sidebar.
  Removing it is tracked as its own future spec (it reaches into ~15
  frontend files and ~28 backend files, including the just-shipped Live
  Terminals "Watch Live" feature — too large to fold in here).
- No backend/API changes. Every action in this spec is composed from
  existing endpoints.

## Sidebar (replaces `Sidebar.tsx`'s render in Code mode)

- Brand mark, unchanged from today.
- **New** — focuses/resets the home composer. No separate dialog.
- **Recents** — a flat list of sessions across *all* projects (not grouped
  by project or company), sorted by `updatedAt` descending. Each row: status
  dot (reuse `attentionZone` from `types/workspace.ts`), session title,
  relative timestamp. Click navigates to that session.
- **More** (bottom, replaces today's footer Settings dropdown) — Pull
  requests, Live Terminals, Settings, theme toggle. Same items the footer
  dropdown already has today, just relocated.

Data source: flatten `workspaces[].sessions` from the existing
`useWorkspaceQuery` result — no new query.

## Home main pane

- Heading: "Welcome back, {org name}" (reuse `useUiStore`'s `orgName`,
  already used by the current CEO Dashboard).
- **Sessions** section: every active session across all projects, `action`
  attention-zone sessions (need input / failed) sorted first, everything
  else after. Same `attentionZone`/`sessionIsActive` helpers the sidebar
  already uses today.
- Empty state (no sessions anywhere yet): a prompt pointing at the
  composer, no card grid.

## Composer (session-creation surface)

Pinned at the bottom of the Home pane.

- Embedded workspace picker (dropdown): lists existing projects, plus a
  "New project…" entry that opens the existing path-picker flow
  (`CreateProjectFlow` / `CreateProjectAgentSheet`, unchanged).
- Text input for the first prompt.
- On submit:
  1. If "New project…" was picked: call the existing `createProject` (in
     `_shell.tsx`), which already spawns an orchestrator as part of project
     creation. Get back the new session id.
  2. If an existing project was picked: call the existing
     `spawnOrchestrator(projectId)` to get a session id.
  3. Send the typed prompt as the session's first message via the existing
     `POST /api/v1/sessions/{sessionId}/send` endpoint (already used by
     `TerminalTile`'s compose bar from the Live Terminals work).
  4. Navigate to `/projects/$projectId/sessions/$sessionId`.
- All three calls are existing, already-tested code paths; this composer
  only orchestrates them in sequence — no new endpoint, no new mutation
  logic beyond call-in-order.

## What's explicitly out of scope

- Artifacts / Customize nav items from the reference screenshot — no
  matching concept in moden-agent, not built.
- CEO Dashboard component and route: left in the codebase, unrouted from
  Code mode's home. Not deleted.
- Company/HQ concept end to end (frontend + backend): explicitly deferred
  to a separate spec, per the locked decision above.
- Code Manage mode and Work mode: untouched, unrelated to this spec.
- Backend/daemon: no changes.

## Testing

- Route/component tests for the new Code-mode home and sidebar (render,
  empty state, composer submit → navigate, per existing testing patterns in
  `frontend/src/renderer/routes/*.test.tsx`).
- Reuse existing coverage for `createProject`, `spawnOrchestrator`, and the
  session `/send` endpoint — this spec doesn't change their behavior, only
  calls them from a new place.
