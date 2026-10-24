# Todo: Photo Annotated

Detail tiap task (deskripsi, acceptance criteria, verifikasi, dependensi, file) ada di [plan.md](plan.md). Spesifikasi: [spec.md](../spec.md).

Legenda: `[x]` selesai · `[ ]` belum · 🔒 butuh input/keputusan Anda · ⚠️ risiko tinggi

## Phase 0: Infrastruktur
- [x] **T1** PostgreSQL 17 shared berjalan di Pi (`/home/myrpi/docker/postgres`)
- [x] **T2** SSD ext4 ter-mount di USB 3 (211 MB/s tulis, 127 file/s fsync)
- [x] **T3** Database `photo_annotated` + `photo_annotated_test` + file secret env di Pi
- [x] **T4** Spike OTP: nomor sendiri terkirim; nomor tanpa WhatsApp → `SEND_FAILED` (tanpa endpoint cek)

## Phase 1: Foundation
- [x] **T5** Skeleton proyek + config loader (fail-fast)
- [x] **T6** HTTP server skeleton: Gin, middleware, `/healthz`
- [x] **T7** Koneksi DB + tool migrasi + `/readyz`
- [x] **T8** Middleware: rate limit, CORS, body limit

### Checkpoint 1: Foundation
- [x] `go vet`, `go test ./... -race`, `go build ./cmd/...` bersih
- [x] `/healthz` dan `/readyz` jalan terhadap DB di Pi
- [ ] Review dengan Anda

## Phase 2: Upload foto (mode `none`)
- [x] **T9** Domain: aturan foto & anotasi
- [x] **T10** ⚠️ Adapter `localfs` (PhotoStore) + penjagaan mount
- [x] **T11** `POST /uploads/presign`
- [x] **T12** ⚠️ `PUT /uploads/{photo_id}` (streaming + validasi isi)

### Checkpoint 2: Upload
- [x] presign → PUT → file di SSD berjalan di Pi (±100 foto/s, 16 paralel)
- [x] File palsu ditolak; SSD dilepas → `503 STORAGE_UNAVAILABLE`
- [ ] Review dengan Anda

## Phase 3: Submission (mode `none`)
- [x] **T13** Persistensi submission + annotation + transaksi
- [x] **T14** `POST /submissions` (idempoten) + `edit_token`
- [x] **T15** `GET /submissions/{id}` + URL/konten foto
- [x] **T16** `PUT /submissions/{id}` (edit, versi, jendela 24 jam)

### Checkpoint 3: Alur anonim end-to-end
- [x] e2e: presign → PUT → submit → replay → get → lihat foto → edit (Go, HTTP asli) dan `curl` di Pi
- [x] `go test ./... -race` hijau; memori stabil saat 200 upload paralel (heap +12–15 MB)
- [x] Kontrak API stabil → FE boleh mulai
- [ ] Review dengan Anda

## Phase 4: Retensi
- [x] **T17** Cleanup job (foto yatim, retensi 90 hari, file yatim, data auth)

### Checkpoint 4: Backend inti (mode `none`) selesai
- [x] Semua test hijau; demo end-to-end di Pi dengan SSD (upload, submit, baca, edit, cleanup)

## Phase 5: Auth OTP
- [x] **T18** Domain nomor HP + skema users/otp/sessions
- [x] **T19** Adapter `wanotif` + probe kanal + stub `wa-notif`
- [x] **T20** `POST /auth/register` & `/auth/otp/request` (antrean, batas)
- [x] **T21** Dispatcher OTP + `GET /auth/otp/{id}` + `channel-status`
- [x] **T22** `POST /auth/otp/verify` + session + `me`/`logout` + middleware
- [x] **T23** Integrasi `otp` ke upload/submission (owner authz) + Turnstile

### Checkpoint 5: Auth OTP
- [x] OTP nyata terkirim ke nomor Anda; register → verify → upload → submit berhasil (di Pi, 24 Okt; logout → 401)
- [x] `wa-notif` dimatikan → `channel-status=false`, pesan gagal tampil, pulih otomatis (diuji dengan stub di T21; `wa-notif` asli tidak dimatikan)
- [ ] Review dengan Anda

## Phase 6: Admin & export
- [x] **T24** Admin login (bcrypt + token HMAC)
- [x] **T25** `GET /admin/export` JSONL

### Checkpoint 6: Backend lengkap
- [x] `go test ./... -race` hijau; review backend di komputer lokal (agent reviewer independen + govulncheck), 24 Okt
  - Diperbaiki: CORS menolak `X-Turnstile-Token` (kritis), PUT ulang bisa menimpa foto yang sudah dipakai, enumerasi nomor lewat permintaan kode dua kali, cleanup pending user menghapus challenge yang sedang dipakai, Go 1.26.6 → 1.26.10 + quic-go v0.59.1 (govulncheck: 0 terjangkau), `/content` memeriksa tanda tangan sebelum DB
  - Dicatat (opsional, belum diubah): `client_request_id` di mode none berfungsi seperti kredensial (FE wajib `crypto.randomUUID()`), nomor bisa "diduduki" dengan nama/email penyerang sebelum pemilik mendaftar, validasi silang config (`pending_photo_ttl`, `edit.window`)

## Phase 7: Frontend (`web/`) — paralel setelah Checkpoint 3
- [x] Framework FE: React + Vite + TypeScript (keputusan Anda, 24 Okt)
- [x] **T26** FE: skeleton + proses foto + upload dengan progres
- [x] **T27** FE: anotasi (Annotorious) + submit
- [x] **T28** FE: edit + draft + sisa waktu edit
- [x] **T29** FE: registrasi/login OTP + banner kanal
- [x] **T30** Publik di https://cam.zipzip.my.id (cloudflared → nginx `web` :8081 → API); LAN http://192.168.1.222:8081

## Perubahan 24 Okt: login dengan password (permintaan Anda)
- [x] **T35** Backend: registrasi dengan password, login nomor HP + password, lupa password via OTP, kirim ulang kode
- [x] **T36** Frontend: form daftar dengan password, login password, lupa password

### Checkpoint 7: Aplikasi utuh
- [ ] Alur lengkap dari HP lewat jaringan seluler (termasuk edit dan OTP)
- [ ] Review dengan Anda

## Phase 8: Hardening & go-live
- [x] **T31** Dockerfile + Compose (`api` + `cloudflared`) — Tunnel menunggu domain/token
- [x] **T32** Observability: Alloy/Loki/Prometheus/Grafana di Pi (data di SSD), 13 alert — tujuan notifikasi belum diatur
- [ ] **T33** ⏸ Backup DB + foto + uji restore — ditunda (keputusan Anda, 24 Okt)
- [ ] **T34** Load test k6 (stub `wa-notif`, ±30 foto/s, klien lambat)

### Checkpoint 8: Go-live
- [ ] Semua acceptance criteria terpenuhi; backup teruji; alert aktif
- [ ] Open questions tersisa ditinjau
