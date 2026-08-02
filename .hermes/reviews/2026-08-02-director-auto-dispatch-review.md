# Review: Director Auto-dispatch (end-to-end)

- **Date:** 2026-08-02
- **Branch:** `feat/live-terminals`
- **Commit:** `3c551a3a9599d5acaceb63542420e33298414029`
- **Scope:** ทุก task ใน `.hermes/plans/2026-08-02_183500-director-auto-dispatch.md` + commit history ที่เกี่ยวข้อง (`ef998bd5` → `3c551a3a`)
- **Mode:** read-only review — ไม่มีการแก้ไฟล์/commit ใด ๆ (ไฟล์รายงานนี้ไฟล์เดียวที่ถูกสร้าง)

---

## ผลรันคำสั่งตรวจ (ที่ HEAD)

| คำสั่ง | ผล |
| --- | --- |
| `cd backend && go test ./...` | ✅ ผ่านหมด |
| `npm run api` | ✅ regenerate แล้ว **ไม่มี diff** — DTO / `openapi.yaml` / `schema.ts` sync กัน |
| `npm run frontend:typecheck` | ❌ **FAIL 4 errors** (ดู C-2) |
| Vitest focused 4 ไฟล์ | ❌ **Workboard.test.tsx fail 10/10** — อีก 3 ไฟล์ผ่าน (25 tests) |

Typecheck errors จริงที่ HEAD:

```
src/renderer/components/Workboard.tsx(2,50): error TS6133: 'Wifi' is declared but its value is never read.
src/renderer/components/Workboard.tsx(2,56): error TS6133: 'WifiOff' is declared but its value is never read.
src/renderer/components/Workboard.tsx(47,40): error TS2339: Property 'readLog' does not exist on type '{ getStatus...; start...; stop...; onStatus... }'.
src/renderer/test/setup.ts(71,4): error TS2353: Object literal may only specify known properties, and 'readLog' does not exist in type ...
```

Vitest failure จริง:

```
TypeError: useDirectorStatus is not a function
 ❯ Workboard src/renderer/components/Workboard.tsx:165:30
Test Files  1 failed | 3 passed (4)
Tests       10 failed | 25 passed (35)
```

---

## CRITICAL

### C-1. Daemon log (Task 5) เป็น dead code ทั้งเส้น — UI พังจริงใน production

- `frontend/src/main/daemon-log.ts` มี implementation + unit test ครบ แต่ **ไม่มีใคร import** — `main.ts` ไม่เคยเรียก `createDaemonLog`
- stderr ของ daemon ไปแค่ `console.error` (`frontend/src/main.ts:707`) — ไฟล์ `~/.ao/data/daemon.log` **ไม่เคยถูกเขียนเลย** (ไม่มี writer ที่อื่นใน backend ด้วย — ตรวจแล้ว)
- ไม่มี IPC handler `daemon:readLog`; `frontend/src/preload.ts:32-43` ไม่มี `readLog`; fallback ใน `frontend/src/renderer/lib/bridge.ts` ก็ไม่มี
- `frontend/src/renderer/components/Workboard.tsx:47` เรียก `aoBridge.daemon.readLog(200)` → runtime TypeError ทุกครั้ง → กด "View daemon log" ได้แค่ "Could not read daemon log."
- **ทำไม test ผ่าน:** mock ล้วน — `frontend/src/renderer/test/setup.ts:71` stub `readLog` (ตัวเดียวกับที่ typecheck ฟ้อง TS2353) + `aoBridgeMock` ใน Workboard.test.tsx — ตัวอย่างคลาสสิก "mock ทำให้ test ผ่านแต่ behavior จริงผิด"

**Fix:** main.ts tee child.stdout/stderr → `createDaemonLog().write()`; เพิ่ม `ipcMain.handle("daemon:readLog")`; เพิ่ม `readLog` ใน preload `AoBridge.daemon` + bridge fallback; ลบ stub ที่ผิด type ออก

### C-2. Verification claims ที่ HEAD เป็นเท็จ

- Commit `3c551a3a` message อ้าง "npm run api and npm run frontend:typecheck pass" — typecheck **fail 4 errors** จริง (unused `Wifi`/`WifiOff` ก็ค้าง แปลว่าไม่ได้รัน tsc หลัง Task 5)
- Workboard.test.tsx พังตั้งแต่ commit Task 4 (`8eeb99a9` เพิ่ม `useDirectorStatus` ใน component แต่ `vi.mock("../hooks/useWorkboardQuery")` ที่ `Workboard.test.tsx:19-23` ไม่ export hook นี้) — DirectorStatusBar tests ทั้ง 5 ตัวใน commit `7fa63587` **ไม่เคยรันผ่านจริง**
- memory-bank (`activeContext.md`, `progress.md`) อ้าง commit `b04a8f33` ซึ่ง**ไม่อยู่ใน branch นี้** — ผล verify ที่บันทึกไว้ผูกกับ history อื่น ตรวจกับ HEAD ไม่ได้

**Fix mock:** เพิ่ม `useDirectorStatus`, `useWorkCardDispatchFailure`, `useDispatchProject` เข้า mock factory (หรือใช้ `importOriginal`)

### C-3. `LastDispatchAttempt` ระเหยทันทีหลัง dispatch จบ — Task 4 acceptance พัง

`backend/internal/daemon/dispatch_trigger.go:135-142` — จบ pass แล้วถ้าไม่มี kick ค้าง:

```go
t.mu.Lock()
project = t.projects[projectID]
if project == nil || t.closing || t.ctx.Err() != nil || !project.pending {
    delete(t.projects, projectID)   // ← ลบ entry รวมทั้ง project.last ที่เพิ่งเซ็ต
    t.mu.Unlock()
    return
}
```

- `LastDispatchAttempt(projectID)` คืน zero value เกือบตลอดเวลา (เหลือค่าเฉพาะจังหวะ dispatch in-flight)
- ผล: `DirectorStatusResponse.lastDispatchAttempt` ว่างตลอด → UI แยก "Last dispatch failed" / "Queue full" ไม่ได้จริง → acceptance ของ Task 4 ("ผู้ใช้แยก 3 สถานะได้โดยไม่เปิด terminal") ไม่ผ่าน
- **DispatchTrigger ไม่มี unit test เลยสักไฟล์** — เลยไม่มีอะไรจับ bug นี้

**Fix:** เก็บ `last` แยกจาก lifecycle ของ worker entry (map แยก `lastResults map[string]DispatchResult` ไม่ลบตาม entry) + เพิ่ม test

### C-4. Smoke checklist ติ๊กเท็จ (Task 6)

- Plan ติ๊ก `[x]` "Smoke test in the desktop app" แต่ `memory-bank/activeContext.md:29` ยอมรับเอง: "Desktop smoke was not run"
- Smoke expectations หลายข้อจะ **fail จริงถ้าไปรัน**:
  - Test 2 "~10 seconds หลัง complete card" → จริงคือรอ poll 1 นาที (ดู M-2)
  - Test 3 reason `hermes_unavailable` → unreachable ใน production (ดู M-1)
  - Test 4 header wording "Auto-dispatch is offline. Reconnect Modern Agent to resume Todo cards." → จริงแสดงแค่ "Daemon offline" / daemon message
  - Verification curl ใช้ path `/workboard/status` → route จริงคือ `/workboard/director-status`

---

## MEDIUM

### M-1. `hermes_unavailable` เป็น reason ที่ unreachable ใน production

- `ErrHermesUnavailable` ประกาศที่ `backend/internal/service/workboard/dispatch.go:45` แต่**ไม่มี production code คืน error นี้เลย** — session service (`SpawnOrchestrator`) ไม่รู้จัก sentinel นี้
- Test `TestDispatchOnce_HermesUnavailableReleasesCardWithoutWorker` ใช้ fake spawner ยิง sentinel เอง (false-positive style) — Hermes ล่มจริงได้ `spawn_failed` แทน
- ด้านดี: ไม่มี fallback spawn worker ตรงสำหรับ Hermes project — recover คืน card เข้า Todo เสมอ (checklist ข้อ 5 ผ่านในแง่นั้น)

**Fix:** session service ต้อง wrap/คืน `ErrHermesUnavailable` ในเงื่อนไขที่เหมาะ (เช่น spawn Hermes ไม่ได้ / Send ไป commander ไม่ได้) หรือถอด reason นี้ออกจาก enum

### M-2. ไม่มี Kick ตอน WIP slot ว่าง

- `kickDispatchIfTodo` ยิงเฉพาะ status `todo`/`ready` — ย้าย card ออกจาก Running (review/done), delete card, session ตาย → **ไม่ kick** → card ที่รอต้องรอ periodic poll 1 นาที
- Kill session ทิ้ง card ค้าง `Running` กิน WIP ตลอด — ไม่มี reconciler ย้าย card ที่ session terminated ออกจาก Running (Hermes เป็นคนย้ายผ่าน CLI; ถ้า commander ตายเอง = ค้าง)

**Fix:** kick เมื่อ transition ออกจาก running ด้วย (Move/Delete) + พิจารณา reconciler สำหรับ running-card-with-terminated-session

### M-3. Semantics ของ trigger result ผิด

`dispatch_trigger.go:125-127`: `claimed == 0` ⇒ `"wip_full"` — แต่ claimed ว่างได้จากหลายเหตุ:

- board ว่าง / ไม่มี candidate เลย → รายงาน "wip_full" (UI: "Queue full") ทั้งที่ idle
- ทุก card spawn fail แบบ recoverable → "wip_full" ทั้งที่ควรเป็น error signal
- และ pass ที่ card หนึ่ง fail แต่ใบอื่นติด ⇒ `"success"` เฉย ๆ

**Fix:** ให้ `DispatchOnce` คืนข้อมูลแยก (claimed / failed / no-candidates / wip-blocked) แล้ว map result ตามจริง

### M-4. Spawn error ถูกกลืนเงียบ — diagnose ไม่ได้

`recoverCardFromSpawnFailure` (dispatch.go:307) ไม่ log `spawnErr` เลย — Dispatcher ไม่มี logger ต่อให้ daemon.log ถูก wire จริง (C-1) ก็ไม่มีบรรทัดให้ diagnose failed dispatch ตามเป้า Task 5 (event เก็บแค่ safe reason code ซึ่งถูกต้องสำหรับ API แต่ราย error จริงหายไปเลย)

**Fix:** inject logger เข้า `DispatchDeps` แล้ว `logger.Warn` spawnErr ตอน recover

### M-5. 404 detection ใน `useWorkCardDispatchFailure` พัง

`frontend/src/renderer/hooks/useWorkboardQuery.ts:61`:

```ts
if (error && typeof error === "object" && "status" in error && error.status === 404) {
```

Error body ของ openapi-fetch = `{error, code, message, requestId}` (envelope `APIError`) — **ไม่มี field `status`** → card Todo ปกติ (ไม่มี failure record) query จะ throw + retry แทน "quiet absence" (UI ไม่แตกเพราะไม่ render `failureQuery.error` แต่ intent ผิด + query noise)

**Fix:** เช็ค `error.code === "WORK_CARD_DISPATCH_FAILURE_NOT_FOUND"` หรือใช้ `response.status` จาก openapi-fetch

### M-6. Race: briefing รั่วไปหา non-Hermes orchestrator

- Dispatcher pre-check `hasActiveNonHermesOrchestrator` แล้วค่อยเรียก `SpawnOrchestrator(clean=false)`
- `backend/internal/service/session/service.go:330-337` reuse "newest active orchestrator" **โดยไม่กรอง harness** และ `Send(prompt)` ทันที
- Post-check ของ dispatcher (kind/harness) จับได้และคืน card เข้า Todo แต่ **briefing ถูกส่งไปแล้ว undo ไม่ได้** (ponytail comment ใน session service ยอมรับ check-then-spawn ไม่ atomic อยู่แล้ว)

**Fix:** ให้ reuse path กรอง harness ตาม project config หรือเช็ค harness ก่อน Send

---

## MINOR

| # | เรื่อง | ที่ |
| --- | --- | --- |
| m-1 | Briefing marshal fail → `return claimed, briefErr` หลัง claim ชนะ โดยไม่ release → card ค้าง Running ไม่มี session (โอกาสเกิดแทบศูนย์ แต่เป็นรูใน invariant ข้อ "ไม่ค้าง Running โดยไม่มี session") | `dispatch.go:234-236` |
| m-2 | FIFO tie พังกรณี batch: promotion รอบเดียวให้ `ReadyAt` เท่ากันหมด → tie-break ด้วย card ID (uuid lexicographic) ไม่ใช่ creation order — smoke Test 6 เชื่อถือไม่ได้หลัง daemon downtime | `dispatch.go:186-196` |
| m-3 | `readTail` รวม fileTail + memory โดย line เดียวกันอยู่ทั้งสองที่ → บรรทัดซ้ำ (ยิ่งไม่สำคัญเพราะ dead code ตาม C-1) | `daemon-log.ts:84-90` |
| m-4 | Dead fields: `WorkboardController.StatusProvider` (ไม่มีใครอ่าน), `dispatchProject.running` | `workboard.go:39`, `dispatch_trigger.go:36` |
| m-5 | systemPatterns.md อ้าง "focused failed-card panel filters diagnostics to daemon/dispatch/Hermes lines" — code ไม่มี filtering ใด ๆ (readLog raw 200 บรรทัด) | `memory-bank/systemPatterns.md:15` |
| m-6 | Offline wording ไม่ตรง plan: header แสดง "Daemon offline" generic ไม่ใช่ "…reconnect Modern Agent to resume Todo cards." (focus panel มีเวอร์ชันย่อถูกต้องกว่า) | `Workboard.tsx:65-85` |
| m-7 | `LastDispatchAttempt DispatchAttemptResponse \`json:"...,omitempty"\`` — omitempty บน struct เป็น no-op, serialize zero value เสมอ (frontend guard ไว้แล้ว harmless) | `dto.go:333` |
| m-8 | ตอน WIP เต็มแล้ว break: เฉพาะ candidate ปัจจุบันที่เคยเป็น Todo ถูกคืน Todo; ใบอื่นที่ promote เป็น Ready ไปแล้วค้าง Ready durably — board map `ready`→Todo column อยู่แล้ว เลย cosmetic เท่านั้น | `dispatch.go:206-216` |
| m-9 | `DispatchOnce` ctx timeout 30s ต่อ pass — Hermes cold spawn ช้ากว่านั้นจะโดน mark `spawn_failed` ทั้งที่ spawn อาจกำลังเสร็จ (spawnErr path ไม่ rollback session ที่อาจเกิดค้าง) | `dispatch_trigger.go:114` |

---

## สิ่งที่ตรวจแล้วถูกต้อง (ตาม checklist โจทย์)

| ข้อ | ผล | หลักฐาน |
| --- | --- | --- |
| 1. Todo auto-dispatch ไม่ต้องลาก | ✅ | kick ที่ Create/Move/Update/Split (`service.go:334,436,511`, `actions.go:328-329`) + boot poll + 1-min tick |
| 2. WIP 4/project, ปลอดภัย multi-kick | ✅ | default 4 ทั้ง `domain/workboard.go:363` และ `workboard-config.ts:6`; claim = single atomic UPDATE + COUNT subquery (`queries/workboard.sql:39-49`) + `writeMu`; concurrency test ใช้ sqlite จริง + barrier (`TestDispatchOnce_TwoDispatchersAtomicallyRespectWIPLimit`) |
| 3. ใบที่ 5 ค้าง Todo | ✅ | `TestDispatchOnce_FiveTodoCardsClaimsFourAndLeavesFifthInTodo` + `wasTodo` คืนใบแพ้ claim กลับ Todo |
| 4. Hermes ผ่าน commander เท่านั้น | ✅ (ยกเว้น race M-6) | `SpawnOrchestrator` reuse ตัวเดียว, brief ผ่าน prompt, card.sessionId ชี้ commander |
| 5. ไม่ fallback spawn ตรง | ✅ โครง / ⚠️ reason ผิด (M-1) | recover คืน Todo เสมอ ไม่แตะ commander |
| 6. spawn failure isolation | ✅ โครงหลัก / ⚠️ m-1, M-4 | release claim + `dispatch_failed` event (safe payload) + continue ใบถัดไป; fatal cases แยกถูก |
| 7. Retry ผ่าน daemon trigger | ✅ | controller เรียกแค่ `Kick` ตอบ 202 (`controllers/workboard.go:134-146`) |
| 8. API/DTO/OpenAPI/schema.ts ตรงกัน | ✅ | `npm run api` ไม่มี diff; specgen มี entry ครบ |
| 9. Director UI แยก 3 สถานะ | ❌ | daemon offline ✅ / WIP เต็ม + card fail ❌ เพราะ C-3 (`lastDispatchAttempt` ว่างตลอด) + M-3 |
| 10. Focus panel ไม่ regress | ✅ | nudge/retarget/split/delete/terminal opt-in/commander wording — WorkCardFocusPanel tests ผ่าน; Retry disabled ตอน offline |
| 11. daemon log | ❌ | path ถูก (`~/.ao/data/`) + bounded/rotated ถูกใน module แต่ **ไม่ถูก wire เลย** (C-1); "View daemon log" gate เฉพาะ unhealthy ✅ ใน UI |
| 12. race/cancellation/stale/false-positive | ⚠️ | พบ C-3 (stale/หาย), M-2 (stale Running), M-6 (race), C-1+C-2 (mock false-positive), trigger ไม่มี test |
| 13. smoke checklist | ❌ | ติ๊กแต่ไม่ได้ทำจริง (C-4) |

---

## Bottom line

Tasks 1–3 + API contract แน่นจริง ทดสอบดี (atomic claim, isolation, WIP=4)
Task 4 acceptance พังเพราะ trigger ลบ `last` ทิ้ง (C-3)
Task 5 ครึ่ง log ไม่ถูก wire เลย — dead code + mock บัง (C-1)
Task 6 ติ๊ก complete โดย typecheck fail / test suite พัง / desktop smoke ไม่ได้ทำ (C-2, C-4)

**Branch นี้ยังไม่ควรถือว่า complete ตามที่ plan อ้าง**

ลำดับซ่อมแนะนำ: C-2 (แก้ mock + typecheck ให้ CI เขียว) → C-1 (wire daemon log จริง) → C-3 (+ trigger tests) → M-1..M-6 → minor ตามสะดวก

---

## Post-fix verification (codex รอบแรก — ตรวจ 2026-08-02)

Codex แก้ C-1, C-2, C-3 แล้ว (working tree, ยังไม่ commit) ตรวจแล้วดังนี้:

### ผ่าน

| ข้อ | ผลตรวจ |
| --- | --- |
| C-2 | ✅ mock เพิ่ม `useDirectorStatus`/`useWorkCardDispatchFailure`/`useDispatchProject` + reset ใน beforeEach; ลบ `Wifi`/`WifiOff`; **typecheck clean**, Workboard.test.tsx ผ่านครบ |
| C-1 | ✅ wire ครบเส้น: `main.ts` tee stdout+stderr → `createDaemonLog().write()`, `close()` ตอน exit, `ipcMain.handle("daemon:readLog")` (fallback เปิด log ใหม่กรณี attach path), preload + bridge fallback มี `readLog` แล้ว |
| C-3 | ✅ `lastResults map[string]DispatchResult` แยกจาก `projects` entry, `LastDispatchAttempt` อ่านจาก map ใหม่; test ใหม่ `TestDispatchTriggerRetainsLastAttemptAfterWorkerExits` ผ่าน (`-count=1` fresh) — test นี้ fail บน code เก่าแน่นอนเพราะ entry โดน delete |

Verification จริงที่รัน:

```
go test -count=1 ./internal/daemon/... ./internal/service/workboard/...  → ok ทั้งหมด
go test ./internal/httpd/...                                             → ok
npm run frontend:typecheck                                               → clean (0 errors)
vitest: Workboard + WorkCardFocusPanel + useDaemonStatus + ProjectSettingsForm → ผ่านครบ 38 tests
```

### พบใหม่ / ค้าง

1. **daemon-log.test.ts fail 2/5 — pre-existing ที่ HEAD** (ยืนยันด้วย stash แล้วรันที่ clean HEAD: fail เหมือนกัน; codex ไม่ได้แตะไฟล์นี้)
   - `rotates the active file when it exceeds maxBytes` — archive ได้ `line two` แทน `line one` (rotation timing ผิดจาก expectation)
   - `falls back to reading the disk file...` — ได้ `['gamma','delta','delta']` แทน `['beta','gamma','delta']` = **บรรทัดซ้ำจาก readTail merge (= m-3 ในรายงาน)**
   - สำคัญขึ้นเพราะ C-1 ทำให้ module นี้ live ใน production path แล้ว — "View daemon log" จะแสดงบรรทัดซ้ำจริง
   - แปลว่า commit `7fa63587` ที่อ้าง daemon-log tests ผ่าน ก็เป็น false claim อีกจุด
2. Trigger test ใหม่ครอบแค่ 1 กรณี — ไม่มี kick-coalescing / `Ready()` หลัง ctx cancel ตามที่ขอ
3. `lastResults` มี nil-guard ใน dispatchProject ที่มีไว้เพื่อ test ที่สร้าง struct ตรง ๆ (ไม่ผ่าน `NewDispatchTrigger`) — hacky เล็กน้อย ไม่ผิด
4. M-1..M-6 + minor ทั้งหมดยังไม่ถูกแตะ

**สถานะ:** CRITICAL 3 ข้อปิดแล้ว verify จริง; งานต่อไป = แก้ daemon-log 2 bugs (rotation + dedupe) แล้วค่อย M-1..M-6

---

## Post-fix verification รอบสอง — daemon-log rotation + dedupe (2026-08-02)

แก้ 2 bugs ที่พบใน `frontend/src/main/daemon-log.ts`:

**Bug A (rotation clobber):** `appendLine` เช็ค `info.size + Buffer.byteLength(data) > maxBytes` ("การเขียนบรรทัดนี้จะทำให้เกินไหม") แทนที่จะเช็ค "ไฟล์เกิน limit อยู่แล้วหรือยัง" — ผลคือเขียนบรรทัดยาวบรรทัดเดียวที่เกือบเท่า `maxBytes` แล้วบรรทัดถัดไป (แม้สั้น) ก็ trigger rotate อีกรอบทันที ด้วย `maxFiles=1` (single backup slot) การ rotate สองรอบติดกัน = archive รอบแรกโดน overwrite ก่อนใครอ่านทัน

Fix: เปลี่ยนเงื่อนไขเป็น `info.size >= this.maxBytes` (เช็คขนาดไฟล์ก่อนเขียนบรรทัดนี้ ไม่รวมบรรทัดใหม่) — ทำให้ rotate เกิดแบบ lazy คือไฟล์ยอมเกิน cap ได้ชั่วคราวสูงสุด 1 บรรทัด ก่อน rotate รอบถัดไป ซึ่งสอดคล้องกับ pattern rotation ทั่วไป (logrotate ก็ยอม overflow ประมาณนี้) และป้องกัน double-rotate ในจังหวะเดียวกัน

**Bug B (readTail dedupe):** `readTail` merge `fileTail` (อ่านจากดิสก์) เข้ากับ `memory` ตรง ๆ โดยไม่ตัด overlap — ทุกบรรทัดใน `memory` ก็ถูกเขียนลงดิสก์ด้วยเสมอ (จาก `write()` ที่เรียกทั้ง `pushMemory` และ append คู่กัน) ผลคือบรรทัดล่าสุดถูกนับซ้ำ (ตัวอย่างจาก test: ได้ `['gamma','delta','delta']` แทน `['beta','gamma','delta']`)

Fix: (1) `await this.writeQueue` ก่อนอ่าน เพื่อการันตีว่าดิสก์มีบรรทัดครบเท่ากับที่อยู่ใน memory ณ ขณะนั้น (ปิด race ที่ readTail อาจถูกเรียกก่อน write ค้างคิวเสร็จ) (2) อ่าน `readFileTail(path, maxLines + memory.length)` แล้วตัด `memory.length` บรรทัดสุดท้ายออกจาก `fileTail` ก่อน concat กับ `memory` — ตัด overlap ที่ซ้ำกันแน่นอนตามลำดับ

### Evidence (รันจริงหลังแก้)

```
npx vitest run src/main/daemon-log.test.ts
  → Test Files 1 passed | Tests 5 passed (5)   [เดิม 2 failed]

npm run frontend:typecheck
  → clean, 0 errors

npx vitest run src/renderer/components/Workboard.test.tsx \
  src/renderer/components/WorkCardFocusPanel.test.tsx \
  src/renderer/hooks/useDaemonStatus.test.tsx \
  src/renderer/components/ProjectSettingsForm.test.tsx \
  src/main/daemon-log.test.ts
  → Test Files 5 passed (5) | Tests 40 passed (40)
```

ตรวจ caller อื่นของ `readTail`/`rotate` — มีแค่ `main.ts:811` (`ipcMain.handle("daemon:readLog")`) ไม่มี consumer อื่นพึ่ง semantics เดิม — ปลอดภัยที่จะเปลี่ยน

**สถานะ:** CRITICAL ทั้ง 3 ข้อ + daemon-log rotation/dedupe bugs ปิดครบ, verify จริงทุกจุด, ยังไม่ commit งานค้าง: trigger test coverage เพิ่ม (coalescing, `Ready()` after cancel), M-1..M-6, minor items
