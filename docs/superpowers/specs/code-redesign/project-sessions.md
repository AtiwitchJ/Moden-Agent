# Project → Many Sessions Plan

## ปัญหาที่ต้องแก้

Project หนึ่งมีงานหลายเรื่องพร้อมกันได้ แต่ UI และบาง control path ยังมอง `card.sessionId` เหมือนเป็น coding worker เพียงตัวเดียว

ข้อเท็จจริงของระบบปัจจุบัน:

- `WorkspaceSummary` ของ frontend มี `sessions[]` อยู่แล้ว จึงรองรับหลาย session ต่อ project ใน read model
- `spawnOrchestrator(projectId, false, prompt)` reuse active orchestrator ของ project; กด Send ซ้ำไม่ได้สร้าง Hermes ใหม่
- Hermes-commanded card ผูกกับ Hermes commander เพื่อให้ commander สั่ง worker ต่อ

ดังนั้น “หลาย sessions” ต้องหมายถึง **หลาย worker/review/test sessions ภายใต้ project เดียว** ไม่ใช่สร้าง Hermes commander ซ้ำหลายตัว

## Topology ที่ล็อก

```text
Project: billing-portal
│
├─ Commander session: Hermes                         0..1 active
│
├─ Work card: Adjust colour palette                  0..N cards
│  ├─ Hermes link                                    1 commander link
│  ├─ Claude Code worker #1                          0..N session history
│  ├─ Codex review                                   0..N session history
│  └─ Codex testing                                  0..N session history
│
└─ Work card: Add billing export
   ├─ Hermes link                                    same project commander
   └─ Claude Code worker #2                          separate session
```

| ความสัมพันธ์ | กติกา |
| --- | --- |
| Project → sessions | 1 : N เสมอ |
| Project → active Hermes commander | 1 : 0..1 |
| Project → cards | 1 : N |
| Card → sessions | 1 : 0..N ตลอดประวัติ |
| Card → active implementation worker | 1 : 0..1 ตาม WIP ปัจจุบัน |
| Session → card | 0..1 สำหรับ legacy/manual session, 1 สำหรับ session ที่ Hermes สร้างจาก card |

## กติกาการใช้งาน

### Code

1. ผู้ใช้เลือก project แล้วพิมพ์ outcome
2. `Send` ส่ง work request ให้ Hermes **ตัวเดิมของ project**
3. Hermes สร้าง/รับ card สำหรับ request นั้น และค่อย spawn worker session ใหม่เมื่อพร้อม
4. Code แสดงหลาย session ของ project เป็นรายการแยกกัน พร้อมชื่อ card/project; ไม่แสดง Hermes ซ้ำเป็นงานของผู้ใช้

ผลลัพธ์: ผู้ใช้เปิด project เดียวและทำ `Palette`, `Export`, `API review` พร้อมกันได้ โดยแต่ละงานมี session ของตัวเอง

### Director

1. Card มี `Commander: Hermes` เพียงจุดเดียว
2. ใต้ card แสดง worker ล่าสุด และใน panel แสดง `Worker sessions` ทั้งหมดของ card
3. `Nudge`, `Retarget` และ `Split` ส่งให้ Hermes; ไม่ kill หรือสลับ Hermes
4. การจบ/redo/เปลี่ยน review เพิ่ม session link ใหม่ โดยไม่เขียนทับประวัติ worker เก่า

### Work

1. เลือก project ก่อน แล้วจึงเลือก file/repo
2. Change map group ไฟล์ตาม **card + worker session**
3. `Open terminal` เปิด worker ที่เลือก; `Open task` เปิด card ที่เกี่ยวข้อง
4. File tree ไม่แยกตาม session เพราะ repo root เป็นของ project; session เป็น filter/context ของ change เท่านั้น

## Durable model ที่ต้องเพิ่ม

เพิ่ม relation แทนการใช้ `work_cards.session_id` เป็นความจริงเพียงจุดเดียว:

```text
work_card_session_links
  id
  card_id
  session_id
  role          commander | worker | reviewer | tester
  started_at
  ended_at      nullable
  is_primary    false by default
```

- `work_cards.session_id` คงไว้ชั่วคราวเป็น compatibility projection: Hermes commander สำหรับ Hermes card, worker สำหรับ legacy card
- API ใหม่ส่ง `sessions[]` บน card detail; UI ใหม่ไม่เดาว่า `sessionId` คือ worker เสมอ
- Backfill card เก่า: link เดิมเป็น `commander` ถ้า session เป็น Hermes orchestrator, มิฉะนั้นเป็น `worker`
- Session ถูกลบ/จบ: ปิด link ด้วย `ended_at`; ไม่ลบประวัติ card

## ลำดับ implementation

1. **Schema + store** — migration `work_card_session_links`, query/read model และ backfill ที่ปลอดภัย
2. **Service boundary** — link commander/worker/reviewer/tester ตอน spawn, complete/kill/redo และอัปเดต control paths ให้ใช้ role
3. **API contract** — card detail ส่ง session links; regenerate OpenAPI และ frontend types
4. **Code UI** — project-scoped session list, project pill และ `New work request` ที่ไม่สร้าง Hermes ซ้ำ
5. **Director UI** — card/panel แสดง commander หนึ่งตัว + worker session history หลายตัว
6. **Work UI** — file change map filter ด้วย card/session จาก relation ใหม่
7. **Migration + regression tests** — project เดียวหลาย card/worker, reuse Hermes, legacy link, nudge/retarget/split, session cleanup

## เกณฑ์สำเร็จ

- Project เดียวมี card running สองใบและ worker sessions ต่างกันได้
- ทั้งสอง card ใช้ Hermes commander เดียวกัน
- เปิด Code แล้วหา session ของงานแต่ละใบได้ โดยรู้ว่าอยู่ project ไหน
- เปิด card แล้วเห็น Hermes และ history ของ workers โดยไม่สับสนว่าใครเป็น owner
- การปิด worker หนึ่งตัวไม่กระทบ Hermes หรือ worker ของ card อื่น

## สิ่งที่ยังไม่ทำ

- ไม่เพิ่ม worker implementation สองตัวพร้อมกันใน card เดียว; รักษา WIP ต่อ card ที่มีอยู่
- ไม่ลบ `sessionId` เดิมในรอบ migration แรก
- ไม่เปิดให้ user สร้าง Hermes ซ้ำเองจากหน้า Code
