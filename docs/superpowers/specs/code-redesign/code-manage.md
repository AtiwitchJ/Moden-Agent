# Code Manage Redesign — Draft 1

## หน้าที่ของหน้า

Code Manage คือหน้าที่ผู้ใช้ **จัดลำดับและควบคุมงานหลายใบ** ไม่ใช่หน้าสำหรับเริ่ม session และไม่ใช่หน้าดู terminal

คำตอบที่ผู้ใช้ต้องได้ภายในไม่กี่วินาทีคือ:

1. งานใดต้องเริ่ม, กำลังทำ, หรือติดปัญหา
2. Hermes กำลังคุมงานใด และ worker คนใดกำลังลงมือ
3. ต้องทำอะไรกับ card ใบนี้ต่อ

## Baseline ที่ต้องรักษา

- Mode bar ด้านบน: `Code`, `Code Manage`, `Work`
- Board หกคอลัมน์: `Todo`, `Running`, `Review`, `Testing`, `Redo`, `Done`
- สร้าง card ด้วยปุ่ม `Create card`
- ลาก card ข้ามคอลัมน์ได้ และ click เพื่อเปิดรายละเอียด
- Card ที่กำลังทำเชื่อมกับ Hermes commander หรือ worker session
- การลบต้องยืนยันก่อนเสมอ และไม่หยุด session ที่ผูกอยู่แบบเงียบ ๆ

## ขอบเขตของหน้า

| หน้า | หน้าที่ |
| --- | --- |
| Code | เริ่มหรือกลับไปทำงานหนึ่ง session |
| **Code Manage** | จัดลำดับ, เริ่ม, ย้าย, และแทรกแซง durable work cards |
| Work | เปิด session, terminal และรายละเอียดการทำงานเชิงลึก |

## ภาษาภาพ

ใช้ dark UI เดิมและ accent สีของสถานะเดิมเพื่อไม่ให้ผู้ใช้ต้องเรียนรู้ความหมายใหม่

| ความหมาย | สี |
| --- | --- |
| Todo / การกระทำหลัก | ฟ้า |
| Running / Hermes กำลังคุม | ส้ม |
| Review | ม่วง |
| Testing / รอผล | อำพัน |
| Redo / ต้องแก้ | แดง |
| Done | เขียว |

ความเสี่ยงด้านดีไซน์ที่เลือกใช้คือให้ **เส้นความคืบหน้าเล็ก ๆ บน card ที่กำลังทำ** เป็นลายเซ็นของหน้า แทน terminal หรือกรอบสีจำนวนมาก:

```text
Brief ✓  Plan ●  Worker ○  Review ○
```

เส้นนี้แสดงเฉพาะ card ที่มี Hermes/worker session; card อื่นยังคงเรียบและสแกนง่าย

## Layout

```text
┌────────────────────────────────────────────────────────────────────────────────────┐
│                     Code · Code Manage · Work                         notifications │
├────────────────────────────────────────────────────────────────────────────────────┤
│ Workboard                                      2 need attention       + Create card │
├───────────┬───────────┬───────────┬───────────┬───────────┬─────────────────────────┤
│ TODO      │ RUNNING   │ REVIEW    │ TESTING   │ REDO      │ DONE                    │
│  2        │  1        │  0        │  1        │  1        │  8                      │
│           │ ┌───────┐ │           │           │ ┌───────┐ │                         │
│ ┌───────┐ │ │Fix    │ │           │ ┌───────┐ │ │Need   │ │ ┌───────┐               │
│ │Add…   │ │ │palette│ │           │ │API test│ │ │input  │ │ │Merged │               │
│ │urgent │ │ │Hermes │ │           │ │Codex   │ │ │       │ │ │       │               │
│ └───────┘ │ │Plan ● │ │           │ └───────┘ │ └───────┘ │ └───────┘               │
│           │ └───────┘ │           │           │           │                         │
└───────────┴───────────┴───────────┴───────────┴───────────┴─────────────────────────┘

Click card → right-side task detail overlay (ไม่บีบคอลัมน์ board)
```

### Focus panel

เปิดเป็น overlay กว้างพออ่านได้ทับด้านขวา; board ด้านหลังยังคงความกว้างเดิม ไม่ถูกบีบจนชื่อ card หาย

ลำดับข้อมูลใน panel:

1. ชื่อ card + status
2. `Work owner`: Hermes หรือ linked session และคำอธิบายหน้าที่หนึ่งประโยค
3. `Task details`: goal/notes ที่อ่านได้ครบ ไม่ตัดเป็น raw JSON
4. `Coding worker`, project path, labels, goal version
5. Action ตามสถานะ
6. ปุ่ม `Show live terminal` แบบ opt-in เท่านั้น

ห้ามเปิด terminal, transcript, tool call หรือ raw log อัตโนมัติใน panel

## การ์ดและการกระทำ

### Card anatomy

- จุด priority, ชื่อ card และ project
- owner สั้น ๆ: `Hermes → Claude Code` หรือ `Codex`
- หนึ่งบรรทัดจาก notes เพื่อระลึก goal
- progress line เฉพาะ card ที่กำลังทำ
- label/path เป็นข้อมูลรองท้าย card

### การย้ายสถานะ

- `Todo → Running` คือคำสั่งให้เริ่มงาน: Hermes รับ brief แล้วจัด worker
- ถ้า Hermes ไม่พร้อม card กลับไป `Todo` พร้อมข้อความที่บอกเหตุผล
- `Running → Review → Testing → Done` เป็นการส่งต่องานที่ผู้ใช้เห็นได้ชัด
- `Redo` แปลว่าต้องแก้; ต้องแสดงสรุปเหตุผลบน card ไม่ใช่แค่สีแดง

### Action ใน panel

| สถานะ | การกระทำ |
| --- | --- |
| ทุกสถานะ | เปิดรายละเอียด, ลบแบบยืนยัน |
| Running | Nudge commander, Retarget goal, Split card, เปิด terminal |
| Scheduled | เปลี่ยนเวลานัดหมาย |
| Redo | อ่านเหตุผลที่ต้องแก้และส่งกลับ Running |

## Flow หลัก

1. สร้าง card จาก `Create card` หรือรับ card ที่มาจากหน้า Code
2. Card อยู่ `Todo`; ลากไป `Running` เพื่อให้ Hermes เริ่มคุมงาน
3. Hermes อ่าน brief, แสดง `Plan`, และเลือก worker ตาม card
4. ผู้ใช้เปิด panel เมื่อต้องดู goal/owner หรือกด `Nudge commander`
5. งานผ่าน Review และ Testing ก่อนเข้า `Done`; ถ้าติดปัญหาเข้า `Redo` พร้อมเหตุผล
6. ผู้ใช้เปิด Work เฉพาะเมื่อจำเป็นต้องคุม session หรืออ่าน terminal

## การใช้งานตาม Layout

```text
Header: Workboard                                      [+ Create card]
Board:  [Todo] → [Running] → [Review] → [Testing] → [Redo] → [Done]
```

1. กด **`Create card`** แล้วระบุ goal, project, target folder และ coding worker
2. Card ใหม่อยู่ใน **Todo** เพื่อรอจัดลำดับ
3. ลาก card ไป **Running** เมื่อต้องการให้ Hermes เริ่มคุมงาน
4. คลิก card เพื่ออ่าน owner, details, path และสถานะสรุป
5. ใช้ `Nudge commander`, `Retarget goal` หรือ `Split card` เมื่อจำเป็น
6. กด `Show live terminal` เฉพาะเมื่อต้องดู log จริง; กด `Delete card` แล้วต้องยืนยันอีกครั้ง

หน้า Code Manage ใช้ควบคุมวงจรของ card; เมื่ออยากดู file/folder ที่เปลี่ยนให้ไป Work

## ไม่ทำในรอบนี้

- ไม่สร้าง child-card schema ใหม่
- ไม่แสดง live terminal หลายบานในหน้า Code Manage
- ไม่ทำให้การลบ card ฆ่า session โดยอัตโนมัติ
- ไม่ซ้ำหน้า Code ด้วย composer หรือ recents sidebar

## คำถามที่ต้องล็อกก่อน implementation

1. Focus panel ควรเป็น overlay ทับ board หรือมีปุ่มสลับเป็น split view ถาวร?
2. Card ที่ `Done` ควรอยู่บน board กี่วันก่อนซ่อนเป็น archive?
3. ให้ผู้ใช้ลาก `Running → Done` ได้โดยตรงหรือบังคับผ่าน Review/Testing?
