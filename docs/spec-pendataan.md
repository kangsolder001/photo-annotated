# Spec: Pendataan Baseline Rumah Tangga Cipadung Kulon

> Status: Draft v0.1 (30 Okt 2026), hasil diskusi dengan Anda; belum diimplementasikan.
> Induk: [spec.md](spec.md) + [spec-labels-admin.md](spec-labels-admin.md). Spec ini berlaku untuk branch
> `feat/instrumen-baseline-rumah-tangga-cipadung-kulon` dan **instance baru** (§10). Instance lama
> (`cam.<domain>`, branch `main`) tidak berubah. Bagian induk yang diganti dicantumkan di §12.

## 1. Ringkasan

Alur aplikasi diganti sepenuhnya: dari "foto + kotak" menjadi **pendataan satu rumah tangga** yang diisi
oleh **mahasiswa (pewawancara)**, mengikuti instrumen Google Form "Instrumen Baseline Rumah Tangga Cipadung
Kulon Kelompok 4" (Universitas Bhakti Kencana, GRADASI Episode 1). Satu pendataan = satu rumah tangga.

Urutan: identitas + persetujuan → checklist B1–B30 → observasi C1–C10 → data rumah tangga → **foto wadah
dengan kotak berlabel (wajib)** → kirim. Bila responden menolak, hanya bagian A yang disimpan (untuk
menghitung penolakan) dan form selesai.

Yang dipakai ulang dari aplikasi lama: akun mahasiswa (nomor HP + password + OTP), upload foto, kotak
berlabel per foto, draf otomatis, edit 24 jam, review admin, hapus permanen, kelola akun, export.

## 2. Goals & Non-Goals

### Goals
- Mahasiswa mengisi seluruh instrumen dari HP dalam satu alur, dengan draf tersimpan otomatis.
- Nama, NIM, dan kelompok terisi dari akun, tidak diketik ulang tiap pendataan.
- NIM unik: satu NIM hanya bisa punya satu akun.
- Kelompok dipilih dari dropdown; daftarnya dikelola admin (teks bebas).
- Foto wadah wajib untuk rumah tangga yang bersedia; tiap wadah ditandai kotak berlabel Organik /
  Anorganik / Residu / B3.
- Penolakan tetap tercatat (bagian A saja) agar tingkat penolakan bisa dihitung.
- Admin melihat daftar + detail pendataan, statistik, dan mengekspor CSV (satu baris per rumah tangga).
- Retensi bisa dimatikan lewat config (`retention.days = 0`).

### Non-Goals
- Form builder di dashboard admin. Pertanyaan ditulis di kode (§4); perubahan pertanyaan = deploy.
- Pengisian offline + sinkron belakangan (cukup draf otomatis seperti sekarang).
- Warga mengisi sendiri; banyak instrumen/kuesioner lain.
- Pemetaan kelompok ↔ RW (kelompok teks bebas; satu kelompok boleh mendata lebih dari satu RW).
- Mengubah NIM atau kelompok setelah registrasi dari sisi mahasiswa.
- Statistik selain yang ada di §7.3.

## 3. Alur mahasiswa

### 3.1 Registrasi
Form registrasi lama ditambah **NIM** dan **Kelompok** (dropdown dari `GET /groups`). Setelah OTP
terverifikasi, akun aktif seperti biasa.

### 3.2 Pendataan baru
Satu layar per bagian (wizard), tombol Kembali/Lanjut, progres "Bagian 3 dari 8".

1. **A. Identitas pendataan.** Tanggal (default hari ini, tidak boleh di masa depan), RW, RT, kode rumah
   tangga, inisial responden, status responden. Nama mahasiswa, NIM, dan kelompok tampil sebagai teks
   (dari akun). Lalu **persetujuan pendataan** dengan teks:
   > "Data dan foto tempat sampah (tanpa wajah dan nomor rumah) dipakai untuk pendataan KKN dan penelitian
   > dosen. Bapak/Ibu boleh menolak."

   Pilihan *Tidak bersedia* langsung menampilkan tombol **Kirim** (bagian lain dilewati).
2. **B.1–B.5** (lima layar): B1–B30 Ya/Tidak + keterangan opsional per sub-bagian.
3. **C.** C1–C10 (Ada/Sesuai, Tidak, Tidak teramati) + catatan opsional.
4. **R2. Data rumah tangga** (opsional semua).
5. **R3. Foto wadah.** 1–3 foto (foto pertama = foto utama). Tiap foto: gambar kotak + pilih label (alur
   anotasi lama). Lalu pertanyaan R3 untuk pendataan ini (kondisi pemilahan, isi terlihat, jumlah wadah,
   jenis wadah, lokasi, pencahayaan). Protokol foto dari instrumen ditampilkan di atas tombol kamera.
6. **Ringkasan + Kirim.** Bagian yang belum lengkap ditandai dan bisa diklik.

Draf (jawaban + foto + kotak) tersimpan otomatis di perangkat seperti draf lama. Setelah kirim, mahasiswa
bisa mengedit dalam 24 jam (`edit.window`).

### 3.3 Riwayat
Daftar pendataan milik mahasiswa: tanggal, RT/RW, kode rumah tangga, status (Bersedia / Menolak), status
review.

## 4. Instrumen (definisi pertanyaan)

Definisi pertanyaan ada **di satu tempat**: paket Go `internal/domain/survey` (data murni). Backend memakai
definisi itu untuk validasi dan menyajikannya lewat `GET /survey/form`; frontend merender form secara
generik dari respons itu. Setiap definisi punya `version` (mulai `1`); jawaban disimpan bersama versinya.

### 4.1 Tipe
| Tipe | Nilai jawaban | Aturan |
|---|---|---|
| `date` | `"YYYY-MM-DD"` | tanggal valid, tidak di masa depan (zona Asia/Jakarta) |
| `text` | string | trim, 1–`max` karakter, tanpa karakter kontrol |
| `textarea` | string | trim, 0–1000 karakter, boleh baris baru |
| `int` | bilangan bulat | `min`–`max` |
| `choice` | id opsi | harus salah satu opsi |
| `multi` | array id opsi (unik) | ≥ 1 bila wajib; opsi `other` wajib disertai teks 1–100 karakter |

### 4.2 Daftar pertanyaan (versi 1)
`*` = wajib (bila bagiannya berlaku, lihat §4.3).

**A. Identitas** (nama, NIM, kelompok dari akun; tidak ada di jawaban)
| id | Pertanyaan | Tipe |
|---|---|---|
| `a_date` | Tanggal pendataan * | date |
| `a_rw` | RW * | text, maks 10 |
| `a_rt` | RT * | text, maks 10 |
| `a_household_code` | Kode rumah tangga (RT, RW, no rumah) * | text, maks 50 |
| `a_initials` | Inisial responden * (contoh: Ny. S) | text, maks 20 |
| `a_respondent` | Status responden * | choice: `head` Kepala keluarga, `spouse` Istri/suami, `adult` Anggota keluarga dewasa |
| `a_consent` | Persetujuan pendataan * | choice: `yes` Bersedia, `no` Tidak bersedia |

**B. Checklist baseline** — semua `choice` `yes`/`no` (Ya/Tidak), wajib; teks pertanyaan persis seperti
instrumen.
| Sub-bagian | Butir | Keterangan opsional |
|---|---|---|
| 1. Pengetahuan dan kebiasaan pemilahan sampah | `b1`–`b8` | `b_note_1` |
| 2. Kondisi pengelolaan sampah organik | `b9`–`b14` | `b_note_2` |
| 3. Pengelolaan sampah anorganik dan bank sampah | `b15`–`b19` | `b_note_3` |
| 4. Hilirisasi hasil riset ecoenzim | `b20`–`b25` | `b_note_4` |
| 5. Dukungan dan keberlanjutan | `b26`–`b30` | `b_note_5` |

**C. Observasi langsung** — `c1`–`c10` `choice`: `yes` Ada/Sesuai, `no` Tidak, `na` Tidak teramati;
wajib. `c_note` catatan observasi (opsional).

**R2. Data rumah tangga** (opsional)
| id | Pertanyaan | Tipe |
|---|---|---|
| `r2_occupants` | Jumlah penghuni rumah | int 1–50 |
| `r2_head_age` | Usia kepala keluarga | choice: `lt30`, `30_44`, `45_59`, `ge60` |
| `r2_head_edu` | Pendidikan terakhir KK | choice: `sd` Tidak sekolah/SD, `smp`, `sma` SMA/SMK, `pt` Diploma/Sarjana ke atas |

**R3. Foto dan label wadah** (foto + kotak lewat `photos`, §5.3)
| id | Pertanyaan | Tipe |
|---|---|---|
| `r3_sorting` | Label lapangan: kondisi pemilahan di foto * | choice: `sorted` Terpilah, `partial` Sebagian, `mixed` Tercampur, `unknown` Tidak bisa dinilai |
| `r3_visible` | Isi wadah terlihat di foto? * | choice: `clear`, `partial`, `hidden` |
| `r3_containers` | Jumlah wadah yang terlihat di foto | int 0–20 |
| `r3_container_types` | Jenis wadah di foto | multi: `lidded`, `bucket`, `plastic_bag`, `sack`, `cardboard`, `other` (+teks) |
| `r3_location` | Lokasi wadah | choice: `indoor`, `porch`, `other` |
| `r3_lighting` | Pencahayaan saat foto | choice: `bright`, `dim`, `flash` |

Penjelasan tiap pilihan `r3_sorting` dari instrumen ditampilkan sebagai bantuan di bawah pertanyaan.

Perbedaan dari Google Form (disengaja):
- R1 (persetujuan riset) dihapus; isinya digabung ke teks persetujuan bagian A.
- "Nama mahasiswa" dan "Kode kelompok" diambil dari akun.
- "Jenis sampah (Organik/Anorganik)" dihapus; jenis sampah ditandai per wadah lewat label kotak
  (Organik, Anorganik, Residu, B3).

### 4.3 Aturan bersyarat
- `a_consent = no`: hanya bagian A yang diisi. Jawaban B, C, R2, R3 harus kosong dan `photos` kosong.
- `a_consent = yes`: semua yang wajib harus terisi dan `photos` berisi 1–3 foto.
- Jawaban dengan id tak dikenal ditolak (`422 INVALID_ANSWER`).

## 5. API

Semua endpoint pendataan memakai auth user (mode `otp`). Bentuk error mengikuti spec induk.

### 5.1 `GET /survey/form`
Publik. Mengembalikan definisi instrumen (§4) berikut teks bantuan. Di-cache FE per `version`.
```json
{ "version": 1, "consent_text": "…", "sections": [
  { "id": "A", "title": "A. Identitas Pendataan", "help": "…", "questions": [
    { "id": "a_rw", "label": "RW", "type": "text", "required": true, "max": 10 } ] } ] }
```

### 5.2 `GET /groups`
Publik (dipakai form registrasi). `200 { "groups": [ { "id": 3, "name": "Kelompok 4 – CL-05" } ] }`,
hanya yang aktif, urut nama.

### 5.3 `POST /submissions` dan `PUT /submissions/{id}` (berubah)
Body lama ditambah `form_version` dan `answers`:
```json
{
  "client_request_id": "…",
  "form_version": 1,
  "answers": { "a_date": "2026-11-02", "a_rw": "05", "a_consent": "yes", "b1": "yes", "c1": "na" },
  "photos": [ { "photo_id": "…", "annotations": [ { "label_id": 1, "x": 0.1, "y": 0.2, "w": 0.3, "h": 0.3 } ] } ]
}
```
- Urutan pemeriksaan induk tetap; validasi jawaban (§4) masuk ke langkah 1 (bentuk, tanpa I/O).
- Jumlah foto: 0 bila `a_consent = no`, 1–3 bila `yes` (`422 INVALID_PHOTO_COUNT`).
- Anotasi per foto tetap 0–50 dan wajib `label_id` aktif.
- `form_version` yang tidak didukung → `422 UNSUPPORTED_FORM_VERSION`.
- Error jawaban: `422 INVALID_ANSWER` dengan lokasi, mis. `answers.b7: wajib diisi`.
- `GET /submissions/{id}` dan daftar riwayat memuat `form_version` + `answers`.
- Body maksimal naik dari 64 KB ke 128 KB.

### 5.4 Registrasi (berubah)
`POST /auth/register` ditambah:
- `nim`: trim, 5–20 karakter, huruf/angka saja (contoh `251FS01017`), disimpan huruf besar. Salah → `422 INVALID_NIM`.
- `group_id`: id kelompok aktif. Tidak ada/arsip → `422 INVALID_GROUP`.

NIM unik:
- Dicek saat register: sudah dipakai akun **aktif** → `409 NIM_TAKEN` ("NIM sudah terdaftar. Masuk dengan
  nomor HP Anda atau hubungi admin."). Ini membuka keberadaan NIM terdaftar; dapat diterima karena NIM
  bukan kredensial dan login tetap butuh nomor HP + password.
- Ditegakkan ulang saat verifikasi OTP lewat indeks unik (dua pendaftar menunggu dengan NIM sama: yang
  memverifikasi belakangan → `409 NIM_TAKEN`, challenge-nya ditutup).
- NIM dan `group_id` disimpan di challenge sampai OTP diverifikasi, sama seperti nama/email.

`GET /auth/me` dan `user` di respons login memuat `nim` dan `group` (`{id, name}`).

### 5.5 Kelompok (admin)
Pola sama dengan label, tanpa warna dan urutan manual:
- `GET /admin/groups` — semua termasuk arsip, dengan jumlah mahasiswa.
- `POST /admin/groups` `{ "name": "…" }` — 1–100 karakter, unik tanpa beda huruf besar/kecil
  (`409 GROUP_NAME_TAKEN`).
- `PUT /admin/groups/{id}` `{ "name": "…", "archived": false }`.
- `DELETE /admin/groups/{id}` — hanya bila belum dipakai akun mana pun (`409 GROUP_IN_USE`; arsipkan saja).
- Admin bisa memindahkan mahasiswa ke kelompok lain: `PUT /admin/users/{id}/group` `{ "group_id": 4 }`.

Aksi dicatat `logAdminAction` (`group.create`, `group.update`, `group.delete`, `user.set_group`).

Bentuk respons (ditetapkan saat implementasi T58): item admin `{ "id", "name", "archived", "student_count",
"created_at", "updated_at" }`; `GET /admin/groups` → `{ "groups": [ … ] }` (aktif dulu, lalu arsip; urut
nama); `POST` → `201` + item; `PUT` → `200` + item; `DELETE` → `204`. Nama tidak valid → `422
INVALID_GROUP_NAME`; id tidak ada/bukan angka → `404 NOT_FOUND`.

### 5.6 Admin: daftar & detail (berubah)
- `GET /admin/submissions` ditambah filter `consent` (`yes`/`no`), `rw`, `group_id`; tiap item memuat
  `a_date`, `a_rw`, `a_rt`, `a_household_code`, `consent`, nama + NIM + kelompok mahasiswa.
- `GET /admin/submissions/{id}` memuat `answers` lengkap dan `form_version`.

### 5.7 Export CSV
`GET /admin/export.csv?from&to&consent&review_status` — satu baris per pendataan, UTF-8 dengan BOM (agar
rapi di Excel), streaming.

Kolom: `submission_id, created_at, review_status, mahasiswa, nim, kelompok, photo_count, annotation_count`,
lalu satu kolom per id pertanyaan (urutan §4.2). Nilai `choice` ditulis sebagai **teks opsi** (mis.
"Ya", "Tidak teramati"); `multi` digabung dengan `; `. Nomor HP dan email tidak ikut. Sel yang diawali
`= + - @` diberi awalan `'` (cegah formula injection). Export JSONL dataset lama tetap ada.

### 5.8 Statistik pendataan
`GET /admin/stats/survey?from&to&rw` (atas `a_date`):
```json
{
  "totals": { "households": 120, "consented": 108, "declined": 12 },
  "by_rw": [ { "rw": "05", "households": 60, "consented": 55, "declined": 5 } ],
  "by_student": [ { "user_id": "…", "name": "…", "nim": "…", "group": "…", "households": 14, "declined": 1 } ],
  "b": [ { "id": "b1", "label": "…", "yes": 80, "no": 28, "yes_share": 0.741 } ],
  "c": [ { "id": "c1", "label": "…", "yes": 40, "no": 50, "na": 18 } ],
  "r3_sorting": { "sorted": 20, "partial": 40, "mixed": 45, "unknown": 3 }
}
```
- `b`, `c`, `r3_sorting` hanya dari pendataan yang bersedia; persentase dihitung dari jawaban yang ada.
- Satu query agregat atas `answers` (jsonb), tanpa tabel ringkasan. Cukup untuk ribuan baris.
- Statistik per label kotak memakai `GET /admin/stats/labels` yang sudah ada.

## 6. Data Model (migrasi baru)

```sql
-- 0011_groups.sql
CREATE TABLE groups (
    id          integer     GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    archived_at timestamptz NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX groups_name_lower_idx ON groups (lower(name));

-- 0012_user_nim.sql
ALTER TABLE users
    ADD COLUMN nim      text    NULL CHECK (nim ~ '^[A-Z0-9]{5,20}$'),
    ADD COLUMN group_id integer NULL REFERENCES groups (id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX users_nim_idx ON users (nim);
-- challenge registrasi membawa nim + group_id sampai OTP diverifikasi (kolom menyusul pola name/email)

-- 0013_survey.sql
ALTER TABLE submissions
    ADD COLUMN form_version integer NULL,
    ADD COLUMN answers      jsonb   NULL,
    ADD COLUMN consent      boolean GENERATED ALWAYS AS ((answers->>'a_consent') = 'yes') STORED,
    ADD COLUMN survey_date  date    NULL,  -- salinan answers.a_date untuk filter/indeks
    ADD COLUMN rw           text    NULL;  -- salinan answers.a_rw
CREATE INDEX submissions_survey_date_idx ON submissions (survey_date);

-- seed label kotak bila tabel labels masih kosong
INSERT INTO labels (name, color, sort_order) VALUES
  ('Organik', '#2e7d32', 1), ('Anorganik', '#1565c0', 2), ('Residu', '#6d4c41', 3), ('B3', '#c62828', 4);
```
- `users.nim`/`group_id` boleh `NULL` di DB (mode `none` dan akun lama), tetapi wajib untuk registrasi
  baru di mode `otp`.
- Kolom submission boleh `NULL` agar migrasi aman; aplikasi selalu mengisinya untuk pendataan baru.
- `Down` 0013 menolak bila sudah ada `answers` (pola 0007); `Down` 0012 menolak bila ada `nim` terisi.

## 7. Frontend

### 7.1 Mahasiswa
- Halaman utama = tombol besar **Pendataan baru** + riwayat. Layar "foto saja" lama diganti wizard §3.2.
- Renderer generik per tipe (§4.1) di `src/features/survey/`; logika validasi + aturan bersyarat di
  `src/lib/survey.ts` (+ test) mengikuti definisi dari server.
- Pilihan Ya/Tidak dan tiga opsi C ditampilkan sebagai tombol besar (target sentuh ≥ 44 px, 360 px).
- Langkah foto memakai komponen anotasi yang sudah ada, dibatasi 3 foto.
- Draf: struktur draf lama ditambah `answers` + `form_version`; migrasi draf lama tidak perlu (instance baru).
- Judul aplikasi + teks UI diganti (mis. "Pendataan Sampah KKN"). Tanpa emoji.
- `web/e2e/flow.mjs` diperbarui untuk alur baru (bersedia + menolak).

### 7.2 Admin
- Menu baru **Kelompok** (daftar, tambah, ubah nama, arsip, hapus).
- **Pendataan** (pengganti daftar anotasi): kolom tanggal, RW/RT, kode RT, mahasiswa, bersedia/menolak,
  review; filter §5.6. Detail: jawaban per bagian + foto dengan overlay kotak.
- **Statistik**: kartu total, tabel per RW dan per mahasiswa, bar % Ya per butir B, distribusi C dan
  kondisi pemilahan. Filter tanggal + RW.
- Tombol **Export CSV** di samping export JSONL.
- Profil annotator menampilkan NIM + kelompok dan bisa memindah kelompok.

## 8. Retensi

`retention.days = 0` berarti retensi **mati** (tidak ada submission yang dihapus karena umur); validasi
config diubah dari `>= 1` menjadi `>= 0`. Pembersihan foto pending/ditolak, file yatim, dan data auth tetap
berjalan. `config.json` instance baru memakai `0`. Status sistem menyembunyikan angka "akan terhapus
retensi" bila retensi mati.

## 9. Keamanan & privasi

- Jawaban memuat data yang bisa mengenali rumah (RT/RW/kode rumah, inisial). Tidak boleh muncul di log:
  `e2e_loghygiene_test.go` ditambah kasus pendataan (kode rumah tangga, inisial, NIM, isi keterangan).
- Foto tetap privat; protokol "tanpa wajah/nomor rumah" ditampilkan di layar foto.
- CSV export hanya untuk admin; tidak memuat nomor HP/email.

## 10. Deployment (instance baru)

| | Instance lama | Instance baru |
|---|---|---|
| Branch / image | `main`, `photo-annotated:*` | branch ini, `baseline-cipadung:*`, `baseline-cipadung-web:*` |
| Folder di Pi | `/home/myrpi/docker/photo-annotated` | `/home/myrpi/docker/baseline-cipadung` |
| Compose project | `photo-annotated` | `baseline-cipadung` |
| Database | `photo_annotated` | `baseline_cipadung` (role `baseline_cipadung`, Postgres shared yang sama) |
| Port (localhost Pi) | api 8080, web 8081 | api **8090**, web **8091** |
| Foto di SSD | `/mnt/ssd/photo-annotated/photos` | `/mnt/ssd/baseline_cipadung/photos` (+ penanda store) |
| Subdomain | `cam.<domain>` | `kknsampah.<domain>` → `http://localhost:8091` |
| WhatsApp OTP | `wa-notif-service` | dipakai bersama, tanpa diubah |
| Retensi | 90 hari | `0` (mati) |

Instance lama tidak disentuh. Database dan folder foto dibuat baru, kosong. Superadmin: nomor diatur di
`config.json` instance baru.

## 11. Acceptance Criteria

- [ ] Registrasi meminta NIM + kelompok; NIM kembar ditolak `409 NIM_TAKEN`, termasuk balapan dua
      pendaftar.
- [ ] Mahasiswa mengisi pendataan lengkap dari HP 360 px; draf bertahan saat halaman dimuat ulang.
- [ ] *Tidak bersedia* menyimpan bagian A saja tanpa foto; muncul di statistik sebagai penolakan.
- [ ] *Bersedia* tanpa foto, atau dengan jawaban wajib kosong, ditolak server dengan lokasi field.
- [ ] Foto 1–3 dengan kotak berlabel Organik/Anorganik/Residu/B3.
- [ ] Admin mengelola kelompok; kelompok terpakai tidak bisa dihapus.
- [ ] Admin melihat detail jawaban + foto; statistik cocok dengan data uji; CSV terbuka rapi di Excel.
- [ ] `retention.days = 0` tidak menghapus submission lama; nilai lain tetap bekerja seperti sebelumnya.
- [ ] Log tidak memuat kode rumah tangga, inisial, NIM, maupun isi keterangan.
- [ ] Instance baru berjalan di `kknsampah.<domain>`; instance lama tetap normal.

## 12. Bagian spec induk yang berubah

- spec.md §4.1 (alur submission), §5.2–§5.4 (body submission), §5.8 `POST /auth/register`, §7.1, §7.4,
  §8.3 langkah 3 (retensi bisa mati), §11 (deploy instance baru).
- spec-labels-admin.md §5.4 (filter daftar), §5.9 (statistik tambahan), §6.5 (`users`), §7.2 (menu admin).

## 13. Keputusan (diskusi 30 Okt)

1. Kotak per wadah dipakai; labelnya Organik, Anorganik, Residu, B3.
2. Urutan: pertanyaan dulu, foto di akhir.
3. R1 dihapus; foto beranotasi wajib bagi yang bersedia; teks persetujuan A memakai usulan sementara.
4. Retensi configurable; `0` = tanpa retensi.
5. Nama dari akun; NIM (unik) + kelompok diisi saat registrasi; kelompok = daftar teks bebas dari admin.
6. Satu mahasiswa mendata satu RW, bisa beberapa RT; RW/RT diisi per pendataan.
7. Alur aplikasi diganti sepenuhnya; instance terpisah (§10).
8. Penolakan disimpan (bagian A).
9. Jumlah wadah diisi manual.
10. Tanpa mode offline untuk sekarang.
11. Admin: daftar + detail + CSV + statistik §5.8.

## 14. Open questions

Tidak ada. Dijawab 30 Okt:
1. NIM: contoh UBK `251FS01017` (10 karakter). Aturan dibuat longgar untuk variasi panjang: 5–20 huruf/angka,
   disimpan huruf besar.
2. Foto boleh tanpa kotak (0–50 kotak per foto). Foto disimpan **asli** (hasil resize FE, tanpa kotak);
   kotak disimpan terpisah sebagai koordinat di DB dan hanya digambar sebagai overlay di layar/admin.
   Foto asli tetap bisa diunduh, dan export memuat koordinat kotak.
3. Teks persetujuan: memakai teks §3.2 sampai ada teks resmi dari DPL.

## 15. Rencana task

T57–T73, dipecah kecil agar bisa dikerjakan satu agent per task: lihat
[tasks/plan-pendataan.md](tasks/plan-pendataan.md) dan [tasks/todo-pendataan.md](tasks/todo-pendataan.md).
