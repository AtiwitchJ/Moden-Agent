# Code mode × Director boundary fixes

Date: 2026-08-02 22:53
Scope: frontend-only. Three small surgical fixes that keep Code mode and the
Director board from blurring into each other.

## Problem

The shell renders the same `CodeSidebar` for every route (Code, Director,
Work). That sidebar (and the Code home Sessions list) currently:

1. Shows a per-session project label under each row in the Code home — a
   "kubun" (区分) the user reported as bug noise in Code mode.
2. Includes orchestrator sessions (per-project Directors) in the flat Recents
   list, even though they belong on the Director board, not the Code shell.
3. Forces the user to pick a project in the global Director's "Create card"
   dialog before they can submit, even when there is only one registered
   project.

## Fixes

### 1. Filter orchestrator sessions out of `recentSessions`

`frontend/src/renderer/lib/recent-sessions.ts`
- Add `&& !isOrchestratorSession(session)` to the predicate. Director sessions
  (kind === "orchestrator" or id ending in `-orchestrator`) are excluded.
- Mirrors what `Sidebar.tsx:555` and `SessionsBoard.tsx:87` already do via
  `workerSessions()`.

`frontend/src/renderer/lib/recent-sessions.test.ts`
- New case: "excludes orchestrator sessions so they don't leak into the Code
  shell" — covers both `kind: "orchestrator"` and the id-suffix fallback.

### 2. Drop the per-session kubun in the Code home Sessions list

`frontend/src/renderer/routes/_shell.index.tsx:52`
- Removed the `<span>{session.workspaceName}</span>` line that rendered the
  project name under each session.
- The full project name is still discoverable on hover (`title` attribute) and
  the row's `onClick` still navigates with the right `projectId`.
- `CodeSidebar.tsx` already had no per-row kubun, so this brings the home list
  in line with the sidebar's flat Recents shape.

### 3. Auto-select project when the global Director has exactly one project

`frontend/src/renderer/components/CreateWorkCardDialog.tsx`
- New effect: when `projectIdProp` is unset (global Workboard at `/manage`)
  and `projectsQuery.data` has exactly one project, set `selectedProjectId`
  to that project (and propagate its path to `targetPath`).
- Behaviour matches the existing per-project branch, where the project field
  is rendered as a disabled `<Input>` pre-filled with the route's projectId.
- When there are 0 or ≥2 projects, the user still has to pick — the
  dropdown UX is unchanged.

## Files touched

| File | Change |
| --- | --- |
| `frontend/src/renderer/lib/recent-sessions.ts` | Add orchestrator filter |
| `frontend/src/renderer/lib/recent-sessions.test.ts` | Add orchestrator-exclusion test |
| `frontend/src/renderer/routes/_shell.index.tsx` | Drop workspaceName line |
| `frontend/src/renderer/components/CreateWorkCardDialog.tsx` | Auto-select when one project |

No backend, API, SQL, or OpenAPI changes — wire shape unchanged.

## Verification

```bash
cd frontend
npx vitest run src/renderer/lib/recent-sessions.test.ts   # 4 tests pass — confirmed 2026-08-02
npm run typecheck
```

`npm run typecheck` is **not** clean on this branch — 5 pre-existing errors in
`Workboard.test.tsx` and `event-transport.test.ts`, unrelated to this plan's
4 files. Confirmed by stashing this plan's diff and re-running typecheck:
same 5 errors persist without it. Likely a `0a53d5b6` regression or an
unlanded rebase, not this plan's fault — but don't claim "clean" until that's
fixed separately; track it outside this plan.

Manual smoke — **not yet run in-session**, do before marking this plan done:
1. With a per-project orchestrator (Director) session live, open Code mode.
   The "Recents" sidebar and the home Sessions list should no longer show it.
2. On `/manage` with exactly one registered project, click "Create card".
   The Project select should be pre-filled; you can go straight to Title +
   Coding Agent.
3. On `/manage` with 0 or ≥2 projects, the Project dropdown still appears
   and is required.

## Out of scope

- Splitting `CodeSidebar` per mode (Code vs Director vs Work). All three
  modes still share the sidebar for now; the changes above make the leak
  impossible without restructuring routes.
- Renaming or hiding the global `/manage` route.
- Removing the per-project Workboard embedded in `ProjectBoard.tsx`. The
  user did not ask to remove it; only its "Create card" friction on the
  global board is addressed here.
