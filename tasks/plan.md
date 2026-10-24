# Implementation Plan: Photo Annotated

Sumber: [docs/spec.md](../docs/spec.md) (Draft v0.5). Daftar centang ringkas ada di [tasks/todo.md](todo.md).

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

#### Task 4: Spike OTP via wa-notif (butuh input Anda)
**Description:** Kirim satu pesan lewat `POST /api/v1/messages/send` ke nomor Anda sendiri, dan satu ke nomor yang **tidak memakai WhatsApp**, untuk menjawab open question #2. Catat respons/error aktual.
**Acceptance criteria:**
- [ ] Pesan ke nomor sendiri diterima; waktu respons tercatat
- [ ] Perilaku untuk nomor tanpa WhatsApp tercatat (error code, `SENT` atau tidak) di spec §14.2
- [ ] Putusan: perlu endpoint cek nomor di `wa-notif` atau tidak
**Verification:** [ ] curl dari Pi; hasil ditulis ke `docs/spec.md` §9.5/§14.2
**Dependencies:** None · **Scope:** XS · **Butuh:** nomor tanpa WhatsApp dari Anda

### Phase 1: Foundation

#### Task 5: Skeleton proyek + config loader
**Description:** `go.mod`, struktur direktori Clean Architecture, loader `config.json` + override env dengan validasi fail-fast (spec §11.4, §9.1).
**Acceptance criteria:**
- [ ] Config tidak lengkap (mis. `otp` tanpa `OTP_PEPPER`, `local.root` tidak ada) → proses gagal start dengan pesan jelas
- [ ] Env menimpa nilai file; secret tidak pernah ada di `config.json`
- [ ] `config.json.example` sesuai spec §11.4
**Verification:** [ ] `go vet ./... && go test ./internal/config/... -race`; [ ] `go build ./cmd/...`
**Dependencies:** None · **Files:** `go.mod`, `internal/config/config.go`, `internal/config/config_test.go`, `config.json.example` · **Scope:** S

#### Task 6: HTTP server skeleton
**Description:** Gin (release mode, `gin.New()`), middleware `request_id`, slog JSON logger (tanpa data sensitif), recovery, trusted proxy + `CF-Connecting-IP`, format error JSON, `GET /healthz`, graceful shutdown. Timeout: `ReadHeaderTimeout 10s`, `IdleTimeout 60s`, deadline per-route lewat `ResponseController` (spec §11.3).
**Acceptance criteria:**
- [ ] `GET /healthz` → 200; setiap response punya `X-Request-ID`; log memuat field minimum spec §12
- [ ] Panic di handler → 500 berformat error, proses tetap hidup
- [ ] `client_ip` hanya dari `CF-Connecting-IP` bila request dari proxy tepercaya
**Verification:** [ ] `go test ./internal/delivery/... -race`; [ ] `curl -i localhost:8080/healthz`
**Dependencies:** 5 · **Files:** `cmd/api/main.go`, `internal/delivery/http/router.go`, `.../middleware/{requestid,logger,recovery}.go` · **Scope:** M

#### Task 7: Koneksi DB, tool migrasi, `/readyz`
**Description:** `pgxpool` (MaxConns dari config), `cmd/migrate` (golang-migrate atau goose), migrasi `0001` kosong/ekstensi bila perlu, `GET /readyz` memeriksa DB saja.
**Acceptance criteria:**
- [ ] `cmd/migrate up/down` berjalan terhadap `photo_annotated_test`
- [ ] `/readyz` 200 saat DB hidup, 503 saat DB mati
**Verification:** [ ] `TEST_DATABASE_URL=… go test ./internal/adapter/postgres/... -race`; [ ] `curl /readyz`
**Dependencies:** 3, 6 · **Files:** `cmd/migrate/main.go`, `internal/adapter/postgres/pool.go`, `migrations/0001_init.sql`, handler readyz · **Scope:** M

#### Task 8: Middleware batas (rate limit, CORS, body limit)
**Description:** Rate limiter per-IP in-memory (token bucket) yang dapat dikonfigurasi per route, CORS (`GET,POST,PUT,OPTIONS`; header `Content-Type, Authorization, X-Edit-Token`; preflight cache 1 jam), batas body JSON 64 KB (spec §9.4).
**Acceptance criteria:**
- [ ] Melebihi limit → `429 RATE_LIMITED` + `Retry-After`
- [ ] Preflight `OPTIONS /api/v1/uploads/x` dari origin yang diizinkan → 204 dengan header benar; origin lain ditolak
- [ ] Body JSON > 64 KB → 413; rute upload dikecualikan dari batas ini
**Verification:** [ ] `go test ./internal/delivery/http/middleware/... -race`
**Dependencies:** 6 · **Files:** `middleware/{ratelimit,cors,bodylimit}.go` + test · **Scope:** M

### Checkpoint 1: Foundation
- [ ] `go vet`, `go test ./... -race`, `go build ./cmd/...` bersih
- [ ] `/healthz` dan `/readyz` berjalan di mesin dev terhadap DB di Pi
- [ ] Review dengan Anda sebelum Phase 2

### Phase 2: Upload foto (mode `none`)

#### Task 9: Domain — aturan foto & anotasi
**Description:** Entity `Photo`, `Annotation`, status foto, error domain, validasi murni: tipe konten, ukuran ≤ 2 MB, sisi terpanjang ≤ 2048, bbox (spec §6.3), teks anotasi (1–1000 rune, tanpa karakter kontrol selain newline).
**Acceptance criteria:**
- [ ] Table-driven test mencakup batas (0, 0.005, 1, `x+w>1`, teks kosong/1001 rune/karakter kontrol)
- [ ] Tidak meng-import paket di luar stdlib
**Verification:** [ ] `go test ./internal/domain/... -race`
**Dependencies:** 5 · **Files:** `internal/domain/{photo,annotation,errors}.go` + test · **Scope:** S

#### Task 10: Adapter `localfs` (PhotoStore) + penjagaan mount
**Description:** Implementasi `Save` (tmp → fsync → rename → fsync dir), `Open`, `Stat`, `Delete` (idempotent), `FreeSpace`, memakai `os.Root`; cek `require_mount` dan `min_free_bytes` (spec §8).
**Acceptance criteria:**
- [ ] `storage_key` dengan `..`/symlink/absolut ditolak
- [ ] Penulisan terputus tidak meninggalkan file di lokasi akhir
- [ ] `require_mount=true` pada direktori yang bukan mount point → gagal start
- [ ] `Delete` file yang tidak ada → sukses
**Verification:** [ ] `go test ./internal/adapter/localfs/... -race` (`t.TempDir()`); [ ] di Pi: start dengan `/mnt/ssd/...` lolos, dengan direktori SD card gagal
**Dependencies:** 2, 5 · **Files:** `internal/adapter/localfs/{store,mountcheck}.go` + test, `usecase/ports.go` · **Scope:** M

#### Task 11: `POST /uploads/presign`
**Description:** Migrasi tabel `photos`, `PhotoRepo` postgres, `URLSigner` HMAC (pesan berawalan `upload`), usecase presign, handler. Menolak `503 STORAGE_FULL/UNAVAILABLE`.
**Acceptance criteria:**
- [ ] Respons sesuai spec §5.1 (URL absolut dari `public_base_url`, `exp`, `sig`)
- [ ] Validasi §5.1 (tipe, ukuran, dimensi) mengembalikan `422` dengan kode yang benar
- [ ] `storage_key` dibuat server `{yyyy}/{mm}/{dd}/{uuid}.{ext}`; baris `photos` berstatus `pending`
- [ ] Mengubah `size_bytes`/`content_type` membuat `sig` tidak valid
**Verification:** [ ] unit test signer + usecase (fake repo); [ ] integrasi repo ke `photo_annotated_test`; [ ] `curl -X POST …/presign`
**Dependencies:** 7, 8, 9, 10 · **Files:** `migrations/0002_photos.sql`, `adapter/postgres/photo_repo.go`, `adapter/signer/signer.go`, `usecase/presign.go`, `delivery/http/handler/upload.go` · **Scope:** M

#### Task 12: `PUT /uploads/{photo_id}` (streaming)
**Description:** Verifikasi `exp`/`sig`, slot upload (`max_concurrent_uploads`), stream ke `tmp` dengan buffer 32 KB + SHA-256, validasi magic bytes dan dimensi (`image.DecodeConfig`, WebP via `x/image/webp`), simpan, isi `uploaded_at` + `sha256`, respons 204. Deadline baca 60 detik per-route.
**Acceptance criteria:**
- [ ] Alur sukses: file ada di SSD, `sha256` benar, `uploaded_at` terisi
- [ ] `Content-Length` ≠ deklarasi / byte aktual ≠ deklarasi → `422 SIZE_MISMATCH`; file non-gambar atau dimensi salah → `422 INVALID_IMAGE` dan foto `rejected`
- [ ] `sig` salah → 403; `exp` lewat → 410; foto bukan `pending` → 409; slot penuh → `503 UPLOAD_BUSY` + `Retry-After`
- [ ] PUT ulang pada foto `pending` menimpa file secara atomik
- [ ] 200 upload paralel tidak menaikkan memori proses secara berarti (memori per upload konstan)
**Verification:** [ ] `go test ./internal/usecase/... ./internal/delivery/... -race`; [ ] skrip curl presign→PUT; [ ] di Pi: 400 KB ×100 file ≥ 30 file/s
**Dependencies:** 11 · **Files:** `usecase/upload.go`, `handler/upload_put.go`, `domain/imagecheck.go` + test · **Scope:** M

### Checkpoint 2: Upload
- [ ] Alur presign → PUT → file di SSD berjalan di Pi dengan SSD asli
- [ ] File palsu ditolak; mount SSD dicabut → presign `503 STORAGE_UNAVAILABLE`
- [ ] Review dengan Anda sebelum Phase 3

### Phase 3: Submission (mode `none`, `edit_token`)

#### Task 13: Persistensi submission + transaksi
**Description:** Migrasi `submissions` + `annotations` (CHECK constraint bbox & teks), `SubmissionRepo`, `AnnotationRepo`, `TxManager.WithTx`.
**Acceptance criteria:**
- [ ] CHECK constraint menolak bbox/teks tidak valid walau lolos aplikasi
- [ ] Rollback transaksi tidak meninggalkan baris setengah jadi
- [ ] `client_request_id` UNIQUE
**Verification:** [ ] `TEST_DATABASE_URL=… go test ./internal/adapter/postgres/... -race`
**Dependencies:** 7, 11 · **Files:** `migrations/0003_submissions.sql`, `adapter/postgres/{submission_repo,tx}.go` + test · **Scope:** M

#### Task 14: `POST /submissions` (idempotent) + `edit_token`
**Description:** Usecase submit: validasi §5.2, verifikasi foto ter-upload (`uploaded_at`, `Stat` ukuran), simpan dalam satu transaksi, foto → `attached`, `edit_token` HMAC stateless (spec §9.3), idempotensi `client_request_id`.
**Acceptance criteria:**
- [ ] Foto belum ter-upload → `422 PHOTO_NOT_UPLOADED`, foto tetap `pending`
- [ ] Request ulang dengan `client_request_id` sama → respons sama (termasuk `edit_token`), tanpa duplikat
- [ ] Foto yang sudah dipakai submission lain ditolak; 1–5 foto, ≤ 50 anotasi/foto
**Verification:** [ ] unit test usecase (fake port); [ ] integrasi + `curl`
**Dependencies:** 12, 13 · **Files:** `usecase/submit.go`, `adapter/signer/edittoken.go`, `handler/submission.go` + test · **Scope:** M

#### Task 15: Baca submission + lihat foto
**Description:** `GET /submissions/{id}` (dengan `version`, `editable_until`), `GET /photos/{id}/url`, `GET /photos/{id}/content` (`ServeContent`, header `nosniff`, deadline tulis diperpanjang). Otorisasi pemilik via `X-Edit-Token`; resource orang lain → 404.
**Acceptance criteria:**
- [ ] Tanpa/ salah `X-Edit-Token` → 404; `Range` request berfungsi; `sig` view kedaluwarsa → 410
- [ ] URL view tidak bisa dipakai sebagai URL upload (awalan tujuan berbeda)
**Verification:** [ ] `go test ./… -race`; [ ] `curl -r 0-99` pada konten foto
**Dependencies:** 14 · **Files:** `usecase/{get,photourl,photo_content}.go`, `handler/{submission_get,photo}.go` · **Scope:** M

#### Task 16: `PUT /submissions/{id}` (edit)
**Description:** Replace semantik, `version` optimistic locking, jendela edit 24 jam, foto yang dihapus → `deleted`, foto baru `pending` → `attached`.
**Acceptance criteria:**
- [ ] `version` basi → `409 VERSION_CONFLICT`; setelah 24 jam → `403 EDIT_WINDOW_CLOSED` (diuji dengan fake clock)
- [ ] Foto yang dihapus berstatus `deleted` dan jadi target cleanup
- [ ] Edit tidak memperpanjang jendela
**Verification:** [ ] `go test ./internal/usecase/... -race`; [ ] integrasi
**Dependencies:** 15 · **Files:** `usecase/edit.go`, `handler/submission_put.go` + test · **Scope:** M

### Checkpoint 3: Alur anonim end-to-end
- [ ] Skrip e2e (curl atau Go test): presign → PUT → submit → get → edit → lihat foto berhasil
- [ ] `go test ./... -race` hijau; tidak ada memory growth saat 200 upload paralel
- [ ] **Kontrak API stabil**: FE boleh mulai (Phase 7)
- [ ] Review dengan Anda

### Phase 4: Retensi

#### Task 17: Cleanup job
**Description:** Scheduler di proses API (interval dari config): foto yatim/`rejected`/`deleted`, retensi 90 hari, sapuan `tmp/*.part` > 1 jam dan file yatim > 24 jam, data auth (users `pending`, challenges, sessions). Objek dulu, baris kemudian.
**Acceptance criteria:**
- [ ] File yang gagal dihapus → baris dipertahankan dan dicoba ulang
- [ ] File < 24 jam tak pernah disapu; hasil putaran tercatat di log
- [ ] Diuji dengan fake clock dan `localfs` di direktori sementara
**Verification:** [ ] `go test ./internal/usecase/... ./internal/adapter/... -race`
**Dependencies:** 14, 16 · **Files:** `usecase/cleanup.go`, `adapter/postgres/cleanup_queries.go`, `cmd/api/scheduler.go` + test · **Scope:** M

### Checkpoint 4: Backend inti (mode `none`) selesai
- [ ] Setara milestone M1 spec; semua test hijau
- [ ] Demo end-to-end di Pi dengan SSD

### Phase 5: Auth OTP

#### Task 18: Domain nomor HP + skema auth
**Description:** Normalisasi nomor Indonesia (`^628[0-9]{8,12}$`, tolak `@`), entity `User/OTPChallenge/Session`, migrasi `users`, `otp_challenges`, `sessions`, tambah FK `owner_id` pada `submissions`/`photos`.
**Acceptance criteria:**
- [ ] `0812-3456-7890`, `+628…`, `628…` → bentuk kanonik sama; JID grup dan nomor non-Indonesia ditolak
- [ ] Migrasi up/down bersih
**Verification:** [ ] `go test ./internal/domain/... ./internal/adapter/postgres/... -race`
**Dependencies:** 13 · **Files:** `domain/{user,phone,otp}.go`, `migrations/0004_auth.sql`, repo + test · **Scope:** M

#### Task 19: Adapter `wanotif` + probe kanal + stub
**Description:** `OTPSender` HTTP client ke `wa-notif` (pemetaan error spec §9.5), probe `GET /api/v1/session/status` per 30 detik, status `otp_available`, dan **stub `wa-notif`** (jeda 1,5 detik) untuk test/load test.
**Acceptance criteria:**
- [ ] 503/timeout/401/500 dipetakan ke `failure_reason` yang benar; token tidak muncul di log
- [ ] 3 kegagalan berturut-turut atau probe tidak `connected` → `otp_available=false`; pulih saat berhasil
**Verification:** [ ] `go test ./internal/adapter/wanotif/... -race` (terhadap stub)
**Dependencies:** 4, 5 · **Files:** `adapter/wanotif/{client,probe}.go`, `cmd/wanotif-stub/main.go` + test · **Scope:** M

#### Task 20: `POST /auth/register` & `/auth/otp/request` (antrean)
**Description:** Buat user `pending`/pakai yang ada, challenge `queued` (kode HMAC dengan `OTP_PEPPER`), respons 202 seragam (anti-enumerasi), cooldown 60 detik (kecuali `failed`), 5/jam per nomor, `max_queue`, `daily_global_cap`, Turnstile, `503 OTP_UNAVAILABLE`.
**Acceptance criteria:**
- [ ] Nomor terdaftar dan tidak terdaftar menghasilkan respons berbentuk sama
- [ ] Batas per nomor/antrean/harian ditegakkan; request baru membatalkan challenge lama
- [ ] `name`/`email` tidak menimpa user aktif
**Verification:** [ ] `go test ./internal/usecase/... -race`
**Dependencies:** 8, 18, 19 · **Files:** `usecase/{register,otp_request}.go`, `adapter/turnstile/verify.go`, handler + test · **Scope:** M

#### Task 21: Dispatcher OTP + status
**Description:** Worker tunggal (`FOR UPDATE SKIP LOCKED`), max 3 percobaan (3s, 10s), isi `sent_at`/`expires_at`, `GET /auth/otp/{id}`, `GET /auth/channel-status`.
**Acceptance criteria:**
- [ ] Hanya satu pesan terkirim per challenge walau worker restart di tengah
- [ ] Gagal permanen terlihat user ≤ ±25 detik dengan `failure_reason`
- [ ] `estimated_wait_seconds` mengikuti panjang antrean
**Verification:** [ ] `go test ./internal/usecase/... -race` (terhadap stub); [ ] manual: matikan stub → status `failed`
**Dependencies:** 20 · **Files:** `usecase/otp_dispatch.go`, `cmd/api/dispatcher.go`, handler status + test · **Scope:** M

#### Task 22: Verify + session + Authenticator `otp`
**Description:** `POST /auth/otp/verify` (5 percobaan, `hmac.Equal`, sekali pakai, aktifkan user), session token 32 byte (hash SHA-256), `GET /auth/me`, `POST /auth/logout`, middleware Bearer.
**Acceptance criteria:**
- [ ] OTP salah ×5 → `429`, challenge dibatalkan; kedaluwarsa → 410; belum terkirim → 409
- [ ] Token logout/kedaluwarsa → 401; token tidak pernah tersimpan polos
**Verification:** [ ] `go test ./… -race`
**Dependencies:** 21 · **Files:** `usecase/{otp_verify,session}.go`, `adapter/auth/otp.go`, `middleware/auth.go`, handler + test · **Scope:** M

#### Task 23: Integrasi `otp` ke upload/submission + admin-ready authz
**Description:** `owner_id` terisi dari principal; aturan akses spec §9.2 (pemilik/admin, edit pemilik saja, 404 untuk milik orang lain); Turnstile pada mode `none`.
**Acceptance criteria:**
- [ ] User A tidak bisa membaca/edit/memakai foto user B (diuji)
- [ ] Mode `none` tetap berjalan seperti Checkpoint 3 (regresi diuji)
**Verification:** [ ] `go test ./… -race`
**Dependencies:** 22 · **Files:** `usecase/authz.go`, perubahan usecase presign/submit/edit/get + test · **Scope:** M

### Checkpoint 5: Auth OTP
- [ ] **OTP nyata terkirim ke nomor Anda lewat `wa-notif`**; register → verify → upload → submit berhasil
- [ ] Matikan `wa-notif` → `channel-status=false` dan pesan gagal tampil; pulih otomatis
- [ ] Review dengan Anda

### Phase 6: Admin & export

#### Task 24: Admin login
**Description:** `POST /admin/login` (bcrypt dari config, rate limit 5/menit, token HMAC stateless 8 jam), admin boleh baca submission/foto, tidak boleh edit.
**Acceptance criteria:**
- [ ] Kredensial salah → 401 pesan seragam; `admin.enabled=false` → 404
- [ ] Token admin tidak bisa `PUT /submissions/{id}`
**Verification:** [ ] `go test ./… -race`
**Dependencies:** 15, 8 · **Files:** `usecase/admin.go`, `adapter/auth/admin.go`, handler + test · **Scope:** S

#### Task 25: Export JSONL
**Description:** Port `Exporter` + implementasi `jsonl` (keyset pagination `(created_at,id)`, `bbox_norm` + `bbox_px`, `storage_key`, `sha256`, tanpa data pribadi, `include_urls`), deadline tulis diperpanjang.
**Acceptance criteria:**
- [ ] Satu baris JSON per foto; tidak ada nama/email/nomor/owner_id
- [ ] 10 ribu foto diekspor dengan memori stabil
**Verification:** [ ] `go test ./… -race`; [ ] `curl -H "Authorization: Bearer …" /admin/export | head`
**Dependencies:** 24 · **Files:** `usecase/export.go`, `adapter/export/jsonl.go`, handler + test · **Scope:** M

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

#### Task 31: Dockerfile + Compose (`api` + `cloudflared`)
**Description:** Image arm64 multi-stage, compose dengan network `shared-db` & `wa-notif_default`, bind mount SSD `/data/photos`, `ulimits nofile 65536`, config read-only.
**Acceptance criteria:**
- [ ] `docker compose up -d` di Pi: API sehat; SSD dilepas → API menolak start (`require_mount`)
- [ ] Container non-root (UID 65532) dapat menulis ke `/data/photos`
**Verification:** [ ] `docker compose ps`; [ ] `curl https://<api>/healthz` lewat Tunnel
**Dependencies:** Checkpoint 6 · **Files:** `Dockerfile`, `docker-compose.yml`, `.dockerignore` · **Scope:** S

#### Task 32: Observability
**Description:** Field log lengkap (tanpa data sensitif), Promtail → Loki, dashboard Grafana (spec §12), alert: error rate, suhu, SSD > 80%, mount hilang, antrean OTP, kegagalan `wa-notif`.
**Acceptance criteria:**
- [ ] Setiap alert di spec §12 punya rule yang teruji (picu manual)
- [ ] `grep` log tidak menemukan OTP, `edit_token`, header Authorization, atau nomor HP utuh
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
| SSD tersambung ke USB 2.0 / terlepas | High | T2 mengukur kecepatan; `require_mount` mencegah tulis ke SD card; alert mount hilang |
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
