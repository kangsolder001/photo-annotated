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

## Perubahan 24 Okt: label terkontrol & dashboard superadmin ([spec-labels-admin.md](../spec-labels-admin.md))
- [x] **T37** Backend: superadmin di config, login admin via OTP (`/admin/otp/*`, `/admin/me`, token `adm2`), hapus `/admin/login`
- [x] **T38** Backend: tabel `labels`, `GET /labels`, CRUD + urutan `/admin/labels`
- [x] **T39** Backend: submit/edit/get/export memakai `label_id`; data teks lama tetap terbaca
- [x] **T40** Backend: `GET /admin/submissions` (filter, cursor, sampul) + detail
- [x] **T41** Frontend user: dropdown label per kotak, warna kotak, migrasi draft
- [x] **T42** Frontend admin: login OTP, layout (lazy chunk) responsif desktop + HP, halaman label
- [x] **T43** Frontend admin: daftar & detail anotasi + annotator, export
- [x] **T44** Backend: hapus permanen oleh admin (submission, foto, massal) dari DB + SSD, langkah cleanup baru
- [x] **T45** Frontend: tombol hapus + konfirmasi di dashboard; pesan "sudah dihapus" di sisi user
- [x] **T46** Backend: review submission (terima/tolak), reset saat edit user, retensi melewati yang diterima, export `review_status`
- [x] **T47** Backend: kelola annotator (daftar, blokir/buka blokir, hapus data)
- [x] **T48** Backend: statistik per label + status sistem
- [x] **T49** Frontend: review di dashboard + lencana review di sisi user
- [x] **T50** Frontend admin: halaman annotator
- [x] **T51** Frontend admin: beranda status sistem + statistik label
- [x] **T52** Frontend: tema terang hangat + gelap (pilihan Sistem/Terang/Gelap) untuk aplikasi user dan dashboard
- [x] **T53** Frontend: hapus emoji, ganti ikon Font Awesome Free (dipakai semua task FE berikutnya)
- [x] **T54** Hapus akun annotator (`DELETE /admin/users/{id}`, akun + semua data; nomor bisa daftar ulang) + tombol di profil annotator (permintaan Anda, 25 Okt)
- [x] **T55** FE user: konfirmasi sebelum Keluar (dialog dipakai bersama user + dashboard) (permintaan Anda, 25 Okt)
- [x] **T56** FE user: tombol Ambil foto / Pilih dari galeri jadi dua ubin besar selebar layar (permintaan Anda, 25 Okt)

### Checkpoint 9: Label & dashboard admin
- [x] Deploy backend + FE bersamaan ke Pi (25 Okt, `a939b30`)
- [ ] Uji manual: login admin ke nomor asli; buat label; anotasi dari HP; lihat di dashboard; review; blokir & buka blokir akun uji; hapus submission uji dan pastikan filenya hilang dari SSD; cek status sistem dan statistik; cek dashboard di HP dan desktop; cek kedua tema; tidak ada emoji
- [ ] Review dengan Anda

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
