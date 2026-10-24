# Implementation Plan: Photo Annotated

Sumber: [spec.md](../spec.md) (Draft v0.5). Daftar centang ringkas ada di [todo.md](todo.md).

## Overview

Aplikasi web untuk mengunggah 1–5 foto, menganotasi dengan bounding box + teks bebas, dan menyimpannya. Backend Go (Clean Architecture + Gin) berjalan di Raspberry Pi 4 bersama PostgreSQL 17 shared dan `wa-notif`; foto disimpan di SSD lokal; frontend SPA statis di Cloudflare Pages. Auth dapat dikonfigurasi (`none` atau `otp` via WhatsApp), admin memakai akun statis, dan data dapat diekspor ke JSONL untuk model.

## Status awal (24 Okt 2026)

| Item | Status |
|---|---|
| Spec v0.5 | Selesai, di `docs/spec.md` |
| PostgreSQL 17 shared (`/home/myrpi/docker/postgres`) | **Berjalan**, healthy |
| Database `photo_annotated` | Belum dibuat |
| SSD 250 GB (Samsung 970 EVO, RTL9210) | **Siap**: ext4 (label `photos`, UUID `76b14e6c-…`), ter-mount di `/mnt/ssd` via fstab (`nofail`), di USB 3 (`uas`, 5000M). Tulis 211 MB/s, baca 353 MB/s, 127 file/s (400 KB, fsync+rename) |
| Kode Go / FE | Belum ada |

## Architecture Decisions

- **Clean Architecture** (`domain` → `usecase` + ports → `adapter`/`delivery`); handler tipis; error domain dipetakan ke HTTP di satu tempat (spec §3.1).
- **Foto di SSD lokal** di balik port `PhotoStore`; upload lewat URL bertanda tangan ke API, di-stream ke disk (`tmp` → `fsync` → `rename`), isi file divalidasi (spec §5.1b, §8).
- **Mode `none` dikerjakan lebih dulu, `otp` menyusul.** Flow inti (upload → submit → edit) bisa dibangun dan diuji tanpa WhatsApp; `otp` hanya menambah identitas pemilik dan pembatasan akses. Ini juga memisahkan risiko: integrasi `wa-notif` tidak memblokir flow foto.
- **Slice vertikal:** tiap task menghasilkan jalur yang bisa dites lewat `curl`/`go test`, bukan lapisan horizontal.
- **Risiko tertinggi lebih awal:** streaming upload ke SSD (Phase 2) dan OTP lewat `wa-notif` (spike T4).
- **Konvensi:** `go 1.26` (selaras `wa-notif`), module `photo-annotated`, test dengan `go test ./... -race`; test integrasi DB memakai database terpisah `photo_annotated_test` lewat `TEST_DATABASE_URL` (akses dari mesin dev lewat `ssh -L 5432:127.0.0.1:5432 myrpi@192.168.1.222`).

## Dependency Graph

```
T1 PostgreSQL ─▶ T3 DB+secrets ─▶ T7 db/migrate ─┐
T2 SSD ───────────────────────────▶ T10 localfs ─┤
T5 config ─▶ T6 http ─▶ T8 middleware ───────────┤
                                                 ▼
T9 domain ─▶ T11 presign ─▶ T12 upload PUT ─▶ T13 persistence ─▶ T14 POST submissions
                                                                      │
                          ┌───────────────────────────────────────────┤
                          ▼                                           ▼
                 T15 GET+foto url/content ─▶ T16 PUT edit        T17 cleanup
                          │
   T4 spike OTP ─▶ T18 domain phone+schema ─▶ T19 wanotif ─▶ T20 register ─▶ T21 dispatcher ─▶ T22 verify+session ─▶ T23 owner authz+Turnstile
                          │                                                                        │
                          ├──▶ T24 admin login ─▶ T25 export                                       │
                          └──▶ T26 FE upload ─▶ T27 FE annotate ─▶ T28 FE edit ─▶ T29 FE OTP ◀─────┘
                                                                         │
                                                      T30 FE deploy ─▶ T31 Docker/compose ─▶ T32 observability ─▶ T33 backup ─▶ T34 load test
```

Paralelisasi: setelah Checkpoint 3 kontrak API stabil, FE (T26–T28) dapat berjalan paralel dengan Phase 4–6. Harus berurutan: migrasi DB (T13, T18), rantai OTP (T19→T22).

## Task List

### Phase 0: Infrastruktur

#### Task 1: PostgreSQL 17 shared — **SELESAI**
**Description:** Container `shared-postgres` (`postgres:17-alpine`) di `/home/myrpi/docker/postgres`, network `shared-db`, helper `create-service-db.sh`.
**Acceptance criteria:**
- [x] Container healthy, `select version()` = 17.x
- [x] Hanya listen di `127.0.0.1:5432` pada host
**Verification:** [x] `docker exec shared-postgres psql -U postgres -Atc 'select version()'`
**Dependencies:** None · **Scope:** XS

#### Task 2: SSD ext4 ter-mount di port USB 3 — **SELESAI**
**Description:** SSD dipartisi ulang (data lama sudah Anda backup), diformat ext4, ditambahkan ke `/etc/fstab` (`nofail`), lalu dipindah ke port USB 3 setelah sempat tersambung USB 2.0 (±35 MB/s).
**Acceptance criteria:**
- [x] `lsusb -t`: SSD di Bus 002 (`Driver=uas`, 5000M)
- [x] `/mnt/ssd` ter-mount (device berbeda dari `/`), kosong; `/mnt/ssd/photo-annotated/photos/tmp` dimiliki UID 65532 (mode 750; akses admin lewat `sudo`)
- [x] Tulis sekuensial 211 MB/s (≥ 150), baca 353 MB/s; 100 file × 400 KB dengan `fsync`+`rename` = 127 file/s (≥ 100)
- [x] `vcgencmd get_throttled` = `0x0`
**Verification:** [x] benchmark di Pi (24 Okt 2026); [x] `umount` lalu `mount -a` berhasil
**Catatan:** jika SSD dicabut dan dipasang lagi, pastikan di port biru; periksa dengan `lsusb -t`.
**Dependencies:** None · **Scope:** XS (operasional)

#### Task 3: Database `photo_annotated` + secret env — **SELESAI**
**Description:** Buat database dan role (`create-service-db.sh photo_annotated` dan `photo_annotated_test`), lalu siapkan file env di Pi (mode 600, di luar git) berisi `DATABASE_URL`, `STORAGE_SIGNING_SECRET`, `EDIT_TOKEN_SECRET`, `OTP_PEPPER`, `ADMIN_TOKEN_SECRET`, `WA_NOTIF_TOKEN`.
**Acceptance criteria:**
- [x] Dua database ada, masing-masing dengan role sendiri (bukan superuser)
- [x] File env mode 600 di Pi (`/home/myrpi/docker/photo-annotated/.env`); secret acak 48 karakter; tidak ada secret di repo (`.env.local` di-gitignore). `WA_NOTIF_TOKEN` dan `TURNSTILE_SECRET` diisi saat T4/T23
- [x] Tes koneksi dari mesin dev lewat SSH tunnel (`scripts/db-tunnel.sh`, port 15432) berhasil
**Verification:** [x] koneksi pgx ke kedua database: PostgreSQL 17.11, bukan superuser
**Dependencies:** 1 · **Scope:** XS (operasional)

#### Task 4: Spike OTP via wa-notif — **SELESAI** (24 Okt 2026, lewat API di Pi)
**Description:** Kirim satu pesan lewat `POST /api/v1/messages/send` ke nomor Anda sendiri, dan satu ke nomor yang **tidak memakai WhatsApp**, untuk menjawab open question #2. Catat respons/error aktual.
**Acceptance criteria:**
- [x] Pesan ke nomor sendiri (…949) diterima (dikonfirmasi user); terkirim < 1 detik setelah diantrekan
- [x] Nomor tanpa WhatsApp (…7460): `500 SEND_FAILED` konsisten di 3 percobaan → `failed` ±14 detik; kanal tetap tersedia (spec §14.2 #2)
- [x] Putusan: endpoint cek nomor **tidak** diperlukan
**Catatan:** sebelum uji, `wa-notif` di-restart (izin user) karena sesi DISCONNECTED sejak 20 Okt; tersambung lagi tanpa QR.
**Verification:** [x] curl dari Pi; hasil ditulis ke `docs/spec.md` §14.2
**Dependencies:** None · **Scope:** XS · **Butuh:** nomor tanpa WhatsApp dari Anda

### Phase 1: Foundation

#### Task 5: Skeleton proyek + config loader — **SELESAI**
**Description:** `go.mod`, struktur direktori Clean Architecture, loader `config.json` + override env dengan validasi fail-fast (spec §11.4, §9.1).
**Acceptance criteria:**
- [x] Config tidak lengkap (mis. `otp` tanpa `OTP_PEPPER`, `local.root` tidak ada) → gagal dengan semua masalah dilaporkan sekaligus
- [x] Secret hanya dari env; field tak dikenal di file (termasuk secret) ditolak; `Secrets` disamarkan di `%v`/`%#v`/slog
- [x] `config.json.example` sesuai spec §11.4 dan selalu diuji valid (`TestExampleConfigIsValid`)
**Verification:** [x] `go vet ./... && go test ./internal/config/... -race` (28 test); [x] `go build ./...`
**Dependencies:** None · **Files:** `go.mod`, `internal/config/config.go`, `internal/config/config_test.go`, `config.json.example` · **Scope:** S

#### Task 6: HTTP server skeleton — **SELESAI**
**Description:** Gin (release mode, `gin.New()`), middleware `request_id`, slog JSON logger (tanpa data sensitif), recovery, trusted proxy + `CF-Connecting-IP`, format error JSON, `GET /healthz`, graceful shutdown. Timeout: `ReadHeaderTimeout 10s`, `IdleTimeout 60s`, deadline per-route lewat `ResponseController` (spec §11.3).
**Acceptance criteria:**
- [x] `GET /healthz` → 200; setiap response punya `X-Request-ID` (id klien hanya diterima bila aman); log memuat field minimum spec §12; query string, header, dan body tidak pernah di-log
- [x] Panic di handler → 500 berformat error tanpa detail, stack hanya di log, proses tetap hidup
- [x] `client_ip` hanya dari `CF-Connecting-IP` bila koneksi dari `app.trusted_proxies` (config baru); `TrustedPlatform` Gin tidak dipakai karena mempercayai header dari siapa pun
- [x] Server tanpa `ReadTimeout`/`WriteTimeout` global; deadline per-route lewat `ResponseController` (diuji: body lambat diputus)
**Verification:** [x] `go test ./internal/... -race`; [x] binary dijalankan: `curl /healthz`, SIGTERM graceful, gagal-start saat secret kurang
**Dependencies:** 5 · **Files:** `cmd/api/main.go`, `internal/delivery/http/router.go`, `.../middleware/{requestid,logger,recovery}.go` · **Scope:** M

#### Task 7: Koneksi DB, tool migrasi, `/readyz` — **SELESAI**
**Description:** `pgxpool` (MaxConns dari config), `cmd/migrate` (golang-migrate atau goose), migrasi `0001` kosong/ekstensi bila perlu, `GET /readyz` memeriksa DB saja.
**Acceptance criteria:**
- [x] `cmd/migrate up/down/status` berjalan (goose, SQL disematkan, session lock) terhadap DB di Pi; up kedua idempoten; down lalu up ulang berhasil
- [x] `/readyz` 200 saat DB hidup, 503 `NOT_READY` saat DB mati (tunnel diputus sungguhan); `/healthz` tetap 200; detail error hanya di log
- [x] API gagal start jika DB tak terjangkau (`NewPool` ping saat startup)
- [x] Helper `internal/testutil/pgtest`: schema acak per test + DROP saat selesai (0 schema yatim), test dilewati bila `TEST_DATABASE_URL` kosong
**Verification:** [x] `TEST_DATABASE_URL=… go test ./... -race` (integrasi benar-benar jalan, bukan skip); [x] `curl /readyz` pada binary asli
**Dependencies:** 3, 6 · **Files:** `cmd/migrate/main.go`, `internal/adapter/postgres/pool.go`, `migrations/0001_init.sql`, handler readyz · **Scope:** M

#### Task 8: Middleware batas (rate limit, CORS, body limit) — **SELESAI**
**Description:** Rate limiter per-IP in-memory (token bucket) yang dapat dikonfigurasi per route, CORS (`GET,POST,PUT,OPTIONS`; header `Content-Type, Authorization, X-Edit-Token`; preflight cache 1 jam), batas body JSON 64 KB (spec §9.4).
**Acceptance criteria:**
- [x] Melebihi limit → `429 RATE_LIMITED` + `Retry-After` tepat (sliding window log; tepat N lolos dari 200 goroutine paralel; IPv6 per /64; jumlah kunci dibatasi)
- [x] Preflight `OPTIONS /api/v1/uploads/x` dari origin sah → 204 dengan header benar (juga untuk rute yang belum ada); origin/metode/header lain → 403; `Retry-After` dan `X-Request-ID` boleh dibaca FE
- [x] Body JSON > 64 KB → 413 (lewat `Content-Length` tanpa membaca body, dan lewat `MaxBytesReader` untuk chunked); `httpx.DecodeJSON` ketat (415/400/413); rute upload tidak memakai grup ini
**Verification:** [x] `go test ./... -race`; [x] curl pada binary: preflight 204/403, body 100 KB → 413
**Dependencies:** 6 · **Files:** `middleware/{ratelimit,cors,bodylimit}.go` + test · **Scope:** M

### Checkpoint 1: Foundation
- [x] `go vet`, `go test ./... -race`, `go build ./cmd/...` bersih
- [x] `/healthz` dan `/readyz` berjalan di mesin dev terhadap DB di Pi
- [ ] Review dengan Anda sebelum Phase 2 (pekerjaan dilanjutkan; review dapat dilakukan kapan saja)

### Phase 2: Upload foto (mode `none`)

#### Task 9: Domain — aturan foto & anotasi — **SELESAI**
**Description:** Entity `Photo`, `Annotation`, status foto, error domain, validasi murni: tipe konten, ukuran ≤ 2 MB, sisi terpanjang ≤ 2048, bbox (spec §6.3), teks anotasi (1–1000 rune, tanpa karakter kontrol selain newline).
**Acceptance criteria:**
- [x] Table-driven test (52 kasus) mencakup batas (0, 0.005, 1, `x+w>1`, toleransi 1e-6, NaN/Inf, teks kosong/1001 rune/karakter kontrol/bidi override/UTF-8 rusak, kunci penyimpanan `../`, `\n`, huruf besar)
- [x] Satu-satunya dependensi non-stdlib: `github.com/google/uuid` (tipe identitas). Direvisi dari "hanya stdlib"
- [x] Keputusan: toleransi `1e-6` pada `x+w ≤ 1` dan kolom koordinat `double precision` (spec §6.3, §7.3 diperbarui)
- [x] `domain.Error` dengan `Kind` (→ status HTTP dipetakan di delivery) + `Code` stabil untuk klien
**Verification:** [x] `go test ./internal/domain/... -race`
**Dependencies:** 5 · **Files:** `internal/domain/{photo,annotation,errors}.go` + test · **Scope:** S

#### Task 10: Adapter `localfs` (PhotoStore) + penjagaan SSD — **SELESAI**
**Description:** Implementasi `Save` (tmp → fsync → rename → fsync dir), `Open`, `Stat`, `Delete` (idempotent), `FreeSpace`, memakai `os.Root`; cek `require_marker` dan `min_free_bytes` (spec §8).
**Acceptance criteria:**
- [x] `storage_key` dengan `..`/absolut/NUL/bentuk tak kanonik ditolak; symlink yang menunjuk keluar root ditolak oleh `os.Root`
- [x] Penulisan terputus (error baca, konteks dibatalkan, melebihi batas, pembaca tak berujung) tidak meninggalkan file di lokasi akhir, tmp bersih, dan isi lama tetap utuh saat PUT ulang gagal; 20 penulis serentak pada kunci yang sama tidak mencampur isi
- [x] `require_marker=true` tanpa file penanda `.photo-annotated-store` → gagal start; penanda yang hilang saat runtime terdeteksi setelah TTL cache (cek device-vs-induk dibuang: terbukti lolos palsu pada bind mount Docker dari SD card)
- [x] `Delete` file yang tidak ada → sukses; ENOSPC → `STORAGE_FULL`, EIO/EROFS → `STORAGE_UNAVAILABLE`; sisa ruang < `min_free_bytes` → `STORAGE_FULL`
- [x] Pada SSD di Pi (dijalankan sebagai UID 65532): seluruh test PASS, root tanpa penanda ditolak, dengan penanda lolos; 54 file/s sekuensial, 312 file/s dengan 16 upload paralel (target 30)
**Verification:** [x] `go test ./internal/adapter/localfs/... -race` (`t.TempDir()`); [x] di Pi: root SSD + penanda lolos, direktori SD card tanpa penanda gagal (`TestRealRoot`, arm64)
**Catatan:** penanda `/mnt/ssd/photo-annotated/photos/.photo-annotated-store` sudah dibuat di Pi (pemilik UID 65532, mode 640).
**Dependencies:** 2, 5 · **Files:** `internal/adapter/localfs/{store,mountcheck}.go` + test, `usecase/ports.go` · **Scope:** M

#### Task 11: `POST /uploads/presign` — **SELESAI**
**Description:** Migrasi tabel `photos`, `PhotoRepo` postgres, `URLSigner` HMAC (pesan berawalan `upload`), usecase presign, handler. Menolak `503 STORAGE_FULL/UNAVAILABLE`.
**Acceptance criteria:**
- [x] Respons sesuai spec §5.1 (URL absolut dari `public_base_url`, `exp`, `sig`; `expires_at` selalu UTC `Z`)
- [x] Validasi §5.1 mengembalikan `422` dengan kode yang benar; validasi mendahului cek penyimpanan dan tidak menyentuh DB
- [x] `storage_key` dibuat server `{yyyy}/{mm}/{dd}/{uuid}.{ext}` (UUIDv7); baris `photos` berstatus `pending`; penyimpanan tidak siap → `503 STORAGE_FULL/UNAVAILABLE` **tanpa** membuat baris
- [x] Mengubah ukuran/content-type/id/exp membuat `sig` tidak valid; sig cacat (padding, base64 standar, panjang salah) ditolak
- [x] Rate limit per IP dari `limits.rate_limit_per_min.presign`; pemetaan error domain→HTTP terpusat (`httpx.WriteError`); error internal tidak bocor ke klien tetapi tercatat di log
- [x] Migrasi `0002_photos` dengan CHECK constraint (tipe, ukuran, dimensi, status, panjang sha256, `uploaded_at`⇔`sha256`, `attached`⇒submission). FK ke `submissions`/`users` ditambahkan di migrasi T13/T18
**Verification:** [x] test signer, usecase (fake), repo + constraint (DB nyata), handler, e2e (Postgres + localfs + signer); [x] di Pi sebagai UID 65532 dengan SSD & DB asli: presign 201, validasi 422, tanpa penanda API menolak start, penanda dihapus saat jalan → 503 `STORAGE_UNAVAILABLE` lalu pulih sendiri; log tanpa `sig`/secret
**Dependencies:** 7, 8, 9, 10 · **Files:** `migrations/0002_photos.sql`, `adapter/postgres/photo_repo.go`, `adapter/signer/signer.go`, `usecase/presign.go`, `delivery/http/handler/upload.go` · **Scope:** M

#### Task 12: `PUT /uploads/{photo_id}` (streaming) — **SELESAI**
**Description:** Verifikasi `exp`/`sig`, slot upload (`max_concurrent_uploads`), stream ke `tmp` dengan buffer 32 KB + SHA-256, validasi magic bytes dan dimensi (`image.DecodeConfig`, WebP via `x/image/webp`), simpan, isi `uploaded_at` + `sha256`, respons 204. Deadline baca 60 detik per-route.
**Acceptance criteria:**
- [x] Alur sukses: file ada di SSD, `sha256` benar, `uploaded_at` terisi, `tmp/` bersih (JPEG asli dan WebP)
- [x] `Content-Length` ≠ deklarasi / byte aktual lebih atau kurang → `422 SIZE_MISMATCH` (foto tetap `pending`, bisa diulang); tanpa `Content-Length` → `411`; non-gambar, magic bytes tak cocok, atau dimensi salah → `422 INVALID_IMAGE` dan foto `rejected`, tak ada file terbit
- [x] `sig` salah, id tak dikenal, `exp`/`sig` hilang atau cacat → 403 `INVALID_SIGNATURE` (jawaban sama, keberadaan foto tak bocor); `exp` lewat → 410 (hanya bila tanda tangan sah); bukan `pending` → 409; slot penuh → `503 UPLOAD_BUSY` + `Retry-After`; upload ganda pada foto yang sama → `409 UPLOAD_IN_PROGRESS`; klien lambat → `408`; body > batas → `413` sebelum dibaca
- [x] PUT ulang pada foto `pending` menimpa file secara atomik dan memperbarui hash; slot dan kunci selalu dilepas (sukses, gagal, klien putus)
- [x] Memori konstan: 200 upload × 400 KB serentak lewat HTTP asli menaikkan heap hanya 12–15 MB (naif ≥ 80 MB), diuji otomatis
- [x] Temuan test: `ParseInt` overflow pada `exp` mengembalikan `MaxInt64`; kini diperlakukan sebagai 0
**Verification:** [x] `go test ./... -race` (usecase diulang 20×); [x] di Pi (UID 65532, SSD & DB asli, alat `cmd/uploadbench`): **99.6 foto/s** (16 paralel) dan **168 foto/s** (32 paralel) @ 400 KB, p95 PUT 153–267 ms, 21 foto/s sekuensial, RSS API 23.5 MB, suhu 52 °C tanpa throttling; 950 baris DB = 950 file, tmp kosong; target ≥ 30 foto/s tercapai ±3–5×
**Catatan:** uji dari mesin dev lewat LAN hanya 60 Mbps (18 foto/s) dan tidak berubah saat ukuran file digandakan, jadi dibatasi tautan mesin dev ke Pi, bukan server.
**Dependencies:** 11 · **Files:** `usecase/upload.go`, `handler/upload_put.go`, `domain/imagecheck.go` + test · **Scope:** M

### Checkpoint 2: Upload
- [x] Alur presign → PUT → file di SSD berjalan di Pi dengan SSD asli
- [x] File palsu ditolak; mount SSD dicabut → presign `503 STORAGE_UNAVAILABLE` (diuji T11 dan T12)
- [ ] Review dengan Anda sebelum Phase 3 (pekerjaan dilanjutkan; review dapat dilakukan kapan saja)

### Phase 3: Submission (mode `none`, `edit_token`)

#### Task 13: Persistensi submission + transaksi — **SELESAI**
**Description:** Migrasi `submissions` + `annotations` (CHECK constraint bbox & teks), `SubmissionRepo`, `AnnotationRepo`, `TxManager.WithTx`.
**Acceptance criteria:**
- [x] CHECK constraint menolak bbox/teks tidak valid walau lolos aplikasi (14 kasus batas, termasuk toleransi 1e-6); aturan domain dan CHECK diuji selaras
- [x] Rollback transaksi (error maupun panic) tidak meninggalkan baris setengah jadi; transaksi bersarang bergabung ke luar; data transaksi yang belum commit tak terlihat dari luar
- [x] `client_request_id` UNIQUE → `DUPLICATE_CLIENT_REQUEST`; pola idempotensi (rollback lalu baca ulang di luar transaksi) teruji
- [x] `Attach` atomik dan bersyarat (pending + ter-upload + pemilik sama): dari 12 transaksi yang berebut satu foto tepat satu menang; `BumpVersion`: dari 10 edit dengan versi sama tepat satu menang (dasar `409 VERSION_CONFLICT`)
- [x] `GetDetail` satu statement JOIN (snapshot konsisten): urutan foto menurut `position`, anotasi menurut urutan pembuatan, foto tanpa anotasi → slice kosong, foto `deleted` tak ikut; `double precision` kembali persis sama
- [x] FK `photos.submission_id → submissions` ON DELETE CASCADE (anotasi ikut), FK anotasi → foto; `ReplaceAnnotations` atomik (gagal tidak menghapus yang lama)
- [x] Config menolak `max_annotation_text_len` > 1000 (batas CHECK DB); test lama T11 diperbaiki karena FK baru kini menolak `submission_id` fiktif
**Verification:** [x] `TEST_DATABASE_URL=… go test ./internal/adapter/postgres/... -race` (test balapan diulang 5×); [x] `migrate up/down/up` pada DB dev
**Dependencies:** 7, 11 · **Files:** `migrations/0003_submissions.sql`, `adapter/postgres/{submission_repo,tx}.go` + test · **Scope:** M

#### Task 14: `POST /submissions` (idempotent) + `edit_token` — **SELESAI**
**Description:** Usecase submit: validasi §5.2, verifikasi foto ter-upload (`uploaded_at`, `Stat` ukuran), simpan dalam satu transaksi, foto → `attached`, `edit_token` HMAC stateless (spec §9.3), idempotensi `client_request_id`.
**Acceptance criteria:**
- [x] Foto belum ter-upload → `422 PHOTO_NOT_UPLOADED`, foto tetap `pending`; foto tak ada atau milik orang lain → `422 PHOTO_NOT_FOUND` (jawaban sama); sudah dipakai/rejected/deleted → `409 PHOTO_NOT_AVAILABLE`; file hilang atau ukuran berubah → `422 PHOTO_FILE_INVALID` dan hanya foto itu yang `rejected`; SSD bermasalah → 503 tanpa menandai foto
- [x] Request ulang dengan `client_request_id` sama → `200`, submission dan `edit_token` sama, `version` terkini, `created_at` identik (mikrodetik), tanpa baris baru; id milik pemilik lain → `409 DUPLICATE_CLIENT_REQUEST`
- [x] 1–5 foto, ≤ 50 anotasi/foto (batas 0, 1, 50, 51 diuji), foto kembar, bbox/teks salah dengan lokasi `photos[i].annotations[j]`; teks dinormalisasi
- [x] Atomik: kegagalan Attach kedua membatalkan submission, anotasi, dan status foto pertama; dari 8 request berebut satu foto tepat satu menang tanpa baris sisa; dari 10 request identik berbarengan tepat satu submission
- [x] **Bug balapan ditemukan test dan diperbaiki:** yang kalah pada request identik bisa gagal `PHOTO_NOT_AVAILABLE` sebelum cek ulang; kini kegagalan jenis konflik memicu pembacaan ulang idempotensi (`replayOnConflict`); stabil di 12 putaran `-race`
- [x] `edit_token` stateless (HMAC `EDIT_TOKEN_SECRET`, domain pesan terpisah dari tanda tangan URL); handler, rate limit 10/menit per IP, IP klien dicatat
**Verification:** [x] test usecase terhadap Postgres + localfs sungguhan, handler (fake), e2e HTTP asli (presign→PUT→submit, replay, balapan); [x] di Pi dengan `curl` (SSD & DB asli): 201 → replay 200 → 409 → 422; 1 submission, 2 anotasi, file 42 byte di SSD; log tanpa `edit_token`/`sig`
**Dependencies:** 12, 13 · **Files:** `usecase/submit.go`, `adapter/signer/edittoken.go`, `handler/submission.go` + test · **Scope:** M

#### Task 15: Baca submission + lihat foto — **SELESAI**
**Description:** `GET /submissions/{id}` (dengan `version`, `editable_until`), `GET /photos/{id}/url`, `GET /photos/{id}/content` (`ServeContent`, header `nosniff`, deadline tulis diperpanjang). Otorisasi pemilik via `X-Edit-Token`; resource orang lain → 404.
**Acceptance criteria:**
- [x] Tanpa token, token salah, token submission lain, dan id tak ada → `404` dengan badan yang persis sama (bukan oracle); foto `pending`/`deleted`/`rejected`/milik submission lain → `404` pada `/url`; kredensial dimodelkan `Caller{EditToken, UserID, Admin}` sehingga user dan admin (T22–T24) tinggal diisi middleware
- [x] `GET /submissions/{id}`: satu snapshot konsisten, foto berurutan, anotasi lengkap, `editable_until`/`editable` memakai jam server (batas diuji: `-1ns` true, tepat batas false); field internal tak bocor (`storage_key`, `sha256`, owner, ip)
- [x] `/content`: `Range`→206 (dan 416), `If-None-Match`→304, `ETag`=SHA-256, `nosniff`, `inline`, `Cache-Control`; `sig` salah/hilang/id tak dikenal → 403 seragam; `exp` lewat → 410; file ditutup tepat sekali
- [x] URL lihat tidak berlaku untuk upload dan sebaliknya (domain pesan HMAC terpisah, diuji dua arah, termasuk e2e)
**Verification:** [x] `go test ./… -race` (101 test, 0 skip); [x] e2e HTTP asli; [x] di Pi (SSD & DB asli): 200 / 404 seragam / konten identik 42 byte / `Range` 206 `0-3/42` / `If-None-Match` 304 / sig diubah 403; log tanpa `edit_token`/`sig`
**Dependencies:** 14 · **Files:** `usecase/{get,photourl,photo_content}.go`, `handler/{submission_get,photo}.go` · **Scope:** M

#### Task 16: `PUT /submissions/{id}` (edit) — **SELESAI**
**Description:** Replace semantik, `version` optimistic locking, jendela edit 24 jam, foto yang dihapus → `deleted`, foto baru `pending` → `attached`.
**Acceptance criteria:**
- [x] `version` basi → `409 VERSION_CONFLICT` tanpa mengubah apa pun (versi, foto baru tetap `pending`); dari 10 edit serentak dengan versi sama tepat satu menang (usecase dan e2e HTTP asli); `INVALID_VERSION` untuk < 1
- [x] Jendela 24 jam dari `created_at`: `-1µs` boleh, tepat batas → `403 EDIT_WINDOW_CLOSED`; edit tidak memperpanjang jendela; jendela diperiksa sebelum versi tetapi setelah otorisasi (tanpa hak tetap 404 seragam)
- [x] Foto yang dibuang → `deleted` dengan anotasi dihapus seketika (file tetap ada sampai cleanup T17), foto tidak bisa dihidupkan lagi; foto baru diperiksa (pemilik, upload, file di disk) dan kegagalannya tidak menyentuh data
- [x] Hapus + tambah + ubah urutan sekaligus; turun ke 1 foto boleh, ke 0 ditolak; batas 50 anotasi dan normalisasi teks
- [x] Atomik: Attach kedua gagal di tengah transaksi → versi, foto yang akan dibuang, anotasi lama, dan status foto baru kembali seperti semula
- [x] Admin: `403 EDIT_NOT_ALLOWED` (boleh baca, tidak boleh edit); user bukan pemilik: 404; foto baru harus milik pemilik submission yang sama
- [x] Edit tidak mengubah `created_at`/`client_request_id`/pemilik; POST yang diulang setelah edit menjawab versi terkini tanpa menimpa hasil edit
- [x] POST dan PUT berbagi satu limiter per IP (10/menit); `client_request_id` pada PUT ditolak 400
**Verification:** [x] `go test ./... -race` (balapan diulang 5–8×); [x] di Pi (SSD & DB asli): edit 200 versi 1→2 (buang P2, tambah P3, urutan baru), versi basi 409, tanpa/ salah token 404, `/url` foto dibuang 404 dan foto baru 200, 0 anotasi pada foto deleted
**Dependencies:** 15 · **Files:** `usecase/edit.go`, `handler/submission_put.go` + test · **Scope:** M

### Checkpoint 3: Alur anonim end-to-end
- [x] e2e (Go test HTTP asli) dan `curl` di Pi: presign → PUT → submit → replay → get → lihat foto → edit berhasil
- [x] `go test ./... -race` hijau; memori stabil saat 200 upload paralel (heap +12–15 MB)
- [x] **Kontrak API stabil**: FE boleh mulai (Phase 7)
- [ ] Review dengan Anda (pekerjaan dilanjutkan; review dapat dilakukan kapan saja)

### Phase 4: Retensi

#### Task 17: Cleanup job — **SELESAI**
**Description:** Scheduler di proses API (interval dari config): foto yatim/`rejected`/`deleted`, retensi 90 hari, sapuan `tmp/*.part` > 1 jam dan file yatim > 24 jam, data auth (users `pending`, challenges, sessions). Objek dulu, baris kemudian.
**Acceptance criteria:**
- [x] File dulu, baris kemudian: kegagalan hapus file mempertahankan baris (dan submission untuk retensi) dan dicoba ulang run berikutnya; baris yang gagal tidak dicoba ulang dalam run yang sama, tidak membuat run berputar, dan tidak menghalangi baris lain
- [x] Foto pending basi di-claim atomik (tidak bisa lagi di-attach, termasuk saat berbalapan dengan `Attach`); batas TTL dan retensi ketat (tepat pada batas belum dihapus); foto attached, muda, dan submission baru tak tersentuh
- [x] Retensi 90 hari menghapus file, baris foto, anotasi (CASCADE), lalu submission; submission tanpa foto ikut dihapus; foto yang dibuang lewat edit dibersihkan
- [x] Sapuan file yatim: hanya file lama tanpa baris berstatus apa pun; file muda, file ber-baris, berkas asing, penanda, dan `tmp/` tak tersentuh; 450 file lintas 3 chunk; **bila pengecekan DB gagal tidak ada yang dihapus**; konteks batal tidak menghapus apa pun
- [x] Idempoten (putaran kedua kosong), 4 run serentak tanpa error, batas `MaxBatches`, langkah tambahan (`CleanupStep`) dengan error terisolasi, `scheduler` (tidak tumpang tindih, panic ditangkap, error shutdown tidak dicatat)
- [x] Config baru: `orphan_sweep_interval`, `tmp_max_age`, `orphan_min_age` (divalidasi; `orphan_min_age` ≥ `upload_url_ttl`); `cmd/maintenance` untuk menjalankan sekali
**Verification:** [x] `go test ./... -race` (termasuk 14 test repo, 7 test localfs, 25 test usecase cleanup/sapuan, 6 test scheduler); [x] di Pi (SSD & DB asli, UID 65532, `cmd/maintenance`): dari 6 file .webp dan 1 tmp lama → 3 file, 0 tmp; submission 100 hari, pending 25 jam, foto rejected, file yatim 3 hari terhapus; submission baru, pending muda, yatim muda (1 jam) utuh dan masih bisa dibaca; putaran kedua kosong
**Dependencies:** 14, 16 · **Files:** `usecase/cleanup.go`, `adapter/postgres/cleanup_queries.go`, `cmd/api/scheduler.go` + test · **Scope:** M

### Checkpoint 4: Backend inti (mode `none`) selesai
- [x] Setara milestone M1 spec; semua test hijau
- [x] Demo end-to-end di Pi dengan SSD

### Phase 5: Auth OTP

#### Task 18: Domain nomor HP + skema auth — **SELESAI**
**Description:** Normalisasi nomor Indonesia (`^628[0-9]{8,12}$`, tolak `@`), entity `User/OTPChallenge/Session`, migrasi `users`, `otp_challenges`, `sessions`, tambah FK `owner_id` pada `submissions`/`photos`.
**Acceptance criteria:**
- [x] 16 bentuk penulisan (`0812-3456-7890`, `+62 812…`, `0062…`, `+62 0812…`, …) → satu bentuk kanonik, idempoten; 24 masukan ditolak (JID grup `…@g.us`, `@`, huruf, `+` di tengah, kode negara lain, `00…` selain `0062`, telepon rumah, terlalu pendek/panjang, karakter kontrol, angka non-ASCII, bidi override, SQL injection); `MaskPhone`; nama (1–100, tanpa kontrol/bidi) dan email (satu alamat polos, huruf kecil, tanpa injeksi header)
- [x] Migrasi 0004 (`users`, `otp_challenges`, `sessions`, FK `owner_id` dengan `SET NULL`) up/down/up bersih pada DB dev; CHECK menolak nomor tak kanonik, email huruf besar, `active` tanpa verifikasi, `failure_reason` pada status bukan failed, hash bukan 32 byte, sesi `expires_at ≤ created_at`; email tidak unik, nomor unik; indeks unik parsial: satu challenge aktif per nomor
- [x] Repo `UserRepo`/`OTPRepo`/`SessionRepo` + konkurensi: 10 `FindOrCreatePending` serentak → 1 pembuat; `ClaimNext` 12 item × 5 worker → tepat sekali; 20 `IncrementVerifyAttempts` serentak → nilai 1..20 unik; 10 `Consume` serentak → 1 pemenang; `Replace` serentak → tepat 1 challenge aktif
- [x] `AuthCleanup` (`CleanupStep`): hanya yang jatuh tempo dihapus (batas ketat), challenge/sesi yang masih berjalan atau dalam masa tenggang aman, multi-batch, idempoten, terintegrasi di `RunOnce`
- [x] Test lama disesuaikan: FK baru menolak `owner_id` fiktif, jadi `pgtest.CreateUser` membuat pemilik nyata
**Verification:** [x] `go test ./... -race` hijau; [x] `migrate up/down/up` pada DB dev (versi 4)
**Dependencies:** 13 · **Files:** `domain/{user,phone,otp}.go`, `migrations/0004_auth.sql`, repo + test · **Scope:** M

#### Task 19: Adapter `wanotif` + probe kanal + stub — **SELESAI**
**Description:** `OTPSender` HTTP client ke `wa-notif` (pemetaan error spec §9.5), probe `GET /api/v1/session/status` per 30 detik, status `otp_available`, dan **stub `wa-notif`** (jeda 1,5 detik) untuk test/load test.
**Acceptance criteria:**
- [x] 14 kombinasi respons (503/500/401/400/404/405/429/502/504, 200 bukan JSON / `success:false` / tanpa `message_id`) + timeout, koneksi ditolak, konteks batal dipetakan ke `SendError{Reason, Retryable, Alert}`; pembatalan konteks (shutdown) bukan kegagalan gateway; error tidak pernah memuat isi pesan/kode OTP/token/URL lengkap (diuji dengan server yang memantulkan semuanya); redirect tidak diikuti; badan respons dibatasi 64 KB
- [x] Pagar tujuan: JID grup/pengguna, format lokal, `+`, huruf, newline, negara lain ditolak **tanpa menyentuh jaringan** (0 panggilan ke server)
- [x] `Channel`: tersedia sampai ada bukti sebaliknya; 3 gagal kirim berturut-turut atau probe gagal → tidak tersedia; kirim sukses mereset hitungan; probe sukses memulihkan (mencegah kanal terkunci); transisi dilaporkan lewat `changed`; aman konkuren
- [x] Stub `wa-notif` (`internal/testutil/wastub` + `cmd/wanotif-stub`): meniru auth Bearer (status publik seperti aslinya), pengiriman diserialkan dengan jeda, 503/500/400/200-rusak/hang, mencatat pesan mentah (termasuk JID) dan mengekstrak kode OTP untuk test
- [x] `cmd/wanotif-check` (hanya baca status)
**Temuan terhadap `wa-notif` ASLI:** `/api/v1/session/status` dan `/qr` ternyata publik (dokumentasi menyebut butuh token); sesi WhatsApp **`DISCONNECTED` sejak 20 Okt 09:12 WIB** (koneksi awal gagal dan tidak pernah dicoba lagi) padahal `/health` UP. Probe kita mendeteksinya dengan benar. Pemulihan butuh `docker restart wa-notif-service` (tindakan di service Anda; belum dilakukan)
**Verification:** [x] `go test ./... -race` (stub, adapter, channel); [x] probe terhadap `wa-notif` asli di 192.168.1.222:3030 (hanya baca): `TIDAK TERSEDIA … DISCONNECTED`; terhadap stub: `TERSAMBUNG`; URL salah: ditolak
**Dependencies:** 4, 5 · **Files:** `adapter/wanotif/{client,probe}.go`, `cmd/wanotif-stub/main.go` + test · **Scope:** M

#### Task 20: `POST /auth/register` & `/auth/otp/request` (antrean) — **SELESAI**
**Description:** Buat user `pending`/pakai yang ada, challenge `queued` (kode HMAC dengan `OTP_PEPPER`), respons 202 seragam (anti-enumerasi), cooldown 60 detik (kecuali `failed`), 5/jam per nomor, `max_queue`, `daily_global_cap`, Turnstile, `503 OTP_UNAVAILABLE`.
**Acceptance criteria:**
- [x] Nomor terdaftar dan tidak terdaftar menghasilkan respons berbentuk sama (4 field, status 202), termasuk batas cooldown/per jam yang **sama persis** (limiter per nomor di memori, bukan hitungan tabel OTP yang hanya ada untuk nomor terdaftar); nomor tak terdaftar tidak membuat user maupun challenge
- [x] Batas per nomor/antrean/harian ditegakkan (`OTP_BUSY` + `Retry-After`); penolakan karena kapasitas atau validasi tidak memakai jatah nomor; request baru membatalkan challenge lama; 12 register berbarengan untuk nomor sama → tepat 1 user dan 1 challenge aktif (sisanya `409 OTP_REQUEST_IN_PROGRESS`); gagal menyimpan challenge mengembalikan jatah (`PhoneLimiter.Refund`, juga dipakai dispatcher T21 saat kirim gagal → cooldown tidak berlaku setelah `failed`)
- [x] `name`/`email` tidak menimpa user yang sudah ada
- [x] Turnstile gagal tertutup: token salah/kosong → `403 TURNSTILE_FAILED` (token kosong tanpa memanggil Cloudflare), Cloudflare tak terjangkau/500/bukan JSON/redirect/timeout → `503 TURNSTILE_UNAVAILABLE`; tanpa verifikasi tidak ada user dibuat
- [x] Rute `/auth/*` hanya ada pada `auth.mode = otp` (mode `none` → 404); `register` dan `otp/request` berbagi satu limiter per IP
**Keputusan:** kode OTP **diturunkan** dari challenge ID: `HMAC(OTP_PEPPER, "otp-code"‖id) mod 10^n` (`adapter/signer/otpcodes.go`). Kode asli tetap tidak disimpan (hanya `code_hash`), tetapi dispatcher bisa membuatnya ulang saat mengirim, tanpa kolom terenkripsi. Ditambah config `wa_notif.send_interval` (1,5 s) untuk `estimated_wait_seconds = (antrean di depan + 1) × send_interval`.
**Verification:** [x] `go test ./... -race` (usecase dengan Postgres asli, adapter Turnstile dengan siteverify palsu, e2e HTTP); [x] test konkuren diulang 3× dengan `-race`
**Dependencies:** 8, 18, 19 · **Files:** `usecase/{otp_request,phone_limiter}.go`, `internal/ratelimit/` (diekstrak dari middleware, + `Undo`), `adapter/turnstile/verify.go`, `adapter/signer/otpcodes.go`, `handler/auth.go` + test · **Scope:** M

#### Task 21: Dispatcher OTP + status — **SELESAI**
**Description:** Worker tunggal (`FOR UPDATE SKIP LOCKED`), max 3 percobaan (3s, 10s), isi `sent_at`/`expires_at`, `GET /auth/otp/{id}`, `GET /auth/channel-status`.
**Acceptance criteria:**
- [x] Satu challenge tidak pernah dikirim berbarengan; bila worker mati di tengah kirim, challenge diantrekan ulang (setelah 3× timeout) dan **bisa** terkirim dua kali, tetapi dengan kode yang sama (diuji). *Diubah dari "hanya satu pesan": at-most-once membuat user tanpa kode bila proses mati setelah kirim, sedangkan kode turunan membuat duplikat tidak berbahaya.* Percobaan yang sudah habis → `failed` tanpa kirim; shutdown → kembali ke antrean, bukan kegagalan kanal
- [x] Gagal permanen terlihat user ≤ ±25 detik dengan `failure_reason` (manual: 14 detik); jatah nomor dikembalikan sehingga kirim ulang langsung boleh
- [x] `estimated_wait_seconds` mengikuti panjang antrean (T20)
- [x] Challenge diganti permintaan baru saat sedang dikirim → `superseded`, pesan berikutnya memuat kode challenge baru
- [x] Status nomor tak terdaftar ditiru (`queued` → `sent` → `expired`) agar polling tidak membuka pendaftaran; id tak dikenal/bukan UUID → `queued`
**Temuan & perbaikan:** test menunjukkan 3× `SEND_FAILED` untuk satu nomor menutup kanal untuk semua pengguna (DoS murah: daftarkan nomor tanpa WhatsApp). Kini hanya `WA_UNAVAILABLE` yang dihitung untuk status kanal.
**Verification:** [x] `go test ./... -race` (dispatcher terhadap stub + Postgres asli, diulang 3×); [x] manual (`cmd/api` mode otp + `cmd/wanotif-stub`, schema sementara): register → `sent` ±13 ms; stub dimatikan → `failed WA_UNAVAILABLE` setelah 14 detik, `channel-status=false`, `otp/request` → `503 OTP_UNAVAILABLE`; stub hidup lagi → pulih ≤ 2 detik (probe), kirim ulang langsung terkirim; log tanpa kode OTP; shutdown bersih
**Dependencies:** 20 · **Files:** `usecase/{otp_dispatch,otp_status}.go`, `cmd/api/main.go` (`startOTP`), `handler/auth.go` + test · **Scope:** M

#### Task 22: Verify + session + Authenticator `otp` — **SELESAI**
**Description:** `POST /auth/otp/verify` (5 percobaan, `hmac.Equal`, sekali pakai, aktifkan user), session token 32 byte (hash SHA-256), `GET /auth/me`, `POST /auth/logout`, middleware Bearer.
**Acceptance criteria:**
- [x] OTP salah ×4 → `422 OTP_INVALID` dengan `attempts_left` 4→1, ke-5 → `429`, challenge dibatalkan (kode benar sesudahnya ditolak); kedaluwarsa/dipakai/tak dikenal → 410; belum terkirim/gagal kirim → 409; input bukan digit → 422 tanpa menghitung percobaan
- [x] 10 verifikasi benar berbarengan → tepat 1 sesi; 30 tebakan salah berbarengan → paling banyak 4 dinilai, sesudahnya kode benar ditolak
- [x] Token logout/kedaluwarsa/salah format → 401 (+`WWW-Authenticate`); token tidak pernah tersimpan polos (hanya SHA-256, diperiksa di DB)
- [x] `challenge_id` palsu untuk nomor tak terdaftar meniru challenge asli juga saat verify (409 → 422×4 → 429 → 410, status ikut `expired`)
- [x] Rute auth publik mengabaikan header `Authorization` (token basi tidak menghalangi login ulang); mode `none` → semua rute auth 404
**Temuan & perbaikan:** test konkuren menunjukkan percobaan ke-6+ yang memanggil `Expire` dapat mendahului `Consume` milik verifikasi benar sehingga tidak ada yang login; kini percobaan berlebih hanya ditolak.
**Verification:** [x] `go test ./... -race` (usecase dengan Postgres + stub `wa-notif`, diulang 5×; e2e HTTP register → kirim → verify → me → logout)
**Dependencies:** 21 · **Files:** `usecase/{otp_verify,session}.go`, `middleware/auth.go` (`RequireSession`), `handler/auth.go`, `httpx` (detail error, sesi) + test · **Scope:** M

#### Task 23: Integrasi `otp` ke upload/submission + admin-ready authz — **SELESAI**
**Description:** `owner_id` terisi dari principal; aturan akses spec §9.2 (pemilik/admin, edit pemilik saja, 404 untuk milik orang lain); Turnstile pada mode `none`.
**Acceptance criteria:**
- [x] User A tidak bisa membaca/edit/memakai foto user B (diuji end-to-end lewat HTTP dengan dua akun yang login sungguhan): baca/URL foto/edit → 404, memakai foto (terpakai maupun belum) → `422 PHOTO_NOT_FOUND` seperti foto yang tidak ada; tanpa sesi → 401; mode otp tidak menerbitkan/menerima `edit_token`
- [x] Mode `none` tetap berjalan seperti Checkpoint 3 (seluruh test e2e lama hijau)
- [x] Turnstile mode `none`: presign & submit tanpa/ salah token → 403; satu token cukup untuk 5 presign + 1 submit (siteverify dipanggil sekali), pemakaian ke-7 ditolak; terikat IP dan TTL; presign paralel memverifikasi sekali; kegagalan tidak disimpan
**Keputusan:** token Turnstile boleh dipakai ulang (lihat spec §9.4), karena token Cloudflare sekali pakai sedangkan satu submission butuh hingga 6 request. Header `X-Turnstile-Token` ditambahkan ke CORS. Bagian admin dari aturan akses dikerjakan di T24.
**Verification:** [x] `go test ./... -race`
**Dependencies:** 22 · **Files:** `router.go`, handler (owner dari sesi), `adapter/turnstile/reusable.go`, `middleware/turnstile.go` + test · **Scope:** M

### Checkpoint 5: Auth OTP
- [ ] **OTP nyata terkirim ke nomor Anda lewat `wa-notif`**; register → verify → upload → submit berhasil
- [ ] Matikan `wa-notif` → `channel-status=false` dan pesan gagal tampil; pulih otomatis
- [ ] Review dengan Anda

### Phase 6: Admin & export

#### Task 24: Admin login — **SELESAI**
**Description:** `POST /admin/login` (bcrypt dari config, rate limit 5/menit, token HMAC stateless 8 jam), admin boleh baca submission/foto, tidak boleh edit.
**Acceptance criteria:**
- [x] Kredensial salah → `401 INVALID_CREDENTIALS` dengan pesan identik (username/password salah, huruf besar, spasi, password > 72 byte); `admin.enabled=false` → 404 dan token admin tidak dikenali (401); rate limit login per IP
- [x] Token admin tidak bisa `PUT /submissions/{id}` (403 `ADMIN_READ_ONLY`, juga dengan `edit_token` sah), presign, submit, atau `/auth/me`; bisa membaca submission & URL foto siapa pun di kedua mode
- [x] Token: kedaluwarsa, secret lain, `exp` diubah/non-kanonik, tanda tangan rusak → ditolak
**Keputusan:** admin hanya-baca juga untuk presign/submit (matriks spec §9.2 diselaraskan; sebelumnya `user / admin`), karena submission tanpa pemilik tidak bisa diedit siapa pun. Satu middleware `Authenticator.Require(Access{Anonymous,User,Admin})` menggantikan `RequireSession`.
**Verification:** [x] `go test ./... -race`
**Dependencies:** 15, 8 · **Files:** `usecase/admin.go`, `adapter/signer/admintoken.go`, `middleware/auth.go`, `handler/admin.go` + test · **Scope:** S

#### Task 25: Export JSONL — **SELESAI**
**Description:** Port `Exporter` + implementasi `jsonl` (keyset pagination `(created_at,id)`, `bbox_norm` + `bbox_px`, `storage_key`, `sha256`, tanpa data pribadi, `include_urls`), deadline tulis diperpanjang.
**Acceptance criteria:**
- [x] Satu baris JSON per foto (5 kunci tetap); tidak ada nama/email/nomor/owner_id/IP (diuji dengan data yang memuat semuanya); `bbox_px` sama dengan contoh spec `[230,432,480,259]`; teks apa adanya (`<`, `&` tidak di-escape); urutan benar dengan halaman 1, 2, dan 500; pending/deleted tidak ikut; filter `[from,to)`
- [x] 10 ribu foto (+30 ribu anotasi, 17 MB output) diekspor dalam 21 halaman, tidak pernah lebih dari 500 baris sekaligus; heap hidup antar halaman selisih ±1 MB
- [x] `include_urls`: URL bertanda tangan 1 jam yang benar-benar mengunduh foto asli (e2e)
- [x] Hanya admin (tanpa token / `edit_token` → 401); query salah → 422; trailer `X-Export-Complete`/`X-Export-Rows`
**Verification:** [x] `go test ./... -race`; [x] e2e lewat server HTTP sungguhan (setara `curl`), termasuk membaca trailer
**Catatan:** ikut diperbaiki test `TestDispatchShutdownRequeues` yang flaky di bawah beban (timeout 50 ms bisa habis sebelum klaim; kini pembatalan dipicu setelah pengiriman dimulai, diulang 10× dengan -race).
**Dependencies:** 24 · **Files:** `usecase/export.go`, `adapter/postgres/export_repo.go`, `adapter/export/jsonl.go`, `handler/export.go` + test · **Scope:** M

### Checkpoint 6: Backend lengkap
- [ ] Setara milestone M1–M2 + M6 spec; `go test ./... -race` hijau
- [ ] Review dengan Anda

### Phase 7: Frontend (`web/`)

> Framework FE belum diputuskan (spec: React/Svelte + Vite). Rekomendasi: **React + Vite + TypeScript** — Annotorious punya dukungan React yang matang. Konfirmasi sebelum T26.

#### Task 26: FE skeleton + proses foto + upload
**Description:** Vite + TS, ambil foto (kamera belakang), resize ≤ 1920 px, WebP dengan fallback JPEG, turunkan kualitas bila > 2 MB, presign + PUT dengan progres dan retry/backoff (hormati `Retry-After`).
**Acceptance criteria:**
- [ ] Foto HP 4000×3000 menjadi ≤ 2 MB, sisi ≤ 1920, EXIF/GPS hilang
- [ ] Progres tampil; `503 UPLOAD_BUSY` di-retry, `STORAGE_FULL` menampilkan pesan
**Verification:** [ ] unit test fungsi resize; [ ] manual di HP (Android Chrome + iOS Safari)
**Dependencies:** Checkpoint 3 · **Files:** `web/package.json`, `web/src/lib/image.ts`, `web/src/lib/api.ts`, `web/src/pages/Upload.tsx` · **Scope:** M

#### Task 27: FE anotasi + submit
**Description:** Annotorious (kotak + teks wajib), validasi koordinat sama dengan backend, submit dengan `client_request_id`, simpan `edit_token` (mode `none`).
**Acceptance criteria:**
- [ ] Kotak < 0.005 atau tanpa teks tidak bisa dikirim; Submit nonaktif sampai semua upload selesai
- [ ] Double-tap Submit tidak membuat duplikat
**Verification:** [ ] test validasi; [ ] manual end-to-end di HP terhadap Pi
**Dependencies:** 26 · **Files:** `web/src/pages/Annotate.tsx`, `web/src/lib/annotation.ts`, test · **Scope:** M

#### Task 28: FE edit + draft
**Description:** Muat submission, ubah anotasi/foto, `version`, sisa waktu edit dari `editable_until` (baca-saja setelahnya), draft di `localStorage`, tangani `409`.
**Acceptance criteria:**
- [ ] Setelah 24 jam layar baca-saja; `409` menawarkan muat ulang; draft pulih setelah reload
**Verification:** [ ] manual; [ ] test reducer draft
**Dependencies:** 27, 16 · **Files:** `web/src/pages/Edit.tsx`, `web/src/lib/draft.ts` · **Scope:** M

#### Task 29: FE registrasi/login OTP
**Description:** Layar nama/email/HP, layar OTP dengan hitung mundur kirim ulang, polling status, banner `channel-status`, pesan gagal WhatsApp, token di `localStorage`, redirect saat 401.
**Acceptance criteria:**
- [ ] `failed`/`otp_available=false` menampilkan pesan jelas dan tombol kirim ulang aktif langsung
- [ ] Input OTP: `autocomplete="one-time-code"`, `inputmode="numeric"`
**Verification:** [ ] manual dengan OTP nyata (Checkpoint 5)
**Dependencies:** 22, 27 · **Files:** `web/src/pages/{Register,Otp}.tsx`, `web/src/lib/auth.ts` · **Scope:** M

#### Task 30: Deploy FE ke Cloudflare Pages
**Description:** Build produksi, deploy, CSP ketat, `VITE_API_BASE_URL`, cek CORS + Cloudflare Tunnel ke API.
**Acceptance criteria:**
- [ ] FE dari CDN memanggil API lewat Tunnel; preflight upload lolos; tidak ada file FE yang di-serve dari Pi
**Verification:** [ ] buka dari HP lewat jaringan seluler, jalankan alur lengkap
**Dependencies:** 29, 31 · **Files:** `web/public/_headers`, konfigurasi Pages · **Scope:** S

### Checkpoint 7: Aplikasi utuh
- [ ] Alur lengkap dari HP (seluler) berhasil, termasuk edit dan OTP
- [ ] Review dengan Anda

### Phase 8: Hardening & go-live

#### Task 31: Dockerfile + Compose (`api` + `cloudflared`) — **SELESAI (kecuali Tunnel)**
**Description:** Image arm64 multi-stage, compose dengan network `shared-db` & `wa-notif_default`, bind mount SSD `/data/photos`, `ulimits nofile 65536`, config read-only.
**Acceptance criteria:**
- [x] `docker compose up -d` di Pi: `migrate` exit 0 lalu `api` `healthy`, `/healthz` dan `/readyz` 200; tanpa penanda SSD (direktori kosong) → API menolak start (`STORAGE_UNAVAILABLE … SSD belum ter-mount?`, exit 1)
- [x] Container non-root (UID 65532, rootfs read-only, `cap_drop ALL`, nofile 65536) menulis ke `/data/photos`: 20 upload lewat SSH tunnel berhasil, file `65532:65532 0640` di SSD
- [ ] ⏳ `curl https://<api>/healthz` lewat Tunnel: butuh domain + `TUNNEL_TOKEN` (profile `tunnel` sudah disiapkan)
**Kondisi deploy saat ini di Pi:** `auth.mode=none`, Turnstile & admin mati (secret/password belum ada), `public_base_url` sementara `http://127.0.0.1:8080`, port hanya localhost. Diubah saat domain, `WA_NOTIF_TOKEN`, `TURNSTILE_SECRET`, dan password admin tersedia.
**Temuan:** `cgroup_disable=memory` di kernel Pi → batas memori container tidak berlaku (lihat spec §11.2).
**Verification:** [x] `docker compose ps`; [x] uji penanda; [x] uji tulis UID 65532; [ ] Tunnel
**Dependencies:** Checkpoint 6 · **Files:** `Dockerfile`, `docker-compose.yml`, `.dockerignore`, `cmd/healthcheck` · **Scope:** S

#### Task 32: Observability — **SELESAI** (kecuali tujuan notifikasi alert)
**Sudah:** field log (`user_id`, `admin`, `code`), baris `stats` per menit (foto pending, slot upload, ruang SSD, antrean & hitungan OTP 24 jam vs `daily_global_cap`), healthz di level debug, test e2e kebersihan log (alur OTP → upload → submit → edit → baca → admin → export → permintaan gagal; tidak ada OTP, token, `Bearer`, `sig=`, nomor utuh, nama, email, password, teks anotasi). Diverifikasi di Pi: baris `stats` muncul, log tidak lagi dibanjiri healthcheck.
**Stack (keputusan Anda: di folder docker Pi, data di SSD):** Alloy + Loki + Prometheus + Grafana di `/home/myrpi/docker/observability`, data di `/mnt/ssd/observability`, dipakai semua aplikasi. Terverifikasi: log 5 project + kernel masuk Loki, metrik suhu/SSD/eth0/disk masuk Prometheus, 13 alert rule sehat, dashboard ter-provision; uji picu: API dimatikan → alert "API tidak mengirim stats" pending lalu kembali inactive setelah API hidup (`for` dipendekkan ke 1m). RAM Pi terpakai total ±1,2 GB.
**Belum:** tujuan notifikasi alert (contact point) — belum Anda tentukan.
**Description:** Field log lengkap (tanpa data sensitif), Promtail → Loki, dashboard Grafana (spec §12), alert: error rate, suhu, SSD > 80%, mount hilang, antrean OTP, kegagalan `wa-notif`.
**Acceptance criteria:**
- [x] Setiap alert di spec §12 punya rule (13, sehat); dipicu manual: API mati
- [x] `grep` log tidak menemukan OTP, `edit_token`, header Authorization, atau nomor HP utuh (test otomatis)
**Verification:** [ ] picu tiap alert; [ ] `grep` log
**Dependencies:** 31 · **Scope:** M

#### Task 33: Backup + uji restore
**Description:** `pg_dump` harian per database (retensi 14 hari) dan `rsync` foto inkremental ke tujuan yang Anda tentukan (open question #3); uji restore sekali.
**Acceptance criteria:**
- [ ] Restore ke database kosong menghasilkan data identik; salinan foto cocok `sha256`
**Verification:** [ ] jalankan restore end-to-end
**Dependencies:** 31 · **Butuh:** keputusan tujuan backup · **Scope:** S

#### Task 34: Load test
**Description:** k6 dari luar jaringan rumah dengan stub `wa-notif`: 1000 VU, jeda 25–40 detik (±30 foto/s), uji klien lambat 200 koneksi, export saat beban, burst submit. **Jangan** menyentuh `wa-notif` asli.
**Acceptance criteria:**
- [ ] Target spec §10 terpenuhi; tanpa thermal throttling; CPU `cloudflared`+API tidak jenuh; iowait SSD rendah; koneksi DB tidak habis
- [ ] Hasil dan tuning dicatat di `docs/`
**Verification:** [ ] laporan k6 + metrik Pi
**Dependencies:** 31, 32 · **Files:** `loadtest/*.js` · **Scope:** M

### Checkpoint 8: Go-live
- [ ] Semua acceptance criteria terpenuhi; backup teruji; alert aktif
- [ ] Open questions tersisa ditinjau; siap dipakai

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| SSD tersambung ke USB 2.0 / terlepas | High | T2 mengukur kecepatan; `require_marker` mencegah tulis ke SD card; alert mount hilang |
| Nomor WhatsApp pengirim diblokir akibat OTP spam | High | Turnstile, cooldown, limit per IP/nomor, batas harian, stub untuk load test (spec §9.5); T4 menguji lebih awal |
| `wa-notif` mengirim satu pesan per 1,5 detik (≈40 OTP/menit) | Med | Antrean + estimasi tunggu; OTP dihitung sejak terkirim |
| Bandwidth ISP (±187 Mbps) membatasi upload | Med | Target 30 foto/s; load test disesuaikan; adapter S3/B2 bisa ditambah di balik `PhotoStore` |
| CPU Pi 4 jenuh (`cloudflared`, SHA-256) | Med | Ukur di T34; opsi turunkan `max_concurrent_uploads`/hitung hash asinkron |
| Data hilang (satu SSD, DB di SD card) | High | T33 backup + uji restore sebelum go-live |
| Slot upload habis oleh klien lambat | Med | `max_concurrent_uploads` 200, timeout 60 detik, uji klien lambat |
| Nomor tanpa WhatsApp: perilaku tidak diketahui | Low | T4 menguji; fallback: user minta kirim ulang |

## Open Questions

- Framework FE (React/Svelte)? Rekomendasi React + Vite + TS.
- Dataset untuk model vs retensi 90 hari (spec §14.2 #1).
- Tujuan backup DB dan foto (spec §14.2 #3).
- Perlu endpoint cek nomor di `wa-notif` (tergantung hasil T4)?
- Domain publik untuk API dan FE (`public_base_url`, `allowed_origins`) belum ditentukan.
