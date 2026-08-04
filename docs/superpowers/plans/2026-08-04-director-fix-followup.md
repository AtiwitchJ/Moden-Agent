# Director Fix Follow-Up Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the seven defects found reviewing the Director-only-commander work, and finally produce the live proof that plan set out to get: a Director that moves a card.

**Architecture:** Six tasks in two groups. Tasks 1-4 are code and repo hygiene, touch disjoint files, and can run in parallel. Tasks 5-6 are live work against the real daemon and must run last, in order, after a rebuild — Task 5 clears the stale sessions the earlier verification left behind, Task 6 drives a real card through a real phase and reads the result out of SQLite.

**Tech Stack:** Go 1.x (`backend/`), Node + esbuild (`director/`), React (`frontend/src/renderer/`), tmux runtime, SQLite store, `ao` CLI.

## Global Constraints

- **No sandbox.** Run every command on this machine against the real `~/.ao` data dir and the real daemon.
- All app state resolves under `~/.ao` (or `AO_DATA_DIR`). Never write to `~/Library/Application Support`.
- Repo files are LF. Do not introduce CRLF.
- `TestLifecycleDispatcherIsUnwired_RunningCardShouldAdvanceToReview_WhenTickRun`, `..._ReviewCardShouldAdvanceToTesting_...` and `..._TestingCardShouldAdvanceToDone_...` in `backend/internal/service/workboard` are **red by design** and were red before this work. Leave them red. Every other test must pass.
- Lint gate is `cd backend && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --path-mode=abs --new-from-rev=55d1eca6`. The repo has 240 pre-existing issues; only new ones count.
- One commit per task. Stage only the files that task owns — never `git add -A`.

## Parallelism

Tasks 1-4 own disjoint files and may run simultaneously:

| Task | Owns |
|---|---|
| 1 | `backend/internal/service/session/service.go`, `service_test.go` |
| 2 | `director/build.mjs`, `backend/internal/service/workboard/actions.go`, `backend/internal/service/workboard/answer.go` |
| 3 | `frontend/src/renderer/components/ProjectSettingsForm.tsx`, `.test.tsx` |
| 4 | `director/dist/` (git index only) |

Tasks 5 and 6 are sequential, run after 1-4 are committed, and touch no source files.

---

### Task 1: Map `ErrDirectorCardRequired` to a typed API error

The spawn guard works — a Director spawn with no card id is refused and creates no session row. But the error reaches the caller as an opaque 500:

```json
{"error":"internal","code":"INTERNAL_ERROR","message":"Internal server error"}
```

with the real cause only in the daemon log: `error="spawn: session: director harness requires a work card id"`.

`toAPIError` (`backend/internal/service/session/service.go:653`) maps both sibling sentinels — `ErrUnknownHarness` and `ErrMissingHarness` (lines 672-675) — to `apierr.Invalid` carrying the message. `ErrDirectorCardRequired` has no case, so it falls through to the generic 500. An invisible refusal is the same failure class the guard was added to remove.

**Files:**
- Modify: `backend/internal/service/session/service.go:674-675` (add a case)
- Test: `backend/internal/service/session/service_test.go:730` (extend the existing table)

**Interfaces:**
- Consumes: `sessionmanager.ErrDirectorCardRequired` (already exists, `backend/internal/session_manager/manager.go:44`).
- Produces: API error code `DIRECTOR_CARD_REQUIRED`, kind `apierr.KindInvalid`. Task 5 and Task 6 assert on it from the wire.

- [ ] **Step 1: Write the failing test**

In `backend/internal/service/session/service_test.go`, add one row to the `TestToAPIErrorMapsWorkspaceBranchSentinels` case table, directly after the `"missing harness"` row (line 730):

```go
		{"director card required", fmt.Errorf("spawn: %w", sessionmanager.ErrDirectorCardRequired), apierr.KindInvalid, "DIRECTOR_CARD_REQUIRED"},
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/service/session/ -run TestToAPIErrorMapsWorkspaceBranchSentinels -v
```

Expected: FAIL on the `director_card_required` subtest — the error maps to nothing, so `errors.As` finds no `*apierr.Error`.

- [ ] **Step 3: Add the case**

In `backend/internal/service/session/service.go`, immediately after the `ErrMissingHarness` case (line 674-675):

```go
	case errors.Is(err, sessionmanager.ErrDirectorCardRequired):
		return apierr.Invalid("DIRECTOR_CARD_REQUIRED", err.Error(), nil)
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd backend && go test ./internal/service/session/ -run TestToAPIErrorMapsWorkspaceBranchSentinels -v
```

Expected: PASS, all subtests including `director_card_required`.

- [ ] **Step 5: Run the package suite**

```bash
cd backend && go test ./internal/service/session/ ./internal/session_manager/
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/session/service.go backend/internal/service/session/service_test.go
git commit -m "fix(session): map ErrDirectorCardRequired to a typed 400

The spawn guard refused the session correctly but toAPIError had no case
for it, so callers got an opaque 500 and the reason only reached the
daemon log. Its two sibling harness sentinels already map to Invalid with
the message; this joins them."
```

---

### Task 2: Three small defects left by the last pass

Independent one-liners, one commit.

**(a) `director/build.mjs` copies the bundle twice.** The same statement and its comment were pasted twice:

```js
// The Go daemon embeds and installs this copy — it, not dist/, is what runs.
// Copying here keeps a src change from silently not shipping.
await copyFile("dist/index.js", "../backend/internal/directorassets/bundle/index.js");

// The Go daemon embeds and installs this copy — it, not dist/, is what runs.
// Copying here keeps a src change from silently not shipping.
await copyFile("dist/index.js", "../backend/internal/directorassets/bundle/index.js");
```

**(b) `backend/internal/service/workboard/actions.go:353` has a stray blank line** left by the `isHermesCommander` → `isCardCommander` rename. It is the only new golangci-lint issue on the branch (`goimports: File is not properly formatted`).

**(c) `backend/internal/service/workboard/answer.go:114` carries a stale comment.** It reads *"Hermes now owns commanded cards directly"*, but the line below it now calls `isCardCommander`, which accepts the Director too.

**Files:**
- Modify: `director/build.mjs`
- Modify: `backend/internal/service/workboard/actions.go:353`
- Modify: `backend/internal/service/workboard/answer.go:113-115`

**Interfaces:**
- Consumes: nothing. Produces: nothing. Behaviour is unchanged by all three.

- [ ] **Step 1: Delete the duplicated copy in build.mjs**

In `director/build.mjs`, remove the second of the two identical `copyFile` blocks so exactly one remains.

- [ ] **Step 2: Verify the build still produces an identical bundle**

```bash
cd director && npm run build
cd .. && cmp director/dist/index.js backend/internal/directorassets/bundle/index.js && echo IDENTICAL
git diff --stat -- director/dist backend/internal/directorassets/bundle
```

Expected: `IDENTICAL`, and `git diff --stat` reports no change to either bundle — the rebuild is byte-for-byte what is already committed.

- [ ] **Step 3: Fix the formatting**

```bash
cd backend && gofmt -w internal/service/workboard/actions.go
git diff -- internal/service/workboard/actions.go
```

Expected: exactly one blank line removed near line 353.

- [ ] **Step 4: Fix the stale comment**

In `backend/internal/service/workboard/answer.go`, replace the two comment lines above the `isCardCommander` call (lines 113-114):

```go
		// The card's commander — Director or Hermes — owns the card directly. It
		// is not the coding worker whose interactive question autonomous
		// answering is designed to handle.
```

- [ ] **Step 5: Verify tests and lint**

```bash
cd backend && go test ./internal/service/workboard/ 2>&1 | tail -8
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --path-mode=abs --new-from-rev=55d1eca6
cd ../director && npm test
```

Expected: only the three `TestLifecycleDispatcherIsUnwired_*` failures; golangci-lint reports **0 issues**; 41 Director tests pass.

- [ ] **Step 6: Commit**

```bash
git add director/build.mjs backend/internal/service/workboard/actions.go backend/internal/service/workboard/answer.go
git commit -m "chore: tidy three leftovers from the Director commander work

build.mjs copied the bundle twice, the isCardCommander rename left a
stray blank line that gofmt flags, and answer.go still described the
predicate as Hermes-only."
```

---

### Task 3: Restore LF line endings in `ProjectSettingsForm`

The API-key commit rewrote both files with CRLF. They are the only CRLF files in the repository:

```
ProjectSettingsForm.tsx      : 625 CR
ProjectSettingsForm.test.tsx : 667 CR
every other source file      : 0 CR
```

The real change was **+111 lines** (`git diff -w`), but the commit landed **545**. Left alone, `git blame` attributes every line of both files to that commit and the next diff will churn again.

**Files:**
- Modify: `frontend/src/renderer/components/ProjectSettingsForm.tsx`
- Modify: `frontend/src/renderer/components/ProjectSettingsForm.test.tsx`

**Interfaces:**
- Consumes: nothing. Produces: nothing. Zero behaviour change — content is byte-identical apart from the line terminators.

- [ ] **Step 1: Confirm the current state**

```bash
cd /Users/up-mac/wokrspace/mind/moden-agent
for f in frontend/src/renderer/components/ProjectSettingsForm.tsx frontend/src/renderer/components/ProjectSettingsForm.test.tsx; do printf "%s: " "$f"; grep -c $'\r' "$f"; done
```

Expected: `625` and `667`.

- [ ] **Step 2: Strip the CRs**

```bash
cd /Users/up-mac/wokrspace/mind/moden-agent
perl -pi -e 's/\r\n/\n/g' frontend/src/renderer/components/ProjectSettingsForm.tsx frontend/src/renderer/components/ProjectSettingsForm.test.tsx
```

- [ ] **Step 3: Verify nothing but line endings changed**

```bash
for f in frontend/src/renderer/components/ProjectSettingsForm.tsx frontend/src/renderer/components/ProjectSettingsForm.test.tsx; do printf "%s: " "$f"; grep -c $'\r' "$f" || echo 0; done
git diff --stat -- frontend/src/renderer/components/ProjectSettingsForm.tsx frontend/src/renderer/components/ProjectSettingsForm.test.tsx
git diff --ignore-cr-at-eol --stat -- frontend/src/renderer/components/ProjectSettingsForm.tsx frontend/src/renderer/components/ProjectSettingsForm.test.tsx
```

Expected: `0` CRs in both; the plain `--stat` shows the whole file rewritten; the `--ignore-cr-at-eol` `--stat` shows **no files changed** — proof this touches nothing but line terminators.

- [ ] **Step 4: Verify the app still builds and tests**

```bash
cd frontend && npm run typecheck && npm test -- --run 2>&1 | tail -6
```

Expected: typecheck clean; 71 files / 565 tests pass.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/renderer/components/ProjectSettingsForm.tsx frontend/src/renderer/components/ProjectSettingsForm.test.tsx
git commit -m "style: restore LF line endings in ProjectSettingsForm

The API-key commit rewrote both files as CRLF — the only two such files
in the repo — turning a 111-line change into a 545-line diff and
reassigning every line in git blame. Content is unchanged."
```

---

### Task 4: Stop tracking the generated `director/dist` bundle

`director/dist/index.js` (134,960 lines) is committed even though `director/.gitignore:2` is `dist/`:

```
$ git check-ignore -v --no-index director/dist/index.js
director/.gitignore:2:dist/	director/dist/index.js
```

It is byte-identical to `backend/internal/directorassets/bundle/index.js`, which is the copy the daemon actually embeds, installs and runs. Tracking both means every Director source change writes ~135k lines of generated output into history twice.

This was the previous plan's own error — its Task 4 Step 10 instructed `git add director/dist/index.js`. Undo it; keep the file on disk, since `build.mjs` writes there and copies from there.

**Files:**
- Modify: git index only. No file contents change.

**Interfaces:**
- Consumes: nothing. Produces: nothing.

- [ ] **Step 1: Confirm the duplication before removing anything**

```bash
cd /Users/up-mac/wokrspace/mind/moden-agent
cmp director/dist/index.js backend/internal/directorassets/bundle/index.js && echo IDENTICAL
git ls-files director/dist/
git ls-files backend/internal/directorassets/bundle/index.js
```

Expected: `IDENTICAL`; `director/dist/index.js` tracked; the embedded bundle tracked. Both present. **If `cmp` reports a difference, stop** — the embedded copy is the one that ships, and a mismatch means the last build did not propagate. Run `cd director && npm run build` first and re-check.

- [ ] **Step 2: Untrack it, keeping the file on disk**

```bash
git rm --cached director/dist/index.js
```

- [ ] **Step 3: Verify the file survived and is now ignored**

```bash
ls -la director/dist/index.js
git status --short director/dist/
```

Expected: the file still exists; `git status` shows `D  director/dist/index.js` staged and lists nothing untracked under `director/dist/` (the `.gitignore` rule now applies).

- [ ] **Step 4: Verify the shipping copy is untouched**

```bash
git status --short backend/internal/directorassets/bundle/index.js
git ls-files backend/internal/directorassets/bundle/index.js
```

Expected: no status output (unchanged), and the path is still tracked. This is the copy that must stay in git.

- [ ] **Step 5: Verify the build still works end to end**

```bash
cd director && npm run build && cd ..
cmp director/dist/index.js backend/internal/directorassets/bundle/index.js && echo IDENTICAL
git status --short
```

Expected: `IDENTICAL`; `git status` still shows only the staged deletion — the rebuild reproduces the committed embedded bundle exactly.

- [ ] **Step 6: Commit**

```bash
git commit -m "chore(director): stop tracking the generated dist bundle

director/.gitignore already lists dist/, and dist/index.js is byte-for-byte
the embedded copy at backend/internal/directorassets/bundle/index.js that
the daemon actually installs and runs. Tracking both wrote ~135k lines of
generated output into history twice per Director change."
```

---

### Task 5: Clear the stale live sessions and rebuild the daemon

Two Director sessions in `~/.ao/data/ao.db` are debris, and both mislead any later check:

| Session | Why it is debris |
|---|---|
| `test-13` (`orchestrator`, `director`, `is_terminated=0`) | Spawned before the guard existed with no card id. Its Director exited at startup; its tmux pane is a bare `zsh`. It reports itself live and is not. |
| `test-14` (`worker`, `director`, `prompt="probe"`, `is_terminated=0`) | The previous plan's verification probe. It was **supposed to be rejected**, but ran against a daemon built before the guard, so it succeeded and left a row. Its existence is the evidence that check was invalid. |

The running daemon must also be rebuilt: the one that answered the earlier verification predates Tasks 1-4 of both plans.

**Files:** none. Live state only.

**Interfaces:**
- Consumes: `DIRECTOR_CARD_REQUIRED` from Task 1.
- Produces: a clean session table and a daemon built from current `HEAD`, both required by Task 6.

- [ ] **Step 1: Record the state you are about to change**

```bash
sqlite3 ~/.ao/data/ao.db "select id,kind,harness,is_terminated,substr(prompt,1,40) from sessions where harness='director';"
tmux list-panes -a -F "#{session_name} #{pane_current_command}"
ps aux | grep "[d]irector/dist/index.js" | wc -l
```

Paste the output. Expected before the change: `test-13` and `test-14` both `is_terminated=0`, every pane `zsh`, zero Director processes.

- [ ] **Step 2: Kill both stale sessions**

These are the user's own test-project sessions and neither is running an agent — both panes are bare shells. Killing them terminates the tmux session and marks the row terminated; it does not delete work cards.

```bash
ao session kill test-13
ao session kill test-14
```

- [ ] **Step 3: Verify they are gone**

```bash
sqlite3 ~/.ao/data/ao.db "select id,kind,harness,is_terminated from sessions where harness='director';"
tmux list-panes -a -F "#{session_name}" | sort
```

Expected: both rows now `is_terminated=1`, and neither `test-13` nor `test-14` appears in tmux.

- [ ] **Step 4: Rebuild and restart the daemon from current HEAD**

Stop the running daemon, then start it the way this machine normally does (the Electron app owns it — `~/.ao/running.json` shows `"owner": "app"`). Restart the app, or run the daemon directly from the repo. Either way, confirm afterwards that the binary is newer than your last commit:

```bash
cat ~/.ao/running.json
ps -p "$(python3 -c "import json;print(json.load(open('$HOME/.ao/running.json'))['pid'])")" -o lstart=,command=
git log -1 --format=%cd
```

Expected: the daemon process start time is **after** the last commit's date.

- [ ] **Step 5: Prove the guard now reports itself properly**

This is the check that was invalid last time. Run it against the freshly built daemon:

```bash
curl -s -X POST http://127.0.0.1:3001/api/v1/sessions \
  -H 'Content-Type: application/json' \
  -d '{"projectId":"test","kind":"worker","harness":"director","prompt":"probe3"}'
echo
sqlite3 ~/.ao/data/ao.db "select id,kind,harness,prompt from sessions where harness='director' order by created_at;"
```

Expected: a **400** body carrying `"code":"DIRECTOR_CARD_REQUIRED"` and the message `director harness requires a work card id` — not `INTERNAL_ERROR`. And **no new row**: the session list must be unchanged from Step 3.

- [ ] **Step 6: Record the baseline for Task 6**

```bash
sqlite3 ~/.ao/data/ao.db "select kind, count(*) from work_card_events group by kind;"
```

Expected today: `stall_nudged|3` and nothing else. Zero `agent_handoff`, zero `agent_transition`. Task 6 exists to change this.

---

### Task 6: Live proof — a Director drives a card through a phase

Everything so far is unproven at runtime. `work_card_events` has never held a single `agent_transition` row; no Director process has ever run. This task produces the evidence or finds out why it cannot.

**Files:** none. Live run only.

**Interfaces:**
- Consumes: a clean session table and a current daemon (Task 5).
- Produces: the answer to "does the Director move cards".

- [ ] **Step 1: Give the project a Director API key through the UI**

This also verifies the previous plan's Task 6 for real. Open project settings for `test` in the app, put a valid key in the **Director API key** field, save. Then confirm it landed:

```bash
sqlite3 ~/.ao/data/ao.db "select json_extract(config,'\$.env'), json_extract(config,'\$.agentConfig.model') from projects where id='test';"
```

Expected: the env object contains the var matching the configured engine — `ANTHROPIC_API_KEY` for the default `anthropic:` model, `OPENAI_API_KEY` for `openai:`, `OPENROUTER_API_KEY` for `openrouter:`.

If the field is missing from the settings screen, stop and report that — the previous plan's Task 6 did not actually ship. The CLI fallback exists but does not substitute for the check:

```bash
ao project set-config test --env ANTHROPIC_API_KEY=<key>
```

- [ ] **Step 2: Create a small work card**

Use the workboard UI in the app. Make the task genuinely small and verifiable — for example *"add a CHANGELOG.md with one dated entry"* — so a phase can actually complete inside the Director's iteration budget. Note the card id:

```bash
sqlite3 ~/.ao/data/ao.db "select id,status,session_id,target_path from work_cards order by created_at desc limit 1;"
```

- [ ] **Step 3: Watch the Director actually start**

Within about a minute of the card reaching `todo`/`ready`, dispatch should claim it and spawn a Director:

```bash
sqlite3 ~/.ao/data/ao.db "select id,kind,harness,is_terminated,created_at from sessions where harness='director' and is_terminated=0;"
ps aux | grep "[d]irector/dist/index.js"
tmux list-panes -a -F "#{session_name} #{pane_current_command}"
```

Expected: one live Director session, **one `node` process**, and its tmux pane running `node` — not `zsh`. A `zsh` pane means the Director exited at startup; read why before going further:

```bash
tmux capture-pane -p -t <director-session-id> -S -40
```

- [ ] **Step 4: Confirm exactly one commander is on the card**

Task 2 of the previous plan gated the Hermes orchestrator tick off for Director projects. Verify no second commander appeared:

```bash
sqlite3 ~/.ao/data/ao.db "select id,kind,harness,is_terminated from sessions where project_id='test' and kind='orchestrator' and is_terminated=0;"
sqlite3 ~/.ao/data/ao.db "select * from active_session;"
```

Expected: exactly one live orchestrator-kind session, and it is the Director.

- [ ] **Step 5: Let the phase complete, then read the events**

Give the Director's worker time to finish the coding phase and send its handoff. Then:

```bash
sqlite3 ~/.ao/data/ao.db "select kind, substr(payload,1,120), created_at from work_card_events where card_id='<card>' order by created_at;"
sqlite3 ~/.ao/data/ao.db "select id,status from work_cards where id='<card>';"
```

Expected, and this is the whole point of both plans: an `agent_handoff` row, then an `agent_transition` row, and the card's status past `running`.

- [ ] **Step 6: Confirm the handoff reached the Director whole**

Task 4 of the previous plan replaced per-line stdin framing with a buffered whole-message read. Check the Director's own pane for how the report arrived:

```bash
tmux capture-pane -p -t <director-session-id> -S -120 | tail -60
```

Expected: the worker's handoff appears as **one** message under `A worker session sent this message on your terminal channel:` — not split across several turns, and not labelled a question.

- [ ] **Step 7: Report the result**

State plainly which of these happened:

- Director process ran (yes/no)
- Exactly one commander (yes/no)
- `agent_handoff` row count
- `agent_transition` row count
- Final card status

If `agent_transition` is still zero, do **not** call this fixed. Report the pane output and where the chain broke — that is a new debugging problem, and its cause belongs in a fresh investigation rather than a patch on top of this plan.

---

## Out of scope

- **The tmux `; exec $SHELL` tail** (`backend/internal/adapters/runtime/tmux/tmux.go:484`). It keeps a pane alive after the agent exits, so every crashed agent leaves a session that reports itself live — the reason `test-13` looked healthy for a day. Deliberate, and it affects all ten harnesses. Recording agent exit separately from pane liveness is its own change.
- **Stall-nudge wording for the Director.** It is now eligible for nudges, but the message tells it to *"Run `ao workboard get ...`"* — shell instructions for a loop that takes tool calls. Rewrite is a separate change.
- **`ProjectConfig` validation** rejecting `orchestrator.agent` and `director.agent` together. The Director wins at runtime; refusing the combination at write time is a larger change across the config surface and its migration.
- **The 240 pre-existing golangci-lint issues** and the three red-by-design `TestLifecycleDispatcherIsUnwired_*` tests. Both predate this work.
