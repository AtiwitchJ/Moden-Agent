# การใช้งาน Modern Agent ตาม Layout ทีละ Step

> สัญลักษณ์: **[ปัจจุบัน]** คือมีในหน้าจอตอนนี้, **[แบบ]** คือออกแบบไว้แต่ยังไม่ implement

## Step 1 — เริ่มคำสั่งใน Code

**เป้าหมาย:** บอก AI ว่าต้องการทำอะไรกับ project ใด

```text
┌──────────────────────────────────────────────────────────────────────────┐
│                           [ Code ]  Code Manage  Work                    │
├───────────────┬──────────────────────────────────────────────────────────┤
│ Modern Agent  │                                                          │
│               │          Welcome back, {ชื่อองค์กร}                      │
│  A  + New     │                                                          │
│               │          No sessions yet…                                │
│  RECENTS      │                                                          │
│  session…     │                                                          │
│               │                                                          │
│               ├──────────────────────────────────────────────────────────┤
│  More         │ B [Choose a project ▾] C [Describe work…] D [Send]      │
└───────────────┴──────────────────────────────────────────────────────────┘
```

1. กด **A `New`** เพื่อวาง cursor ที่ช่องพิมพ์ C
2. เลือก project ที่จะทำใน **B**; เลือก `New project…` หากยังไม่มี project
3. อธิบายผลลัพธ์ที่ต้องการใน **C** เช่น “ปรับโทนสีตามลิงก์นี้”
4. กด **D `Send`**
5. ระบบสร้าง/เปิด session และพาไปหน้า session เพื่อให้ agent เริ่มทำงาน

**สิ่งที่ไม่ทำในขั้นนี้:** ไม่ต้องสร้าง Kanban card และไม่ต้องเปิด terminal เอง

---

## Step 2 — กลับมาดู session ใน Code

**เป้าหมาย:** กลับไปหางานที่คุยหรือกำลังทำอยู่

```text
┌───────────────┬──────────────────────────────────────────────────────────┐
│ RECENTS       │ Sessions                                                  │
│ ● palette     │ ┌──────────────────────────────────────────────────────┐ │
│   2 min ago   │ │ ● Adjust colour palette                 just now     │ │
│               │ │   billing-portal                                      │ │
│               │ └──────────────────────────────────────────────────────┘ │
│               │                                                          │
│               ├──────────────────────────────────────────────────────────┤
│               │ [Choose project ▾] [Describe work…] [Send]               │
└───────────────┴──────────────────────────────────────────────────────────┘
```

1. คลิกชื่อ session ใน **Recents** ด้านซ้าย หรือ row ในพื้นที่กลาง
2. ระบบเปิด session นั้น
3. ใช้ composer ล่างจอเมื่อต้องเริ่ม session ใหม่เท่านั้น

**[แบบ]** row จะเพิ่มสถานะสั้นของ Hermes เช่น `Plan` หรือ `Worker` แต่ยังไม่แสดง raw log

---

## Step 3 — สร้างและจัดลำดับงานใน Code Manage

**เป้าหมาย:** แปลงงานที่ต้องติดตามเป็น card และบอกระบบว่างานใดต้องเริ่มก่อน

```text
┌──────────────────────────────────────────────────────────────────────────┐
│              Code  [ Code Manage ]  Work                  + Create card   │
├───────────┬───────────┬───────────┬───────────┬───────────┬──────────────┤
│ TODO      │ RUNNING   │ REVIEW    │ TESTING   │ REDO      │ DONE         │
│           │           │           │           │           │              │
│ [card A]  │ [card B]  │           │           │ [card C]  │ [card D]     │
└───────────┴───────────┴───────────┴───────────┴───────────┴──────────────┘
```

1. กด **`Create card`**
2. ระบุ title, details, project, target folder และ coding worker
3. Card ใหม่เข้า **Todo**
4. ลาก card จาก **Todo → Running** เมื่อต้องการเริ่มงาน
5. Hermes รับ brief, วางแผน และสั่ง worker ตามค่าที่อยู่ใน card

ความหมายของแต่ละคอลัมน์:

| Layout | ใช้เมื่อ |
| --- | --- |
| Todo | งานรอเริ่มหรือรอจัดลำดับ |
| Running | Hermes/worker กำลังทำงาน |
| Review | รอตรวจคุณภาพ/โค้ด |
| Testing | รอผลทดสอบ |
| Redo | มีเหตุผลที่ต้องแก้และทำซ้ำ |
| Done | งานเสร็จ |

---

## Step 4 — เปิด card เพื่อคุมงาน

**เป้าหมาย:** เข้าใจงานหนึ่งใบและสั่ง Hermes โดยไม่จมกับ terminal log

```text
┌──────────────────────── Board ───────────────────────┬── Task detail ──┐
│ TODO       RUNNING                                    │ Adjust palette   │
│            ┌─────────────────┐                        │ Running          │
│            │ Adjust palette  │ ← click                │ Hermes owns it   │
│            │ Hermes · Plan ● │                        │ details…         │
│            └─────────────────┘                        │ worker: Claude   │
│                                                        │ [Nudge] […]      │
└────────────────────────────────────────────────────────┴──────────────────┘
```

1. คลิก card ที่ต้องการดู
2. อ่าน status, owner (Hermes/worker), task details และ target path
3. ใช้เมนู `…` เฉพาะเมื่อจำเป็น:
   - `Nudge commander` — ส่งคำสั่งเพิ่มให้ Hermes
   - `Retarget goal` — เปลี่ยนเป้าหมายของงาน
   - `Split card` — แตกงานออกเป็นอีก card
   - `Delete card` — ต้องยืนยันก่อน; **ไม่หยุด session อัตโนมัติ**
4. กด `Show live terminal` เมื่อต้องการ log จริง; ปกติให้ดูสรุปใน card ก่อน

**[แบบ]** panel จะเปิดทับ board แทนการบีบคอลัมน์ Kanban

---

## Step 5 — ตรวจ file และ folder ใน Work

**เป้าหมาย:** ดูว่า agent เปลี่ยนไฟล์ใดและไฟล์นั้นอยู่ตรงไหนใน project

> **[แบบ]** หน้านี้ยังเป็น `Coming soon` ในระบบปัจจุบัน

```text
┌──────────────────────────────────────────────────────────────────────────┐
│                 Code  Code Manage  [ Work ]       project ▾  Changes 4   │
├────────────────┬────────────────────────────┬────────────────────────────┤
│ PROJECTS       │ FILES                      │ INSPECT                    │
│ billing-portal │ src/                       │ src/theme.ts               │
│ website        │ ├ components/              │ diff / preview             │
│                │ └ theme.ts       M         │                            │
│                │ package.json     M         │ Related work               │
│                │                            │ Adjust palette · Running  │
└────────────────┴────────────────────────────┴────────────────────────────┘
```

1. เลือก project/repository จาก rail หรือ project picker
2. เลือก folder/file จาก **Files**
3. ดู preview หรือ git diff ใน **Inspect**
4. กด related work เพื่อกลับไป Code Manage หรือกด `Open terminal` เพื่อเข้า session

รอบแรก Work เป็น **read-only**: ดู tree, path, preview และ diff ได้ แต่ยังสร้าง/ย้าย/ลบไฟล์ไม่ได้

---

## เส้นทางสั้นที่สุด

```text
Code: เลือก project + พิมพ์ + Send
  → Code Manage: สร้าง/เริ่ม/คุม card
    → Work: ตรวจ file tree และ diff
      → Code Manage หรือ session: สั่งแก้/ดู terminal เมื่อจำเป็น
```
