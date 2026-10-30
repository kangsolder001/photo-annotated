# Implementation Plan: Pendataan Baseline Rumah Tangga Cipadung Kulon

Sumber: [spec-pendataan.md](../spec-pendataan.md) (Draft v0.1, 30 Okt 2026). Daftar centang ringkas ada di
[todo-pendataan.md](todo-pendataan.md). Plan aplikasi lama (T1–T56) tetap di [plan.md](plan.md) dan tidak
diubah.

Plan ini ditulis agar **satu task bisa dikerjakan satu agent dalam satu sesi** tanpa harus memahami seluruh
repo: tiap task menyebut file yang dibaca dulu, pola yang ditiru, langkah, dan perintah verifikasi.

## 0. Panduan untuk agent (baca sebelum task apa pun)

1. Baca [CLAUDE.md](../../CLAUDE.md), lalu bagian spec yang disebut di task. **Jangan** membaca spec induk
   utuh (95 KB); cukup bagian yang dirujuk.
2. Kerjakan **satu task saja**. Jangan mengubah file di luar daftar "File" task kecuali untuk wiring kecil
   (router, `cmd/api/main.go`, `endpoints.ts`) yang disebut di langkah.
3. **Tiru pola yang disebut.** Repo ini sudah punya contoh untuk hampir semua hal (label = pola CRUD admin,
   `e2e_labels_test.go` = pola test e2e). Jangan membuat abstraksi baru.
4. Bahasa: komentar, pesan error untuk manusia, dan teks UI dalam **bahasa Indonesia**; identifier dan kode
   error API bahasa Inggris (`NIM_TAKEN`).
5. Kontrak API (path, bentuk JSON, status, kode error) **persis spec**. Bila harus menyimpang, perbarui
   `docs/spec-pendataan.md` dan catat alasannya di bagian "Catatan" task di file ini.
6. Jangan melemahkan test (skip, hapus assertion) agar hijau.
7. Selesai = semua perintah verifikasi hijau → centang task di `todo-pendataan.md`, tulis "Hasil" singkat di
   task ini → **satu commit** `feat(<area>): T<n> <ringkas>` (pesan commit bahasa Inggris boleh). Jangan push.
8. Bila buntu > 2 percobaan pada hal yang sama, berhenti dan tulis temuannya; jangan menebak kontrak.

### Perintah standar

Backend (dari root repo). DB test ada di Pi lewat tunnel:
```bash
scripts/db-tunnel.sh            # sekali, di terminal lain (port 15432)
set -a; . ./.env.local; set +a  # memuat TEST_DATABASE_URL
go vet ./... && go build ./cmd/... && go test ./... -race
```
**Jangan** menjalankan API dengan `DATABASE_URL` dari `.env.local` (itu DB produksi).

Frontend (dari `web/`):
```bash
npm run typecheck && npm test && npm run build
```

### Peta kode yang sering dipakai

| Kebutuhan | Lihat |
|---|---|
| Error domain (`Kind` + `Code`) | `internal/domain/errors.go` (`domain.Invalid(...)`, dst.) |
| Port usecase | `internal/usecase/ports.go` |
| CRUD admin lengkap (pola) | label: `internal/domain/label.go`, `internal/usecase/labels.go`, `internal/adapter/postgres/label_repo.go`, `internal/delivery/http/handler/labels.go`, `e2e_labels_test.go` |
| Wiring route | `internal/delivery/http/router.go`, `cmd/api/main.go` |
| Log aksi admin | cari `logAdminAction` di `internal/delivery/http/handler/` |
| Registrasi + OTP | `internal/usecase/otp_request.go` (`Register`), `internal/usecase/otp_verify.go`, `internal/adapter/postgres/otp_repo.go`, `user_repo.go`, `e2e_auth_test.go` |
| Submit / edit | `internal/usecase/submit.go`, `internal/usecase/edit.go`, `internal/adapter/postgres/submission_repo.go`, `handler/submission.go`, `handler/edit.go` |
| Baca / admin daftar | `internal/usecase/read.go`, `internal/usecase/admin_submissions.go`, `handler/read.go`, `handler/admin_submissions.go` |
| Export / statistik | `internal/usecase/export.go`, `handler/export.go`, `internal/usecase/label_stats.go`, `internal/adapter/postgres/stats_repo.go`, `handler/admin_stats.go` |
| Cleanup / retensi | `internal/usecase/cleanup.go` (baris ±133), `internal/usecase/system.go` (±163), `internal/config/config.go` (±495) |
| Batas body JSON | `internal/delivery/http/middleware/bodylimit.go` (`JSONBodyLimit`) |
| Test e2e HTTP | `internal/delivery/http/e2e_test.go` (setup + helper), `e2e_loghygiene_test.go` |
| FE API | `web/src/api/endpoints.ts`, `web/src/api/types.ts`; admin: `web/src/features/admin/api.ts`, `types.ts` |
| FE registrasi | `web/src/features/auth/LoginPage.tsx` |
| FE alur foto lama | `web/src/features/submit/NewSubmissionPage.tsx`, `PhotoPicker.tsx`, `usePhotoUploads.ts`, `web/src/features/annotate/*`, `web/src/lib/draft.ts` |
| FE admin (pola halaman) | `LabelsPage.tsx`, `SubmissionsPage.tsx`, `SubmissionDetailPage.tsx`, `StatsPage.tsx`, `AnnotatorPage.tsx`, `ui/AnnotatedPhoto.tsx` |

## Architecture Decisions

- **Pendataan = submission lama yang diperluas** (`answers jsonb` + `form_version`), bukan entitas baru.
  Upload, kotak berlabel, idempotensi, edit 24 jam, review, hapus permanen, dan export langsung terpakai.
- **Definisi instrumen satu sumber** di `internal/domain/survey` (data murni Go). Backend memvalidasi
  dengan definisi itu dan menyajikannya lewat `GET /survey/form`; FE merender generik. Tanpa form builder.
- **Validasi jawaban di domain** (tanpa I/O), masuk langkah 1 "bentuk" pada `POST/PUT /submissions`.
- **Kolom salinan untuk filter** (`survey_date`, `rw`, `consent` generated) agar daftar/statistik tidak
  parsing jsonb di `WHERE`.
- **NIM unik ditegakkan DB** (indeks unik); cek awal saat register hanya untuk pesan yang ramah.
- **Kelompok = tabel `groups` teks bebas**, pola sama dengan `labels`.
- **Backend selesai dulu** (Checkpoint A) agar FE punya kontrak stabil.

## Dependency Graph

```
T57 retensi ───────────────────────────────────────────────────────────────┐
T58 groups ──► T59 registrasi NIM ──► T65 FE registrasi ─────────────────────┤
          └──────────────────────────► T70 FE admin kelompok ────────────────┤
T60 survey domain ──► T61 submit/edit ──► T62 baca + admin daftar ──► T71 ──┤
                                     ├──► T63 export CSV ───────────► T71 ──┤
                                     └──► T64 statistik ────────────► T72 ──┤
T60 ──► T66 FE lib survey ──► T67 FE wizard ──► T68 FE foto+kirim ──► T69 E2E ┤
                                                               semua ──► T73 deploy
```
Task yang bisa paralel (tidak menyentuh file yang sama): T57, T58, T60.

## Task List

### Phase 1: Backend

#### Task 57: Retensi bisa dimatikan (`retention.days = 0`) — S
**Spec:** §8. **Baca dulu:** `internal/config/config.go` (validasi retention ±495),
`internal/usecase/cleanup.go` (±125–150), `internal/usecase/system.go` (±150–175).
**Langkah:**
1. Config: ubah validasi `days >= 1` jadi `days >= 0`; pesan error ikut diubah.
2. `cleanup.go`: bila `RetentionDays == 0`, lewati blok retensi (`ExpireSubmissions` dan
   `DeleteEmptyExpiredSubmissions`). Langkah lain tetap jalan.
3. `system.go`: bila `0`, jangan hitung "akan terhapus retensi"; tambahkan `retention_enabled` (bool) ke
   respons `GET /admin/system` (cari struct respons di `handler/admin.go` atau `usecase/system.go`).
4. `config.json.example`: tambahkan komentar/nilai yang menjelaskan `0` = mati (test
   `TestExampleConfigIsValid` harus tetap hijau).
**Acceptance:**
- [ ] `days: 0` lolos; `days: -1` ditolak
- [ ] Dengan `0`, submission > 90 hari tidak terhapus; foto pending basi tetap terhapus
- [ ] Dengan > 0 perilaku lama sama (test lama hijau)
**Test baru:** `internal/config/config_test.go` (0 lolos, -1 gagal); test cleanup usecase dengan
`RetentionDays: 0` (lihat test cleanup yang ada: `grep -rn RetentionDays internal --include=*_test.go`).
**Verifikasi:** perintah standar backend.

#### Task 58: Kelompok (`groups`) — M
**Spec:** §5.2, §5.5, §6 (bagian `groups`). **Tiru:** seluruh pola label (lihat peta kode).
**Langkah:**
1. `migrations/0011_groups.sql`: tabel `groups` persis §6 (goose Up/Down; Down `DROP TABLE groups`).
   Kolom `users.group_id` **belum** di task ini.
2. `internal/domain/group.go`: struct `Group`, validasi nama (trim, 1–100, tanpa karakter kontrol — tiru
   validasi nama label).
3. Usecase `internal/usecase/groups.go` + port di `ports.go`: List aktif, List admin (dengan jumlah
   mahasiswa = 0 dulu; diisi di T59), Create, Update (nama + archived), Delete.
4. Adapter `internal/adapter/postgres/group_repo.go`. Nama kembar → `409 GROUP_NAME_TAKEN` (tangkap unique
   violation seperti di `label_repo.go`).
5. Handler `handler/groups.go`; route `GET /groups` (publik, tanpa auth) dan `/admin/groups` (auth admin);
   `logAdminAction` `group.create|update|delete`.
**Acceptance:**
- [ ] Nama kembar (beda huruf besar/kecil) → 409 `GROUP_NAME_TAKEN`
- [ ] Arsip → hilang dari `GET /groups`, tetap di `GET /admin/groups`
- [ ] `DELETE` kelompok yang belum dipakai → 204; id tidak ada → 404
**Test baru:** `internal/delivery/http/e2e_groups_test.go` (tiru `e2e_labels_test.go`).
**Verifikasi:** perintah standar backend.

#### Task 59: Registrasi dengan NIM + kelompok — M ⚠️
**Spec:** §5.4, §6 (kolom `users`). **Baca dulu:** `otp_request.go` (`Register`), `otp_verify.go`,
`otp_repo.go`, `user_repo.go`, `migrations/0004_auth.sql`, `migrations/0005_password.sql` (cara nama/email/
hash disimpan di challenge), `e2e_auth_test.go`.
**Langkah:**
1. `migrations/0012_user_nim.sql`: `users.nim` + `users.group_id` (§6) + indeks unik `users_nim_idx`;
   kolom `nim`, `group_id` di `otp_challenges` (ikuti pola kolom registrasi yang sudah ada di challenge).
   Down menolak bila ada `nim` terisi (tiru blok `DO $$ … RAISE EXCEPTION` di `0007_labels.sql`).
2. Domain: `NormalizeNIM(s) (string, error)` — trim, huruf besar, `^[A-Z0-9]{5,20}$`, contoh valid
   `251FS01017`; error `422 INVALID_NIM`. Test tabel.
3. `Register`: terima `nim` + `group_id` (wajib di mode `otp`). Cek kelompok aktif (`422 INVALID_GROUP`),
   cek NIM sudah dipakai user **aktif** (`409 NIM_TAKEN`). Simpan di challenge.
4. Verifikasi OTP: saat menerapkan data registrasi ke `users`, tangkap unique violation `users_nim_idx` →
   `409 NIM_TAKEN` dan tandai challenge selesai (tidak bisa dipakai ulang).
5. `/auth/me` + `user` di respons login/verify: tambah `nim` dan `group: {id, name}` (null bila kosong).
6. `GET /admin/groups`: isi jumlah mahasiswa; `DELETE /admin/groups/{id}` terpakai → `409 GROUP_IN_USE`.
7. `PUT /admin/users/{id}/group` (`logAdminAction` `user.set_group`); `GET /admin/users[/{id}]` memuat NIM
   + kelompok.
8. `e2e_loghygiene_test.go`: tambah NIM ke daftar string yang tidak boleh muncul di log.
**Acceptance:**
- [ ] Register tanpa `nim` → 422 `INVALID_NIM`; NIM huruf kecil disimpan huruf besar
- [ ] NIM akun aktif → 409 `NIM_TAKEN`
- [ ] Dua pendaftar menunggu NIM sama: yang verifikasi kedua → 409 `NIM_TAKEN`
- [ ] Kelompok arsip → 422 `INVALID_GROUP`; kelompok terpakai tidak bisa dihapus
- [ ] Test auth lama tetap hijau (perbarui body register di test lama bila perlu, bukan hapus assertion)
**Test baru:** kasus di `e2e_auth_test.go` (atau `e2e_nim_test.go`), test domain NIM.
**Verifikasi:** perintah standar backend.

#### Task 60: Definisi instrumen + `GET /survey/form` — M
**Spec:** §4 (seluruhnya), §5.1. Teks pertanyaan B/C **persis** tabel di bawah ini (dari Google Form).
**Langkah:**
1. `internal/domain/survey/form.go`: tipe `Form{Version, ConsentText, Sections}`, `Section{ID, Title,
   Help, Questions}`, `Question{ID, Label, Help, Type, Required, Max, Min, Options}`, `Option{ID, Label,
   Other bool}`. Tipe: `date`, `text`, `textarea`, `int`, `choice`, `multi` (§4.1).
2. `internal/domain/survey/v1.go`: definisi versi 1 (§4.2), urutan bagian: `A`, `B1`…`B5`, `C`, `R2`,
   `R3`. Gunakan teks di lampiran "Teks instrumen" di bawah.
3. `internal/domain/survey/validate.go`: `Validate(version int, answers map[string]json.RawMessage, today
   time.Time) error` → `domain.Invalid("INVALID_ANSWER", "answers.<id>: <alasan>")` untuk error pertama
   (urut definisi); versi lain → `UNSUPPORTED_FORM_VERSION`. Aturan bersyarat §4.3. Fungsi bantu
   `Consented(answers) bool`.
4. Handler `GET /survey/form` (publik), JSON persis §5.1 (field `snake_case`).
**Acceptance:**
- [ ] Test: id unik; tiap `choice`/`multi` punya ≥ 2 opsi; semua id §4.2 ada
- [ ] Test tabel `Validate`: menolak (`a_consent=no`) hanya butuh A; jawaban B saat menolak → error;
      bersedia dengan `b7` kosong → `answers.b7`; opsi tak dikenal; id tak dikenal; tanggal besok; `multi`
      kembar; `other` tanpa teks; `int` di luar batas; teks > max
**Verifikasi:** `go test ./internal/domain/survey/... -race` lalu perintah standar.

**Lampiran — teks instrumen (versi 1).** Opsi B: `yes` "Ya", `no` "Tidak". Opsi C: `yes` "Ada/Sesuai",
`no` "Tidak", `na` "Tidak teramati".

- A help: "Jangan menuliskan nama lengkap penghuni. Gunakan inisial."
- `a_household_code` label "Kode rumah tangga (RT, RW, no rumah)"; `a_initials` help "Contoh: Ny. S".
- B1 judul "1. Pengetahuan dan kebiasaan pemilahan sampah", help "Tanyakan secara netral. Untuk pertanyaan
  pengetahuan, minta contoh singkat dari responden."
  - b1 Apakah keluarga mengetahui pentingnya memilah sampah dari rumah?
  - b2 Apakah keluarga mengetahui perbedaan sampah organik dan anorganik?
  - b3 Apakah keluarga mengetahui kategori sampah B3 dan residu?
  - b4 Apakah keluarga telah menyediakan wadah sampah terpisah?
  - b5 Apakah keluarga sudah memilah sampah organik dan anorganik?
  - b6 Apakah keluarga memisahkan sampah B3 rumah tangga?
  - b7 Apakah keluarga memisahkan sampah residu?
  - b8 Apakah pemilahan dilakukan secara rutin setiap hari?
- B2 "2. Kondisi pengelolaan sampah organik"
  - b9 Apakah keluarga menghasilkan sampah organik dari aktivitas sehari-hari?
  - b10 Apakah sampah organik dipisahkan dari sampah lainnya?
  - b11 Apakah keluarga mengetahui cara membuat kompos sederhana?
  - b12 Apakah keluarga memiliki atau mengakses fasilitas komposter?
  - b13 Apakah keluarga sudah mengolah sampah organik menjadi kompos atau produk lain?
  - b14 Apakah keluarga bersedia mengikuti pelatihan pengolahan sampah organik?
- B3 "3. Pengelolaan sampah anorganik dan bank sampah"
  - b15 Apakah keluarga memisahkan botol plastik, kardus atau bahan daur ulang lainnya?
  - b16 Apakah keluarga mengetahui adanya bank sampah di wilayahnya?
  - b17 Apakah keluarga menjadi anggota atau memanfaatkan bank sampah?
  - b18 Apakah keluarga pernah menyetorkan sampah anorganik ke bank sampah?
  - b19 Apakah keluarga bersedia berpartisipasi dalam kegiatan bank sampah?
- B4 "4. Hilirisasi hasil riset ecoenzim"
  - b20 Apakah keluarga pernah mendengar tentang ecoenzim?
  - b21 Apakah keluarga mengetahui bahan organik yang dapat digunakan untuk membuat ecoenzim?
  - b22 Apakah keluarga pernah membuat ecoenzim?
  - b23 Apakah keluarga memiliki akses terhadap bahan dan wadah untuk pembuatan ecoenzim?
  - b24 Apakah keluarga bersedia mengikuti demonstrasi pembuatan ecoenzim?
  - b25 Apakah keluarga bersedia melakukan pemantauan proses ecoenzim bersama kader/mahasiswa?
- B5 "5. Dukungan dan keberlanjutan", help "Pertanyaan kesediaan mengukur niat berpartisipasi, bukan bukti
  perubahan perilaku."
  - b26 Apakah keluarga pernah memperoleh edukasi pengelolaan sampah?
  - b27 Apakah terdapat kader atau penggerak lingkungan yang dikenal keluarga?
  - b28 Apakah keluarga bersedia mengikuti pendampingan pemilahan sampah?
  - b29 Apakah keluarga bersedia menerima kunjungan evaluasi berikutnya?
  - b30 Apakah keluarga bersedia mempertahankan pemilahan sampah setelah KKN selesai?
- Keterangan `b_note_1`…`b_note_5`: label "Keterangan butir B1–B8" (B9–B14, B15–B19, B20–B25, B26–B30),
  help "Opsional. Tulis nomor butirnya, contoh: B3 – belum bisa membedakan baterai dan popok."
- C "C. Checklist Verifikasi Observasi Langsung", help "Wajib diisi berdasarkan pengamatan mahasiswa,
  bukan hanya jawaban responden. Jika tidak dapat diperiksa, pilih \"Tidak teramati\". \"Tidak teramati\"
  berbeda dengan \"Tidak\". Jangan membuka tempat sampah sendiri dan jangan menyentuh sampah B3."
  - c1 Terdapat wadah sampah organik
  - c2 Terdapat wadah sampah anorganik
  - c3 Sampah organik dan anorganik benar-benar terpisah
  - c4 Sampah B3 rumah tangga disimpan terpisah dengan aman
  - c5 Sampah residu dipisahkan dari sampah yang dapat diolah
  - c6 Terdapat sarana pengomposan yang dapat digunakan
  - c7 Ada bukti pengolahan sampah organik oleh rumah tangga
  - c8 Ada tempat penyimpanan sampah anorganik untuk disalurkan
  - c9 Ada bukti kegiatan pemanfaatan atau penyetoran ke bank sampah
  - c10 Ada kegiatan ecoenzim yang benar-benar dilakukan keluarga
  - `c_note` "Catatan observasi" (opsional)
- R2 "R2. Data rumah tangga": opsi `r2_head_age` "< 30 tahun", "30–44 tahun", "45–59 tahun", "≥ 60 tahun";
  `r2_head_edu` "Tidak sekolah/SD", "SMP", "SMA/SMK", "Diploma/Sarjana ke atas".
- R3 "R3. Foto dan label wadah", help "Protokol foto: minta warga membuka tutup wadah (jangan dibuka
  sendiri). Foto dari atas, jarak ±50 cm, semua wadah masuk satu bingkai, cahaya cukup. Tanpa wajah,
  tangan, nomor rumah, atau dokumen. 1 foto utama, boleh tambah maks. 2 foto dekat."
  - `r3_sorting` "Label lapangan: kondisi pemilahan di foto", help:
    "Terpilah: ada ≥2 wadah terpisah dan isi tiap wadah sesuai jenisnya (paling banyak 1–2 benda kecil
    salah tempat). Sebagian: ada wadah terpisah tetapi isinya masih banyak tercampur, ATAU hanya sebagian
    jenis yang dipisah (misal plastik saja dipisah, sisanya campur). Tercampur: semua sampah dalam satu
    wadah/kantong, atau wadah terpisah tetapi isinya sama-sama campur. Tidak bisa dinilai: isi tidak
    terlihat (kantong terikat, tertutup), foto buram, atau tidak ada foto. Ini label lapangan (label awal).
    Label final ditetapkan kemudian oleh 2 pelabel dan DPL."
  - `r3_visible` "Isi wadah terlihat di foto?": "Ya, terlihat jelas", "Sebagian terlihat", "Tidak terlihat
    (tertutup/kantong terikat)"
  - `r3_containers` "Jumlah wadah yang terlihat di foto"
  - `r3_container_types` "Jenis wadah di foto": "Tempat sampah bertutup", "Ember", "Kantong plastik",
    "Karung", "Kardus", "Lainnya" (other)
  - `r3_location` "Lokasi wadah": "Dalam rumah/dapur", "Teras/depan rumah", "Lainnya"
  - `r3_lighting` "Pencahayaan saat foto": "Terang (siang/luar ruangan)", "Redup/dalam ruangan", "Pakai
    flash"

#### Task 61: Submit/edit dengan `answers` — M ⚠️
**Spec:** §5.3, §6 (`0013_survey.sql`). **Baca dulu:** `submit.go`, `edit.go`, `submission_repo.go`,
`handler/submission.go`, `handler/edit.go`, `e2e_submit_test.go`, `e2e_edit_test.go`.
**Langkah:**
1. `migrations/0013_survey.sql`: kolom submission §6 + seed label (hanya bila `labels` kosong:
   `INSERT … SELECT … WHERE NOT EXISTS (SELECT 1 FROM labels)`). Down menolak bila ada `answers`.
2. Request `POST /submissions` dan `PUT /submissions/{id}`: tambah `form_version` (int) dan `answers`
   (objek). Panggil `survey.Validate` di langkah 1 (bentuk) sebelum cek foto.
3. Jumlah foto: 0 bila menolak, 1–3 bila bersedia (`INVALID_PHOTO_COUNT`). Ganti default
   `limits.max_photos_per_submission` di config menjadi 3 dan cek 0 hanya diizinkan saat menolak.
4. Simpan `answers`, `form_version`, `survey_date` (dari `a_date`), `rw` (dari `a_rw`) di insert dan update.
5. `JSONBodyLimit` 64 KB → 128 KB (perbarui test batas body bila ada).
6. `e2e_loghygiene_test.go`: kirim pendataan dengan kode rumah tangga, inisial, keterangan unik → string itu
   tidak boleh ada di log.
**Acceptance:**
- [ ] Menolak + 0 foto → 201; menolak + 1 foto → 422; bersedia + 0/4 foto → 422 `INVALID_PHOTO_COUNT`
- [ ] Jawaban salah → 422 `INVALID_ANSWER` dengan `answers.<id>`; `form_version: 2` → 422
      `UNSUPPORTED_FORM_VERSION`
- [ ] Replay idempoten, `VERSION_CONFLICT`, jendela edit, reset review tetap berlaku
- [ ] Edit mengubah `survey_date`/`rw`; `consent` ikut (kolom generated)
- [ ] Test submit/edit lama diperbarui untuk mengirim `answers` valid (helper bersama di `e2e_test.go`)
**Test baru:** `e2e_survey_test.go`.
**Verifikasi:** perintah standar backend.

#### Task 62: Baca + admin daftar/detail — S
**Spec:** §5.3 (GET), §5.6. **Baca dulu:** `read.go`, `admin_submissions.go`, handler terkait,
`e2e_read_test.go`, `e2e_admin_submissions_test.go`.
**Langkah:** `GET /submissions/{id}` dan riwayat user memuat `form_version`, `answers`, `consent`.
`GET /admin/submissions`: filter `consent` (`yes`/`no`), `rw`, `group_id`; item memuat `survey_date`, `rw`,
`rt`, `household_code`, `consent`, `user: {name, nim, group}`. Detail admin memuat `answers` lengkap.
**Acceptance:** [ ] filter tunggal dan gabungan; [ ] filter lama tetap jalan; [ ] cursor tetap stabil.
**Verifikasi:** perintah standar backend.

#### Task 63: Export CSV — S
**Spec:** §5.7. **Tiru:** `internal/usecase/export.go`, `internal/adapter/export/jsonl.go`,
`handler/export.go` (streaming). Pakai `encoding/csv`.
**Langkah:** adapter `internal/adapter/export/csv.go` (header dari definisi `survey` v1, teks opsi, `multi`
digabung `; `, awalan `'` untuk sel diawali `= + - @`, BOM `\xEF\xBB\xBF` di awal); route
`GET /admin/export.csv` (`Content-Type: text/csv; charset=utf-8`, `Content-Disposition` attachment).
**Acceptance:** [ ] test membaca hasil dengan `encoding/csv`: header, satu baris per pendataan, nilai teks
opsi, tanpa nomor HP/email; [ ] sel `=1+1` jadi `'=1+1`.
**Verifikasi:** perintah standar backend.

#### Task 64: Statistik pendataan — S
**Spec:** §5.8. **Tiru:** `label_stats.go`, `stats_repo.go`, `handler/admin_stats.go`,
`e2e_admin_stats_test.go`.
**Langkah:** query agregat atas `submissions` (yang belum dihapus) memakai `survey_date`, `rw`, `consent`,
dan `answers->>'b1'` dst. Daftar id B/C diambil dari definisi `survey` v1 (bukan ditulis ulang). Label
pertanyaan ikut di respons.
**Acceptance:** [ ] data uji 3 pendataan (2 bersedia, 1 menolak, 2 RW) → angka persis dihitung manual di
test; [ ] filter tanggal dan RW.
**Verifikasi:** perintah standar backend.

### Checkpoint A: Backend lengkap
- [ ] Perintah standar backend hijau
- [ ] Alur `curl` di schema sementara DB test (`?search_path=<schema>`, hapus setelahnya): register NIM →
      pendataan bersedia + menolak → admin daftar/detail → CSV → statistik
- [ ] Spec diperbarui bila ada penyimpangan

### Phase 2: Frontend

Aturan FE tambahan: tanpa emoji; ikon via `components/Icon.tsx` (import per berkas); warna via token CSS;
360 px tanpa scroll horizontal, target sentuh ≥ 44 px; kode `features/admin` tidak di-import dari bundle user.

#### Task 65: FE registrasi NIM + kelompok — S
**Baca dulu:** `features/auth/LoginPage.tsx`, `api/endpoints.ts`, `api/types.ts`.
**Langkah:** field NIM (otomatis huruf besar, `autocapitalize="characters"`), dropdown kelompok dari
`GET /groups`; tampilkan pesan `INVALID_NIM`, `NIM_TAKEN`, `INVALID_GROUP` di field-nya; tanpa kelompok aktif
→ teks "Belum ada kelompok. Hubungi admin." Tipe `User` ditambah `nim`, `group`.
**Acceptance:** [x] test komponen: kirim body berisi `nim` + `group_id`; error tampil di field benar.
**Verifikasi:** perintah standar FE.
**Hasil:** `LoginPage` tab Daftar: field NIM (huruf besar otomatis, `autocapitalize="characters"`, cek
`^[A-Z0-9]{5,20}$` di FE) + dropdown Kelompok dari `GET /groups` (tanpa token; dimuat saat tab Daftar
dibuka). `INVALID_NIM`/`NIM_TAKEN` tampil di field NIM, `INVALID_GROUP` di field Kelompok (daftar dimuat
ulang). `User` ditambah `nim?`, `group?` (`{id, name}`, opsional agar sesi lama tetap terbaca);
`api.groups()`; pesan error di `lib/errors.ts`. Test: `LoginPage.test.tsx` (5 kasus baru).

#### Task 66: FE logika survey (murni) — S
**Langkah:** tipe definisi di `api/types.ts`; `getSurveyForm()` di `endpoints.ts`; `src/lib/survey.ts`:
`visibleSections(form, answers)` (menolak → hanya A), `missingRequired(form, answers)`,
`validateAnswer(question, value)` (aturan §4.1, sama dengan backend), `isConsented(answers)`.
**Acceptance:** [x] `src/lib/survey.test.ts` meniru kasus test tabel T60 (fixture form kecil di test).
**Verifikasi:** perintah standar FE.
**Hasil:** tipe `SurveyForm`/`SurveySection`/`SurveyQuestion`/`SurveyOption`/`Answers` di `api/types.ts`;
`api.getSurveyForm()` (publik). `src/lib/survey.ts`: `visibleSections`, `missingRequired`,
`validateAnswer(q, value, answers, today)`, `sectionErrors`, `incompleteSections`, `isConsented`,
`cleanAnswers` (menolak → hanya A, kosong dibuang, teks di-trim), `answerText` (teks opsi untuk tampilan),
`todayJakarta`. Fixture form di `src/test/surveyFixture.ts`.
**Catatan (asumsi kontrak, cocokkan dengan T60/T61):**
- Teks opsi "Lainnya" pada `multi` dikirim sebagai **kunci jawaban terpisah** `<id>_other` (mis.
  `"r3_container_types_other": "Galon bekas"`), hanya bila opsi `other` dipilih. Spec §4.1 tidak menyebut
  tempat teks itu; backend harus menerima kunci ini (bukan id tak dikenal).
- `Option.other` dibaca dari JSON `"other": true` (boleh tidak ada = false); `help`, `min`, `max`,
  `options` boleh `null`/tidak ada.
- `int` dikirim sebagai angka JSON; jawaban kosong tidak dikirim (bukan `""`/`null`).

#### Task 67: FE wizard bagian A–R2 + draf — M
**Baca dulu:** `features/submit/NewSubmissionPage.tsx`, `lib/draft.ts`, `components/ui.tsx`.
**Langkah:** `src/features/survey/SurveyPage.tsx` + `QuestionField.tsx` (render per tipe; Ya/Tidak dan opsi
C sebagai tombol besar), satu layar per bagian, Kembali/Lanjut, progres "Bagian x dari y". Kartu identitas
(nama, NIM, kelompok dari sesi). Draf: tambah `answers` + `form_version` ke struktur `lib/draft.ts`.
Menolak di A → langsung ke langkah Kirim.
**Acceptance:** [x] test komponen: navigasi, menolak melompati bagian lain, draf dipulihkan setelah mount
ulang; [x] 360 px OK.
**Verifikasi:** perintah standar FE.
**Hasil:** `features/survey/`: `SurveyPage` (rute `/pendataan/baru`), `SurveyWizard` (satu layar per bagian,
Kembali/Lanjut, "Bagian x dari y", kartu identitas nama/NIM/kelompok dari sesi, teks persetujuan di atas
`a_consent`, ringkasan dengan bagian "Belum lengkap" yang bisa diklik), `QuestionField` (render per tipe;
`choice` = radio asli bergaya tombol besar ≥ 48 px, `multi` = kotak centang besar + isian "Sebutkan
lainnya"), `useSurveyForm` (GET /survey/form sekali per pemuatan). Menolak → hanya bagian A. Draf: `Draft`
ditambah `answers`, `form_version`, `step`; pendataan baru memakai id `baru` (version 0), draf versi form
lain diabaikan, tombol "Mulai dari awal". Error per pertanyaan tampil setelah bagian ditinggalkan lewat
Lanjut. Test: `SurveyPage.test.tsx`. Layout 360 px: grid `auto-fit` (opsi membungkus, tanpa scroll
horizontal).
**Catatan:** foto pendataan baru tidak ikut draf (foto `pending` tidak punya URL lihat, spec induk §5.5),
sama seperti draf lama; jawaban + langkah tetap pulih.

#### Task 68: FE langkah foto, ringkasan, kirim, riwayat — M
**Baca dulu:** `PhotoPicker.tsx`, `usePhotoUploads.ts`, `features/annotate/PhotoAnnotator.tsx`,
`features/history/HistoryPage.tsx`, `features/submission/*`.
**Langkah:** langkah R3: protokol foto (help R3) di atas picker, maks 3 foto, kotak + label memakai komponen
lama, lalu pertanyaan R3. Ringkasan (bagian yang kurang bisa diklik) + Kirim (`POST /submissions` dengan
`answers`). Riwayat: tanggal, RT/RW, kode rumah tangga, Bersedia/Menolak, lencana review. Edit 24 jam memakai
wizard yang sama. Halaman utama → tombol besar "Pendataan baru" + riwayat; route alur foto lama dihapus.
Judul aplikasi "Pendataan Sampah KKN".
**Acceptance:** [x] tidak bisa kirim bila wajib kurang atau 0 foto (bersedia); [x] draf terhapus setelah
kirim; [x] bundle user tanpa `features/admin` (`npm run build` lulus cek bundle).
**Verifikasi:** perintah standar FE.
**Hasil:** `SurveyWizard` lengkap: bagian R3 memuat `PhotoStep` (protokol foto dari `help` R3 di atas
tombol kamera, maks. 3 foto, foto 1 = utama, kotak + label memakai `PhotoAnnotator`/`PhotoStrip` lama)
lalu pertanyaan R3; ringkasan menandai bagian kurang (termasuk foto) dan bisa diklik; Kirim →
`POST /submissions` `{client_request_id, form_version, answers, photos}` (menolak: tombol Kirim langsung di
bagian A, `photos: []`, jawaban hanya A lewat `cleanAnswers`). `422 INVALID_ANSWER` → pesan server +
bagian pertanyaannya dibuka. Edit 24 jam memakai wizard yang sama (`PUT` keadaan akhir + `answers`,
draf per laporan berisi jawaban + foto tersimpan). Beranda `/` = tombol besar "Pendataan baru" + riwayat
(tanggal pendataan, RT/RW, kode rumah tangga, Bersedia/Menolak, lencana review); `/riwayat` dialihkan ke `/`.
Halaman pendataan menampilkan jawaban per bagian dengan teks opsi (`components/SurveyAnswers.tsx`, dipakai
juga admin). Alur foto lama dihapus (`NewSubmissionPage`, `EditSubmission`). Judul "Pendataan Sampah
KKN"; `limits.maxPhotos` = 3. Test: `SurveyPage.test.tsx`, `SubmissionPage.test.tsx`,
`HistoryPage.test.tsx`. 360 px dicek di Chrome headless (API palsu): tanpa scroll horizontal di semua langkah.
**Catatan:** `consent` tidak dibaca dari respons `GET /submissions/{id}` (spec §5.3 hanya menyebut
`form_version` + `answers`); FE menurunkannya dari `answers.a_consent`. Riwayat tetap lokal per perangkat
(belum ada endpoint daftar milik user di spec), diisi ringkasan dari `answers` saat kirim/buka.

#### Task 69: E2E browser — S
**Baca dulu:** `web/e2e/flow.mjs`, `web/README.md` (cara menjalankan dengan `cmd/wanotif-stub`).
**Langkah:** ubah alur: buat kelompok (admin) → register dengan NIM → pendataan menolak → pendataan
bersedia (1 foto + 1 kotak) → admin lihat detail. DB: schema sementara di DB test, hapus setelahnya.
**Acceptance:** [ ] `flow.mjs` lulus penuh.

#### Task 70: FE admin kelompok + profil annotator — S
**Tiru:** `LabelsPage.tsx`, `AnnotatorPage.tsx`, `features/admin/api.ts`.
**Langkah:** menu "Kelompok" (daftar + jumlah mahasiswa, tambah, ubah nama, arsip/aktifkan, hapus dengan
pesan `GROUP_IN_USE`). Profil annotator: NIM, kelompok, pilih kelompok lain.
**Acceptance:** [x] test komponen + api admin.
**Hasil:** `GroupsPage` (`/admin/kelompok`, menu "Kelompok"): tabel (desktop) / kartu (HP) nama, jumlah
mahasiswa, status; tambah, ubah nama, arsipkan/aktifkan (`PUT {name, archived}`), hapus hanya bila belum
dipakai (dialog konfirmasi; `GROUP_IN_USE` → pesan). `adminApi.groups/createGroup/updateGroup/deleteGroup/
setUserGroup`; tipe `AdminGroup`; `AdminUser` + `nim?`, `group?`. Profil annotator: NIM + kelompok di
meta, kartu "Kelompok" dengan dropdown kelompok aktif (selain kelompok saat ini) + "Pindahkan"
(`PUT /admin/users/{id}/group {group_id}`). Pesan `GROUP_NAME_TAKEN`, `GROUP_IN_USE`, `INVALID_GROUP_NAME`,
`INVALID_GROUP`. Test: `features/admin/groups.test.tsx`.
**Catatan (asumsi kontrak, cocokkan dengan T58/T59):** spec §5.5 tidak menyebut bentuk JSON admin.
FE memakai pola label: `GET /admin/groups` → `{ "groups": [ { id, name, archived: bool, user_count,
created_at, updated_at } ] }`; `POST`/`PUT` membalas objek kelompok; `DELETE` → 204; `PUT
/admin/users/{id}/group` → isi respons diabaikan (profil dimuat ulang). `GET /admin/users/{id}` memuat
`nim` (string|null) dan `group` (`{id, name}`|null), sama dengan `/auth/me`. Kode nama tidak valid
diasumsikan `INVALID_GROUP_NAME` (pola `INVALID_LABEL_NAME`).

#### Task 71: FE admin daftar/detail pendataan + export CSV — M
**Tiru:** `SubmissionsPage.tsx`, `SubmissionDetailPage.tsx`, `filters.ts`, `ui/AnnotatedPhoto.tsx`.
**Langkah:** kolom tanggal, RW/RT, kode RT, mahasiswa (nama + NIM), Bersedia/Menolak, review; filter
`consent`, `rw`, `group_id`. Detail: jawaban per bagian dengan **teks opsi** (dari `GET /survey/form`), foto
berkotak. Tombol "Export CSV" (unduh lewat fetch + blob, karena butuh header auth).
**Acceptance:** [ ] test filter → query string; [ ] detail menampilkan teks opsi, bukan id.

#### Task 72: FE admin statistik — S
**Tiru:** `StatsPage.tsx`.
**Langkah:** kartu total/bersedia/menolak; tabel per RW dan per mahasiswa; bar horizontal % Ya per B (CSS,
tanpa library chart baru); distribusi C (tiga angka per butir) dan kondisi pemilahan. Filter tanggal + RW.
**Acceptance:** [ ] test render dari fixture; [ ] kontras AA (`src/styles/contrast.test.ts` hijau).

### Checkpoint B: Aplikasi utuh (lokal)
- [ ] Perintah standar FE + BE hijau; T69 lulus
- [ ] Review dengan Anda (tampilan wizard di HP)

### Phase 3: Deploy

#### Task 73: Instance baru di Pi — M 🔒
**Butuh dari Anda:** domain final untuk `kknsampah.<domain>`, nomor superadmin (diisi langsung di Pi).
**Spec:** §10. **Baca dulu:** bagian "Deploy ke Raspberry Pi" di `CLAUDE.md`, `docker-compose.yml`, README.
Kredensial SSH: `docs/raspi_auth.md` — **jangan pernah ditampilkan/di-commit**; baca ke env, `sshpass -e`.
**Langkah:**
1. Postgres shared: role + DB `baseline_cipadung` (password acak, hanya di `config.json` Pi).
2. SSD: `/mnt/ssd/baseline_cipadung/photos` milik 65532 + file penanda store (lihat
   `storage.local.require_marker` di README/spec §8).
3. Repo: `deploy/baseline-cipadung/docker-compose.yml` (salinan compose dengan project
   `baseline-cipadung`, image `baseline-cipadung:*`/`baseline-cipadung-web:*`, port `127.0.0.1:8090` dan
   `8091`, volume foto baru).
4. Pi: `/home/myrpi/docker/baseline-cipadung/` berisi compose + `config.json` (640, `chown myrpi:65532`,
   `retention.days: 0`, `public_base_url` `https://kknsampah.<domain>`).
5. Build arm64, `docker save | gzip | ssh … docker load`, `docker compose up -d`.
6. cloudflared di host: public hostname `kknsampah.<domain>` → `http://localhost:8091`.
**Jangan:** menyentuh `/home/myrpi/docker/photo-annotated`, DB `photo_annotated`, `wa-notif-service`.
**Acceptance:**
- [ ] `/readyz` 200 lewat `kknsampah.<domain>`; instance lama tetap 200 di `cam.<domain>`
- [ ] Login admin OTP ke nomor asli; buat kelompok; satu pendataan uji dari HP lalu dihapus
- [ ] Log project baru masuk Loki

### Checkpoint C: Go-live
- [ ] Uji lapangan oleh 1–2 mahasiswa dari HP (seluler), bersedia + menolak
- [ ] Admin isi daftar kelompok sebenarnya
- [ ] Review dengan Anda

## Risks and Mitigations

| Risiko | Dampak | Mitigasi |
|---|---|---|
| Sinyal lemah di lapangan | Gagal kirim | Draf otomatis + retry upload yang sudah ada |
| Teks pertanyaan berubah dari DPL | Deploy ulang | Definisi satu sumber; `form_version` naik bila arti berubah |
| FE dan BE beda aturan validasi | Kirim ditolak | FE membaca definisi dari server; kasus test T66 meniru T60 |
| Agent mengubah kontrak sendiri | FE/BE tidak cocok | Aturan §0.5: kontrak persis spec, penyimpangan dicatat |
| Dua instance berebut RAM/port | Instance lama terganggu | Port terpisah, cek RAM setelah deploy |
| Data rumah tangga bocor ke log | Privasi | Log hygiene diperluas (T59, T61) |
