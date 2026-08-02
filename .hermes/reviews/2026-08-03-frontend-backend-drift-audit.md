# Review: Frontend/backend drift audit — "อะไรที่ frontend แก้แล้วแต่ backend ลืมแก้"

- **Date:** 2026-08-03
- **Branch:** `feat/live-terminals`
- **Commit at time of audit:** `7819f6e0 feat(session): introduce native session ID handling for Claude Code`
- **Uncommitted at time of audit:** `CreateProjectAgentSheet.tsx`/`.test.tsx`, `CreateWorkCardDialog.tsx`/`.test.tsx` (work in progress, not finished)
- **Scope:** ทั้ง repo — หา endpoint/field ที่ frontend เรียกใช้แล้ว backend ไม่รองรับจริง (structural + behavioral), ไม่ใช่ diff เดียว
- **Mode:** read-only audit — ไม่มีการแก้โค้ด (นอกจาก `npm run api` regenerate ซึ่งไม่มี diff เกิดขึ้น)
- **หมายเหตุสำหรับ codex:** repo นี้มี process อื่นแก้โค้ดสดคู่ขนานตลอดเวลา (commit ใหม่โผล่มาหลายรอบระหว่างตรวจงานอื่นในเซสชันนี้ เช่น `4352dd78`, `f9a9ae7f`, `b081ab55`, `759b5783`, `7819f6e0`) — snapshot นี้ valid เฉพาะ ณ commit ข้างบน ถ้า HEAD ขยับไปแล้วให้รันคำสั่งด้านล่างซ้ำก่อนเชื่อผลนี้

---

## วิธีตรวจซ้ำ (ให้ codex รันเองเพื่อ verify)

```bash
cd /Users/up-mac/wokrspace/mind/moden-agent

# 1) contract sync — ถ้ามี diff แปลว่า backend เปลี่ยนแล้ว frontend type ไม่ regenerate ตาม (หรือกลับกัน)
npm run api && git diff --stat -- backend/internal/httpd/apispec/openapi.yaml frontend/src/api/schema.ts

# 2) backend compiles + vets clean
cd backend && go build ./... && go vet ./...

# 3) backend service tests ของ area ที่ frontend เพิ่งแก้
go test ./internal/service/project/... ./internal/httpd/controllers/... ./internal/service/session/...

# 4) frontend typecheck + test ของไฟล์ที่เพิ่งแก้
cd ../frontend
npm run typecheck
npx vitest run src/renderer/components/CreateWorkCardDialog.test.tsx \
  src/renderer/components/CreateProjectAgentSheet.test.tsx \
  src/renderer/components/ShellTopbar.test.tsx \
  src/renderer/components/CodeHomeComposer.test.tsx
```

ทุกคำสั่งข้างบนรันจริงแล้วผ่านหมด ณ commit `7819f6e0` (ดูรายละเอียดด้านล่าง)

---

## ผลตรวจ

### 1. OpenAPI contract sync — ✅ ตรงกัน

`npm run api` regenerate `openapi.yaml` + `schema.ts` แล้ว **diff ว่างเปล่า** — ไม่มี backend route/DTO ไหนเปลี่ยนแล้ว frontend type ค้างของเก่า (หรือกลับกัน)

### 2. `CreateWorkCardDialog.tsx` auto-create project on submit — ✅ มี backend รองรับจริง (ยัง uncommitted)

Diff ที่กำลังแก้อยู่ (`frontend/src/renderer/components/CreateWorkCardDialog.tsx`, uncommitted ตอนตรวจ) เพิ่ม flow: ถ้าไม่มี `projectId` ให้เรียก `POST /api/v1/projects` สร้าง project ใหม่ก่อน ด้วย body:

```ts
{
  path: folderPath,
  config: {
    worker: { agent: WORKBOARD_ORCHESTRATOR_AGENT },
    orchestrator: { agent: WORKBOARD_ORCHESTRATOR_AGENT },
    workboard: DEFAULT_WORKBOARD_CONFIG,   // { wipLimit: 4 }
  },
}
```

ตรวจ backend แล้ว: `backend/internal/service/project/dto.go:13-22` — `AddInput.Config *domain.ProjectConfig` รับ shape นี้ได้ครบ (`worker`, `orchestrator`, `workboard` เป็น field ของ `domain.ProjectConfig` อยู่แล้ว) ไม่ใช่ field ใหม่ที่ backend ไม่มี typecheck สะอาด (`npm run typecheck` ไม่มี error ในไฟล์นี้) test ของไฟล์นี้ผ่าน 20/20 (รวมกับอีก 3 ไฟล์ที่เกี่ยวข้อง)

**ช่องว่างที่เจอ (ไม่ใช่ bug แต่เป็น coverage gap):** ไม่มี backend test ที่ยิง `Config` ครบ 3 sub-field (`worker`+`orchestrator`+`workboard`) พร้อมกันตอน `Add` — test ที่มีอยู่ใน `service_test.go` ยิงทีละ field (`TestManager_UpdateWorkboardAutonomousPreservesProjectConfig` เทส workboard อย่างเดียว, บรรทัด 516 เทส `Config: &cfg` แบบ generic) ไม่มี test เฉพาะ combo ที่ frontend เพิ่งเริ่มยิงจริง แนะนำเพิ่ม 1 test case ก่อน merge งานนี้

### 3. `spawn-worker.ts` (`759b5783`) — ✅ ใช้ endpoint ที่มีอยู่แล้ว ไม่ใช่ของใหม่

`frontend/src/renderer/lib/spawn-worker.ts` เรียก `POST /api/v1/sessions` ด้วย `kind: "worker"` ตรวจ `backend/internal/httpd/controllers/sessions.go:136-137` แล้ว `domain.KindWorker` เป็นค่า default อยู่แล้วเมื่อ `Kind` ว่าง endpoint นี้รองรับ `kind: "worker"` มาก่อนหน้าการแก้ครั้งนี้แล้ว ไม่มีอะไรขาด

### 4. ไฟล์อื่นที่ commit ไปแล้วในช่วงนี้ (`f9a9ae7f`, `b081ab55`, `7819f6e0`) — ตรวจแล้วเป็น backend+frontend คู่กันในตัว

`7819f6e0` แก้ทั้ง `backend/internal/adapters/agent/claudecode/claudecode.go` + `backend/internal/ports/agent.go` + `backend/internal/session_manager/manager.go` และฝั่ง frontend ที่ใช้งานพร้อมกันใน commit เดียว — ไม่ใช่กรณี "frontend ไปก่อน backend"

---

## สรุป

ตรวจ 4 มุม (contract sync, build/vet, backend test, frontend test) ที่ commit `7819f6e0` **ไม่พบจุดที่ frontend เรียกใช้ backend ที่ไม่มีจริง** จุดเดียวที่ควรระวังคือ coverage gap ข้อ 2 (ไม่มี test สำหรับ combo config ใหม่) และงาน `CreateWorkCardDialog.tsx`/`CreateProjectAgentSheet.tsx` ที่ยัง uncommitted ณ ตอนตรวจ — ยังไม่จบงาน ต้องตรวจซ้ำหลัง commit จริง

**ขอให้ codex ตรวจเพิ่ม:** เพิ่ม backend test สำหรับ `AddInput{Config: &domain.ProjectConfig{Worker: ..., Orchestrator: ..., Workboard: ...}}` ยิงพร้อมกันครบ 3 field แล้วยืนยันว่า project ที่สร้างออกมามี config ครบตามที่ frontend ส่งจริง (ไม่ใช่แค่ compile ผ่าน)
