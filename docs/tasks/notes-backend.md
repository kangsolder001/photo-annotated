# Catatan backend: label terkontrol & dashboard superadmin

Catatan per task backend dari [spec-labels-admin.md](../spec-labels-admin.md) §12 (branch `feat/labels-backend`).
Ditulis terpisah dari `plan.md`/`todo.md` agar tidak bentrok dengan pekerjaan frontend; digabung saat merge.

## Penyimpangan kontrak

Hal yang berbeda dari spec §5 (FE perlu menyesuaikan saat merge). Bila kosong, kontrak diikuti persis.

_(lihat tiap task; ringkasan diperbarui di akhir)_

## Perubahan config untuk deploy

- `admin.username` dan `admin.password_bcrypt` **dihapus**; bila masih ada, API menolak start dengan pesan
  "login admin kini lewat OTP; hapus field ini dan isi admin.superadmin.phone".
- `admin.superadmin: { "name": "...", "phone": "08…" }` wajib bila `admin.enabled`.
- `admin.otp_message_template` opsional (default memuat kata ADMIN; wajib berisi `{code}`).
- `admin.enabled = true` kini mewajibkan `wa_notif.base_url`, `secrets.wa_notif_token`, `secrets.otp_pepper`,
  `secrets.admin_token_secret` di **semua** `auth.mode` (juga `none`).

---

#### Task 37: Superadmin + login OTP admin — **SELESAI**
**Hasil:** migrasi `0006_admin_otp` (`otp_challenges.purpose` + `admin_login`, `user_id` nullable dengan CHECK
`(purpose = 'admin_login') = (user_id IS NULL)`, CHECK data registrasi diperluas, indeks unik challenge aktif
per `(phone, purpose = 'admin_login')`). Config `admin.superadmin {name, phone}` dinormalisasi saat load,
`admin.otp_message_template`, field lama ditolak dengan pesan jelas. Endpoint `POST /admin/otp/request`,
`GET /admin/otp/{id}`, `POST /admin/otp/resend`, `POST /admin/otp/verify`, `GET /admin/me`,
`GET /admin/channel-status`; `POST /admin/login` dihapus (404). Token `adm2` mengikat nomor superadmin;
`adm1` ditolak. Dispatcher + probe wa-notif kini juga berjalan di `auth.mode = none` bila admin aktif.
**Keputusan:**
- Inti antrean OTP (`admit`/`issue`/`decoy`/`status`) dipisah ke `otpCore` yang dipakai `OTPRequest` (pengguna)
  dan `AdminLogin`; satu repo/antrean/dispatcher/limiter per nomor. Catatan challenge palsu (decoy) terpisah
  per jenis, sama seperti challenge asli yang tidak saling membatalkan.
- Pemeriksaan kode (status, kedaluwarsa, hitung percobaan atomik, HMAC) diekstrak ke `checkCode` dan dipakai
  bersama verifikasi pengguna dan admin, sehingga aturan percobaan identik.
- Challenge admin tidak dikenali endpoint pengguna (`/auth/otp/verify` → `OTP_EXPIRED`, `/auth/otp/{id}` →
  `queued`, `/auth/otp/resend` → `OTP_EXPIRED`) dan sebaliknya; jadi kode admin tidak pernah menghasilkan sesi
  pengguna.
- `verify` admin memakai limiter per IP tersendiri dengan nilai `otp_verify` (tidak berbagi kuota dengan
  `/auth/otp/verify`). `request` + `resend` admin berbagi limiter `admin_login`.
- `expires_at` token dibulatkan ke detik (token menyimpan detik unix) agar respons verify dan `/admin/me` sama.
- Log: `msg="admin login"` per tahap (`stage=request` dengan nomor tersamar + `superadmin` true/false;
  `stage=verify` dengan `ok` dan `code` bila gagal). Disiapkan `logAdminAction` untuk baris `admin action`.
**Temuan:** test DB lewat SSH tunnel kadang gagal `pgtest: migrate up: context deadline exceeded` saat banyak
paket berjalan paralel (beban Pi); diulang lolos. Bukan masalah kode.
**Verification:** [x] `go vet ./...`, `go build ./cmd/...`, `go test ./... -race` (config: validasi superadmin,
field lama, mode none; signer: adm2, adm1 ditolak, ganti nomor membatalkan token; repo: challenge admin & pengguna
berdampingan + CHECK; usecase: alur, decoy, nomor diganti di tengah alur, resend, kanal mati; e2e: mode none &
otp, `/admin/login` 404, anti-enumerasi (respons, polling, verify, tanpa pesan), Turnstile, rate limit bersama,
admin vs pengguna terpisah, log hygiene mencakup login admin)
