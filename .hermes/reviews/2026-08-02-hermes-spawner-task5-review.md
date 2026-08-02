# Review: Hermes-aware Spawner with Per-card Locking (Task 5)

- **Date:** 2026-08-02
- **Branch:** `task5-active-session`
- **Commit:** `b8bc72710395e03507a25c38e575797e99237fe7`
- **Scope:** `backend/internal/commander/spawner/{spawner.go,spawner_test.go}`, `backend/internal/storage/sqlite/migrations/0036_active_session.sql`, `backend/internal/storage/sqlite/queries/active_session.sql`, generated `gen/active_session.sql.go` + `gen/models.go`. Reviewed against Task 5 of `.hermes/plans/2026-08-02_223800-hermes-director-orchestrator.md`.
- **Mode:** read-only review — no files edited, no commits made (this report is the only file created).

---

## ผลตรวจ

| คำสั่ง | ผล |
| --- | --- |
| `git show --stat b8bc7271` | 8 files, 1247 insertions |
| grep หา `"already running"` ทั้ง repo | **ไม่พบ** — string ที่ commit message อ้างว่า error คืนไม่มีอยู่จริง |
| grep หา implementation ของ `ActiveSessionStore` | ไม่มี concrete impl ใน repo — มีแต่ test fake (`fakeStore`, `concurrentStore`) |
| grep หา call site ของ `spawner.New(` | มีแค่ `spawner_test.go` — ยังไม่ถูก wire เข้า daemon/orchestrator (คาดว่าเป็นงาน Task 6) |

---

## CRITICAL

### C-1. Per-card lock ไม่ทำงานจริง — commit message claim เป็นเท็จ

`spawner.go:82-94`:

```go
func (l *cardLock) Acquire(cardID string) func() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, busy := l.active[cardID]; busy {
		return func() {}   // no-op release, ไม่มี signal บอกว่า busy
	}
	l.active[cardID] = struct{}{}
	return func() { ... }
}
```

`Spawn()` (`spawner.go:161-163`) เรียกแบบนี้:

```go
cardLock := s.lockFor(spec.CardID)
release := cardLock.Acquire(spec.CardID)
defer release()
```

**ไม่มีการเช็ค** ว่า `Acquire` ได้ lock จริงหรือ busy — ทั้งสองกรณี `Spawn()` เดินหน้าเรียก `launcher.Spawn()` แล้ว `InsertActiveSession()` เหมือนกันหมด lock ไม่ block อะไรและไม่คืน error ใด ๆ เลย

Commit message ระบุ: *"concurrent Spawn for the same cardID returns 'already running' error instead of blocking"* — behavior นี้ไม่มีอยู่ในโค้ด grep ทั้ง repo หา `"already running"` ไม่เจอ

**ผลกระทบจริง:** สอง goroutine เรียก `Spawn()` พร้อมกันสำหรับ card เดียวกัน ทั้งคู่จะไปถึง `launcher.Spawn()` จริง (เช่น spawn Hermes session สองตัวซ้อนสำหรับ card เดียว) ก่อนที่ DB `PRIMARY KEY` constraint (migration 0036) จะมีโอกาสปฏิเสธ insert ที่สอง — DB constraint เป็น safety net ตัวสุดท้ายที่ทำงานได้จริง แต่ในระดับ app จะเห็น process/session รั่วไปแล้วก่อนโดน reject

**Fix:** ให้ `Acquire` คืนค่า `(release func(), ok bool)` แล้ว `Spawn()` เช็ค `ok` คืน error ทันทีถ้า busy — หรือถ้าจะพึ่ง DB constraint เป็นตัว enforce จริงอยู่แล้ว (migration comment เขียนไว้เองว่า "enforces unique-per-card concurrency without a global mutex") ให้ตัด `cardLock`/`lockFor`/`locks`/`locksMu` ทั้งบล็อกทิ้งไปเลย (~50 บรรทัด) แทนที่จะเก็บโค้ดที่ไม่ enforce อะไรไว้หลอกคนอ่าน

### C-2. Test ที่อ้างว่า cover concurrency ไม่ได้ concurrent จริง

`spawner_test.go:147-182` (`TestSpawn_ConcurrentSameCard`) — เรียก `s.Spawn(ctx, spec1)` แล้ว **await ผลเสร็จ** ก่อนเรียก `s.Spawn(ctx, spec2)` ตามลำดับ ไม่มี goroutine/`sync.WaitGroup` ใด ๆ `t.Parallel()` มีผลแค่ให้ test function นี้รันขนานกับ test function อื่นในไฟล์ ไม่ใช่ขนานกับตัวเอง

Test นี้จริง ๆ ตรวจแค่ว่า `concurrentStore` (fake ที่ทำ mutex+map เอง) ปฏิเสธ insert ซ้ำ — ไม่ได้ตรวจ `Spawner`'s lock (`cardLock`) เลยสักนิด ผลคือ C-1 ไม่มี test ใดจับได้

Plan เอง (Task 5 Verification) ระบุชัดว่าต้องมี "a concurrency test spawns for N cards in parallel and asserts exactly one `active_session` row per card" — สิ่งที่ต้องการคือ**หลาย card ต่างกัน spawn พร้อมกันจริง** (goroutines) เพื่อพิสูจน์ per-card lock ไม่ block ข้าม card และไม่ race กัน ไม่ใช่ sequential same-card test ที่มีอยู่ตอนนี้

**Fix:** เพิ่ม test จริงที่ spawn N cards ต่างกันพร้อมกันด้วย goroutines + `sync.WaitGroup`, assert `len(store.Inserts) == N` ไม่มี error, ไม่มี race (`go test -race`); แยกอีก test หนึ่งที่ยิง goroutine สองตัว spawn **card เดียวกัน** พร้อมกันจริง แล้ว assert ตัวหนึ่งสำเร็จอีกตัว fail ด้วย error ที่ตั้งใจ (ต้องรอ fix C-1 ก่อนถึงจะเขียน assertion นี้ได้อย่างมีความหมาย)

---

## MEDIUM

### M-1. `RegistryLauncher.Spawn` ไม่ได้ spawn session จริง — session ID เป็นของปลอม

`spawner.go:208-226` — เรียก `agent.GetLaunchCommand(...)` ได้ `argv` มาแล้ว **ทิ้งไปเลย** (`_ = argv`) ไม่มีการ start process จริง แล้วคืน:

```go
return SessionHandle{
	ID:      spec.CardID,   // ← เอา CardID มาใช้แทน session ID จริง
	NativeID: "",
}, nil
```

Comment ในโค้ดบอกว่า process start เป็นหน้าที่ session runtime (tmux/pty) ที่ session service เป็นเจ้าของ — เข้าใจได้ว่าจงใจ defer ส่วนนี้ แต่ผลคือ `active_session.session_id` ที่บันทึกจริงตอนนี้จะเท่ากับ `card_id` เสมอ ไม่ใช่ session ID ที่ session service ออกให้จริง เมื่อ Task 17 (orphan/restart recovery) ต้องอ้างอิง session ID นี้ไปเช็ค liveness ของ process จริง จะเช็คไม่ได้เพราะ ID ไม่ตรงกับของจริง

**Fix:** ไม่ต้อง fix ตอนนี้ถ้าเป็น scope ของ task ถัดไปจริง แต่ให้ใส่ TODO/note อ้าง Task 6 หรือ session-service integration ไว้ในโค้ดหรือ plan เพื่อไม่ให้ลืมว่า `SessionHandle.ID` ยังเป็นของปลอมอยู่

---

## MINOR

| # | เรื่อง | ที่ |
| --- | --- | --- |
| m-1 | `Deps` struct ประกาศไว้ (fields: `Store`, `Launcher`, `Clock`, `NewID`) แต่ไม่มีที่ใดสร้าง/ใช้เลย — `New()` รับ positional args แทน dead code | `spawner.go:97-102` |
| m-2 | Double registry lookup: `Spawn()` เช็ค `s.reg.Get(...)` เอง (บรรทัด 155-159) แล้ว `RegistryLauncher.Spawn()` เช็คซ้ำอีกรอบ (บรรทัด 199-202) — ราคาไม่แพงแต่ซ้ำซ้อนไม่จำเป็น | `spawner.go:155-159, 199-202` |

---

## สิ่งที่ตรวจแล้วถูกต้อง

| ข้อ | ผล | หลักฐาน |
| --- | --- | --- |
| Migration ใช้ `card_id PRIMARY KEY` ป้องกัน duplicate ที่ระดับ DB | ✅ | `0036_active_session.sql:4` — ถูกต้องและเป็น safety net ตัวจริงที่ enforce ได้ |
| FK cascade ไปยัง `work_cards` (ชื่อ table ตรงกับ migration จริง ไม่ใช่ singular ตามที่บาง plan เขียน) | ✅ | `0036_active_session.sql:9` ตรงกับ `0030_add_workboard.sql:3` |
| `TestSpawn_UnknownAgent` / `TestSpawn_LancherError` ครอบ error path ที่ตั้งใจถูกต้อง | ✅ | `spawner_test.go:125-209` |
| SQLC generated code ตรงกับ query file, ไม่มี diff ค้าง | ✅ | `active_session.sql.go` มี `InsertActiveSession`/`GetActiveSession`/`ListActiveSessionsByPhase`/`ListActiveSessionsByAgent`/`DeleteActiveSession` ครบตาม `.sql` |
| Briefing ที่ยังไม่ทำ (`tool_inject.go` stub) ถูก defer ไป Task 11 ตามที่ plan ระบุ ไม่ implement เกินขอบเขต | ✅ | commit message ระบุชัด, ตรงกับ plan's sequencing note ที่เพิ่งแก้ |
| ไม่มี wiring เข้า daemon จริง (`spawner.New(` เรียกแค่ใน test) | ⚠️ context เฉย ๆ ไม่ใช่ bug — คาดว่าเป็นของ Task 6 | grep ยืนยันไม่มี call site อื่น |

---

## Bottom line

Task 5 มี 2 ส่วนที่ทำถูก (DB constraint, error-path tests) แต่ **จุดขายหลักของ commit — "per-card locking" — เป็นโค้ดตายที่ไม่ทำอะไรเลย** และ commit message บรรยาย behavior ที่ไม่มีอยู่จริง (C-1) เพราะ test ที่ตั้งใจ cover เคสนี้ไม่ได้ concurrent จริง (C-2) เลยไม่มีอะไรจับได้ DB `PRIMARY KEY` ยังเป็นแนวป้องกันสุดท้ายที่ใช้ได้จริงอยู่ — เพียงพอสำหรับ correctness ระดับข้อมูล แต่ไม่เพียงพอสำหรับ "ไม่ launch process ซ้ำ" ตามเจตนาที่ plan ต้องการ

**ลำดับซ่อมแนะนำ:** C-1 (แก้ lock ให้ enforce จริง หรือลบทิ้งแล้วพึ่ง DB constraint อย่างเดียว) → C-2 (เขียน concurrency test จริงด้วย goroutines) → M-1 (note ไว้ว่า session ID ยังปลอมอยู่ รอ session-service wiring) → minor ตามสะดวก

ยังไม่ควรถือว่า Task 5 "done" ตามที่ commit message อ้าง จนกว่า C-1/C-2 จะแก้
