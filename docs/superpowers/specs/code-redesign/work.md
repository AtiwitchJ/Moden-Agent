# Modern Work Redesign — Draft 1

## หน้าที่ของหน้า

Modern Work คือ **workspace desk** สำหรับจัดการ project, repo, file และ folder ที่เกี่ยวกับงานจริง

ผู้ใช้เข้าหน้านี้เมื่ออยากตอบว่า:

1. ไฟล์ใดกำลังเปลี่ยน และการเปลี่ยนนั้นอยู่ใน project/repo ใด
2. โครงสร้าง folder ของงานนี้เป็นอย่างไร
3. งานหรือ session ใดเกี่ยวข้องกับไฟล์ที่กำลังดู

ไม่ใช่ file manager ทั้งเครื่อง และไม่ใช่หน้าสำหรับเริ่มงานหรือย้าย card

## Baseline

- Mode bar มี `Work` อยู่แล้ว แต่ route ปัจจุบันเป็น `Coming soon`
- Project ที่ลงทะเบียนแล้วมี path/repo roots ที่ daemon รู้จัก
- Session detail มี terminal และ inspector อยู่แล้ว จึงไม่ต้องสร้าง terminal ซ้ำใน Work

## Boundary และความปลอดภัย

Work มองเห็นได้เฉพาะ project และ repository roots ที่ผู้ใช้ลงทะเบียนกับ Modern Agent

- ไม่ browse `/`, home directory หรือ folder ภายนอก project
- Daemon เป็นผู้ list/read/write file ผ่าน API; renderer ไม่อ่าน filesystem โดยตรง
- การแก้, ย้าย, rename, สร้าง และลบไฟล์ต้องมี confirmation และแสดง diff ก่อนบันทึก
- symlink ที่ชี้ออกนอก repo root ต้องไม่เปิดอ่าน/เขียนจากหน้านี้

## ความสัมพันธ์กับสามหน้า

| หน้า | คำถามที่ตอบ |
| --- | --- |
| Code | อยากให้ AI เริ่มทำอะไร? |
| Code Manage | งานใบไหนอยู่ขั้นไหน และต้องสั่งอะไรต่อ? |
| **Work** | ไฟล์ไหนอยู่ที่ไหน เปลี่ยนอะไร และเกี่ยวกับงานใด? |

## Layout

```text
┌────────────────────────────────────────────────────────────────────────────────────┐
│                     Code · Code Manage · Work                         notifications │
├────────────────────────────────────────────────────────────────────────────────────┤
│ billing-portal ▾       Search files…                    Changes 4     Open session │
├────────────────┬─────────────────────────────────┬─────────────────────────────────┤
│ PROJECTS       │ FILES                           │ INSPECT                            │
│ • billing      │ billing-portal / src / theme.ts  │ theme.ts                           │
│ • website      │ ├─ src                           │ ───────────────────────────────   │
│ • api          │ │  ├─ components                 │  12   export const palette = …    │
│                │ │  ├─ features                   │  13 + export const accent = …    │
│ REPOSITORIES   │ │  └─ theme.ts       M           │                                    │
│ billing-portal │ ├─ public                        │ Related work                       │
│ └ main         │ ├─ package.json      M           │ ● Adjust colour palette            │
│                │ └─ README.md                     │   Hermes → Claude Code · Running   │
│                │                                 │                                    │
│                │                                 │ [Open task] [Open terminal]       │
└────────────────┴─────────────────────────────────┴─────────────────────────────────┘
```

### 1. Project rail

- เลือก project ปัจจุบัน และสลับ repository root เมื่อ project เป็น multi-repo workspace
- แสดง project ล่าสุด 5 รายการ ไม่ทำ company tree ซ้ำจาก UI เก่า
- ทุกการเปลี่ยน project เปลี่ยน file tree, changes และ related work พร้อมกัน

### 2. File tree

- tree เปิดเฉพาะ root ที่ลงทะเบียน; path breadcrumb อยู่บนสุด
- สถานะ git ขนาดเล็ก: `M`, `A`, `D`, `?` อยู่ท้ายชื่อ file
- filter/search ชื่อ file หรือ path; ไม่ search เนื้อหาในรอบแรก
- folder ว่างและไฟล์ที่ ignore ซ่อนไว้เป็น default พร้อม toggle `Show ignored`

### 3. Inspect pane

เมื่อเลือกไฟล์:

- ไฟล์ text แสดง preview แบบ read-only + diff หากมีการเปลี่ยน
- ไฟล์ภาพ/PDF แสดง preview ที่เหมาะสม
- binary/ไฟล์ใหญ่ แสดง metadata และปุ่มเปิดด้วยระบบภายนอก แทนการพยายาม render
- ข้าง preview แสดง `Related work`: card/session ที่เปลี่ยน path เดียวกัน หรือเลือกด้วยผู้ใช้

## Signature: Change map

แทนที่จะให้ git status เป็นรายการไฟล์ลอย ๆ, Work group การเปลี่ยนแปลงเป็นแผนที่:

```text
Adjust colour palette     Hermes → Claude Code     3 files
└ src/theme.ts · src/tokens.css · public/logo.svg
```

ผู้ใช้เห็นก่อนว่า “ใครเปลี่ยนอะไรเพื่อเป้าหมายใด” แล้วจึงลงไปดู file/diff รายไฟล์

## การจัดการไฟล์และโฟลเดอร์

### Read-first (รอบแรก)

- browse tree, search path, preview file, ดู git diff, และดู related card/session
- ไม่มี create/rename/move/delete เพื่อพิสูจน์ว่า project root, repo และ path guard ถูกต้องก่อน

### Write phase (หลังจาก read-first ผ่าน)

- `New file`, `New folder`, `Rename`, `Move`, `Delete`
- ทุก action เปิด confirmation sheet พร้อม old/new path และผลกระทบ git
- `Delete` บอกชัดว่าเป็น permanent filesystem operation หรือย้ายไป Trash ได้หรือไม่
- ถ้า file ถูกแก้โดย agent อยู่ ต้องแสดง conflict/warning ก่อนเขียนทับ

## Flow หลัก

1. เข้า Work แล้วเลือก project/repo จาก rail หรือ project picker
2. เห็น change map และ file tree ของ root ที่ปลอดภัย
3. เลือกไฟล์เพื่อดู preview/diff ใน Inspect pane
4. เลือก related card เพื่อดูว่า Hermes/worker กำลังทำอะไรกับไฟล์นั้น
5. กด `Open task` เพื่อไป Code Manage หรือ `Open terminal` เพื่อไป Work session detail
6. เมื่อ write phase เปิดใช้ ผู้ใช้ทำ file operation ผ่าน confirmation sheet เท่านั้น

## การใช้งานตาม Layout

```text
Rail:    [Project / repository]
Middle:  [Files / folders]
Right:   [Inspect: preview, diff, related work]
```

1. เลือก project และ repository root จาก rail
2. เปิด folder หรือเลือกไฟล์ในคอลัมน์ **Files**
3. อ่าน preview หรือ diff ใน **Inspect**
4. คลิก `Related work` เพื่อกลับไปที่ card ใน Code Manage
5. กด `Open terminal` เมื่อต้องคุยหรือสั่งงาน session โดยตรง

รอบแรกเป็น read-only; การสร้าง, ย้าย, rename และลบไฟล์จะเปิดใช้หลัง path guard และการยืนยันผ่านการทดสอบเท่านั้น

## Daemon/API ที่ต้องมีภายหลัง

Work ไม่ควรใช้ Electron filesystem API ตรง ๆ จึงต้องออกแบบ daemon contract ก่อน implementation:

- list registered project roots/repositories
- list directory แบบ path-sandboxed
- read preview ที่มี file size/type limit
- git working-tree status และ diff รายไฟล์
- file operation แบบ path-sandboxed พร้อม validation และ error envelope

## ไม่ทำในรอบแรก

- ไม่เป็น IDE เต็มรูปแบบ และไม่มี code editor
- ไม่เปิด terminal หลายบานในหน้า Work
- ไม่ index/search เนื้อหา project ทั้งหมด
- ไม่ write filesystem จนกว่า read-only experience และ guard ผ่านการทดสอบ

## คำถามที่ต้องล็อกก่อน implementation

1. Work ต้องเริ่มที่ project ล่าสุด หรือบังคับให้เลือก project ทุกครั้ง?
2. Change map ผูก file กับ card/session อัตโนมัติด้วย target path หรือให้ผู้ใช้ link เอง?
3. Write phase ควรย้ายไฟล์ไป Trash เป็น default ได้ทุก platform หรือจำกัด read-only ไปก่อน?
