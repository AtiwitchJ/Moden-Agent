# Cursor prompt — execute the Director-only commander plan

Paste everything below the line into Cursor.

---

Execute the implementation plan at `docs/superpowers/plans/2026-08-04-director-only-commander.md`. Read the whole plan first, including the Global Constraints, Verification and Out of scope sections.

## Execution rules

**No sandbox. Run everything on this machine, for real.**
- Do not use a container, VM, sandbox, or any isolated test runner.
- Run `go test`, `npm test`, `npm run typecheck`, `npm run lint`, `sqlite3`, `tmux`, `ps` and `ao` directly in the workspace shell.
- The live verification steps talk to the real daemon and the real `~/.ao` data dir. Run them as written. If the daemon is not running, start it and say so — do not fake or skip the step.
- Never report a step as passing without pasting the command's actual output.

**Maximum parallelism. Do not serialize what can run at once.**
- Spawn one background agent per task and start all six simultaneously. Do not run them in waves, do not throttle, do not wait for task N before starting task N+1.
- The six tasks touch disjoint files (table below), so they cannot conflict on edits.

| Task | Owns these files exclusively |
|---|---|
| 1 | `backend/internal/session_manager/manager.go`, `manager_test.go` |
| 2 | `backend/internal/commander/orchestrator/**`, `backend/internal/daemon/orchestrator_store_adapter*.go` |
| 3 | `backend/internal/service/workboard/**` |
| 4 | `director/**`, `backend/internal/directorassets/bundle/index.js` |
| 5 | `frontend/src/renderer/components/WorkCardFocusPanel.tsx`, `.test.tsx` |
| 6 | `frontend/src/renderer/components/ProjectSettingsForm.tsx`, `.test.tsx` |

- One rule they must share: **commit one at a time.** Before `git commit`, an agent takes the repo lock, commits only the files in its own row, releases. Never `git add -A` and never `git add .` — stage exactly the paths listed in that task's commit step.
- If an agent's full-suite run (`go test ./internal/...`) fails because a sibling task is mid-edit, re-run it after that sibling commits rather than "fixing" a file it does not own.

## Method — non-negotiable

Each task is TDD and the plan spells out every step:

1. Write the failing test exactly as given.
2. **Run it and confirm it fails, with the expected failure message.** Paste the output. A test that passes before the implementation means the test is wrong — fix the test, do not proceed.
3. Write the implementation exactly as given.
4. Run the test and confirm it passes. Paste the output.
5. Run the wider suite for that area.
6. Commit with the message given in the plan.

Do not batch steps. Do not skip step 2. Do not "improve" the plan's code while implementing it — if you believe a step is wrong, stop and say why before changing it.

## Where the plan says to check the existing file first

Three steps ask you to adapt to helpers that already exist rather than inventing new ones. Read the file before writing:

- Task 2 Step 1 — the spawner fake in `backend/internal/commander/orchestrator/orchestrator_test.go`. Use the one that is there.
- Task 5 Step 1 — the `apiClient` mock and `renderPanel` helper in `WorkCardFocusPanel.test.tsx`. Extend them, do not add a second set.
- Task 6 Step 1 — the render helper and PUT spy in `ProjectSettingsForm.test.tsx`. Same rule.

## When all six are done

Run the plan's Verification section end to end, on the real daemon. The decisive check is the last one:

```bash
sqlite3 ~/.ao/data/ao.db "select kind, substr(payload,1,80), created_at from work_card_events where card_id='<card>' order by created_at;"
```

`agent_transition` currently has zero rows in that table across the whole database. A non-zero count after a Director-driven card completes a phase is the proof this work succeeded. Report the actual row count.

Then report, per task: what changed, the test output, and anything in the plan you could not do and why.
