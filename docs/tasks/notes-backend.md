# Catatan backend: label terkontrol & dashboard superadmin

Catatan per task backend dari [spec-labels-admin.md](../spec-labels-admin.md) §12 (branch `feat/labels-backend`).
Ditulis terpisah dari `plan.md`/`todo.md` agar tidak bentrok dengan pekerjaan frontend; digabung saat merge.

## Penyimpangan kontrak

Hal yang berbeda dari spec §5 (FE perlu menyesuaikan saat merge). Bila kosong, kontrak diikuti persis.

Tidak ada penyimpangan dari bentuk JSON spec. Detail yang spec tidak sebutkan dan dipilih backend
(FE perlu mengikutinya):

- `description` label yang kosong dikirim sebagai `null` (bukan `""`), di `GET /labels` maupun `/admin/labels`.
- `PUT /admin/labels/order` membalas `200` dengan bentuk yang sama dengan `GET /admin/labels` (`{ "labels": [...] }`).
- `PUT /admin/labels/{id}` wajib membawa `color`; tanpa `color` → `422 INVALID_LABEL_COLOR`. `archived` yang tidak
  dikirim dianggap `false` (replace), jadi FE harus selalu mengirim nilai `archived` saat mengubah label arsip.
- Warna huruf besar (`#E4572E`) diterima dan disimpan huruf kecil.
- `GET /admin/labels` diurutkan: label aktif menurut `sort_order`, lalu label arsip.

## Perubahan config untuk deploy

- `admin.username` dan `admin.password_bcrypt` **dihapus**; bila masih ada, API menolak start dengan pesan
  "login admin kini lewat OTP; hapus field ini dan isi admin.superadmin.phone".
- `admin.superadmin: { "name": "...", "phone": "08…" }` wajib bila `admin.enabled`.
- `admin.otp_message_template` opsional (default memuat kata ADMIN; wajib berisi `{code}`).
- `limits.max_labels` (opsional, default 200).
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

#### Task 38: Label + kelola label admin — **SELESAI**
**Hasil:** migrasi `0007_labels` (tabel `labels` dengan id identity, nama unik `lower(name)`, CHECK warna/panjang;
`annotations.label_id` FK `ON DELETE RESTRICT`, `text` nullable, CHECK `annotations_label_or_text`, indeks
`annotations(label_id)`; `Down` menolak bila sudah ada anotasi berlabel). `domain.Label` + normalisasi nama/deskripsi/
warna, palet bawaan 12 warna. Endpoint `GET /labels` (akses sama dengan presign, token admin boleh, `Cache-Control:
private, max-age=60`), `GET/POST /admin/labels`, `PUT /admin/labels/{id}`, `PUT /admin/labels/order`,
`DELETE /admin/labels/{id}`. CORS menerima `DELETE`. Config `limits.max_labels`.
**Keputusan:**
- Semua penulisan label berjalan dalam transaksi yang memegang `pg_advisory_xact_lock` khusus label, sehingga
  batas 200 label aktif, `sort_order` label baru, dan urutan tidak bisa rusak oleh dua tab admin berbarengan (diuji 8
  create paralel dengan batas 3).
- Warna bawaan dipilih bergiliran dari palet berdasarkan jumlah label yang pernah ada (termasuk arsip).
- Label yang dibuka arsipnya diletakkan di urutan terakhir; mengarsipkan tidak mengubah `sort_order`.
- Hapus memeriksa pemakaian di **semua** anotasi (termasuk foto `deleted` yang belum dibersihkan) lalu FK RESTRICT
  sebagai pagar terakhir. `usage_count` di daftar hanya menghitung foto `attached`.
- `{id}` yang bukan bilangan bulat positif → `404` (sama dengan id yang tidak ada).
- Log `admin action` untuk `label.create/update/archive/unarchive/reorder/delete` dengan id dan nama label.
**Temuan:** migrasi test (`pgtest`) memakai session locker goose yang mengunci **seluruh database**; paket test yang
berjalan paralel saling menunggu sampai melewati batas 30 detik (penyebab kegagalan acak di T37). `pgtest` kini
memakai `MigrateUpUnlocked` (schema uji sudah terisolasi); `cmd/migrate` tetap memakai locker. Suite penuh turun dari
±3 menit ke ±2 menit dan tidak lagi gagal acak.
**Verification:** [x] `go vet`, `go build ./cmd/...`, `go test ./... -race` (domain: normalisasi; repo: CHECK, unik
tanpa beda huruf, RESTRICT, Down ditolak bila terpakai; usecase: palet, batas, arsip/buka arsip, urutan basi/duplikat,
hapus terpakai + usage_count, id tidak dipakai ulang, create paralel; e2e: CRUD lengkap, 401 tanpa token/token
pengguna, mode otp, preflight DELETE, log hygiene mencakup kelola label)
