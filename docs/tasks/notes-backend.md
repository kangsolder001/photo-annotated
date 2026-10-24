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
- `422 INVALID_LABEL` / `LABEL_ARCHIVED` dari submit/edit membawa `details: { "label_id", "photo_index",
  "annotation_index" }` (tambahan) agar FE bisa langsung menandai kotak yang perlu dipilih ulang. Pesan juga memuat
  lokasi `photos[i].annotations[j]` seperti error validasi lain.
- `label_id` yang bukan bilangan bulat (mis. `"1"`) → `400 INVALID_JSON`; `label_id` tidak dikirim / `0` →
  `422 INVALID_LABEL`.
- `GET /admin/submissions`: `next_cursor` selalu ada (`null` di halaman terakhir). Cursor mengikat arah `sort`;
  memakai cursor dari `sort` lain → `422 INVALID_QUERY`. `cover.annotations[].label_id` = `null` untuk kotak teks
  lama. `labels` diurutkan jumlah terbanyak dulu. `cover` = `null` hanya bila tidak ada foto (tidak terjadi untuk
  item yang tampil).
- `GET /admin/submissions/{id}` menambah `url_expires_at` (kapan URL foto di respons kedaluwarsa) di samping field
  spec; bentuk lain sama dengan `GET /submissions/{id}` (+ `annotator`, `review`, `retention_deletes_at`, per foto
  `url` + `sha256` hex).
- Hapus massal `POST /admin/submissions/delete`: daftar kosong, > 50, duplikat, atau UUID nol → `422 INVALID_IDS`
  (kode tidak disebut spec); id yang bukan UUID → `400 INVALID_JSON`. Respons `202 cleanup_pending` memuat field
  yang sama dengan `200` (`photos_deleted`, `annotations_deleted`, `submission_deleted`).
- Review: kode error `422 INVALID_REVIEW_STATUS` (status bukan pending/accepted/rejected), `422 INVALID_REVIEW_REASON`
  (tolak tanpa alasan, alasan pada `pending`, > 500 karakter, karakter kontrol), `422 INVALID_VERSION` (`version`
  tidak dikirim/< 1). Review massal: item kosong/> 50/duplikat/versi < 1 → `422 INVALID_ITEMS`. Alasan yang hanya
  spasi dianggap kosong. Review **tidak** menaikkan `version`.
- `GET /submissions/{id}` (pengguna) kini memuat `"review": { "status", "reason" }`.
- Export: setiap baris memuat `"review_status"`; query `review_status` yang salah → `422 INVALID_QUERY`.

## Perubahan config untuk deploy

- `admin.username` dan `admin.password_bcrypt` **dihapus**; bila masih ada, API menolak start dengan pesan
  "login admin kini lewat OTP; hapus field ini dan isi admin.superadmin.phone".
- `admin.superadmin: { "name": "...", "phone": "08…" }` wajib bila `admin.enabled`.
- `admin.otp_message_template` opsional (default memuat kata ADMIN; wajib berisi `{code}`).
- `limits.max_labels` (opsional, default 200).
- `limits.max_annotation_text_len` **dihapus**; bila masih ada, API menolak start ("hapus field ini").
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

#### Task 39: Anotasi memakai label — **SELESAI**
**Hasil:** `POST /submissions` dan `PUT /submissions/{id}` menerima kotak `{label_id, x, y, w, h}`; `text` ditolak
`400`. Label divalidasi di dalam transaksi submit/edit (`SELECT … FOR SHARE`): tidak ada → `422 INVALID_LABEL`,
diarsipkan → `422 LABEL_ARCHIVED`. `GET /submissions/{id}` memuat `label {id, name, color, archived}` + `text`
(`null` untuk kotak berlabel; baris lama: `label: null` + teks aslinya). Export JSONL memuat `label_id`, `label`
(nama saat export), `text`; query baru `label_id`. Config `limits.max_annotation_text_len` dihapus; validasi teks
anotasi (`NormalizeAnnotationText`) dibuang karena tidak ada lagi jalur tulis teks.
**Keputusan:**
- Pengecualian arsip saat edit dihitung per **foto + label**: label arsip diterima pada foto yang di versi
  sebelumnya sudah punya kotak berlabel itu (kotak tidak punya identitas, jadi "kotak yang sama" tidak bisa
  dibedakan dari kotak tambahan berlabel sama di foto itu). Di foto lain atau foto baru → `LABEL_ARCHIVED`.
  Label yang sudah dibuang dari foto itu tidak bisa dipakai lagi di sana. Daftar label lama dibaca setelah
  `BumpVersion` (baris submission terkunci) sehingga konsisten dengan versi yang diedit.
- Kunci `FOR SHARE` membuat arsip/hapus/ubah label berbarengan menunggu submit yang sedang berjalan (dan
  sebaliknya); FK `RESTRICT` tetap pagar terakhir untuk hapus.
- Submission lama (teks bebas) yang diedit wajib memberi label ke setiap kotak (PUT = replace; kotak tanpa
  `label_id` → `INVALID_LABEL`).
**Temuan:**
- Test memori export (`TestExportTenThousandPhotosStableMemory`) gagal karena halaman pertama terukur +10 MB.
  Profil heap menunjukkan penyebabnya buffer parser migrasi goose (±16 MB) yang tertahan di `sync.Pool` dari
  penyiapan schema test, bukan export: heap per halaman tetap datar. Test kini menjalankan GC dua kali sebelum
  mengukur (sync.Pool baru dilepas pada GC kedua); ambang tidak diubah, hasil 1,06 MB.
- `TestSessionsRejectBadExpiredAndRevoked` gagal acak (±25%): token "diubah" dengan mengganti karakter terakhir
  menjadi `A`, padahal karakter terakhir token base64 32 byte hanya salah satu dari `A/Q/g/w`. Diperbaiki agar
  selalu memakai karakter yang berbeda.
- Test usecase memakai label uji ber-id tetap (≥ 100001) yang disemai lewat satu koneksi khusus: penyemaian lewat
  pool dari dalam transaksi membuat test konkurensi macet (pool habis).
**Verification:** [x] `go vet`, `go build ./cmd/...`, `go test ./... -race` (domain: label wajib; repo: label JOIN,
baris lama, label arsip, `PhotoLabelIDs`, FK label; usecase: label tidak ada/arsip saat submit (tanpa sisa data),
pengecualian arsip saat edit, edit submission lama, export berlabel + filter + nama terkini; e2e: `text` → 400,
tanpa/tidak ada label → 422, GET memuat label lengkap, ganti nama langsung terlihat di GET dan export, arsip
ditolak untuk kotak baru tetapi boleh dipertahankan, baris lama di GET dan export, log hygiene)

#### Task 40: Daftar & detail submission admin — **SELESAI**
**Hasil:** `GET /admin/submissions` (filter `from`/`to`/`label_id`/`user_id`/`q`/`unlabeled`, `sort` newest/oldest,
`limit` 1–50, cursor keyset `(created_at, id)` opak) dan `GET /admin/submissions/{id}`; keduanya `Cache-Control:
no-store`. Item memuat annotator (nama, email, nomor **utuh**, `blocked`), jumlah foto/kotak, jumlah per label,
`legacy_text_count`, `retention_deletes_at`, dan sampul (foto `position` terkecil + URL bertanda tangan
`view_url_ttl` + kotaknya) sehingga satu halaman tidak butuh request tambahan per foto.
**Keputusan:**
- Satu query halaman (+1 baris untuk mendeteksi halaman berikutnya) lalu dua query agregat untuk id di halaman itu
  (jumlah foto/label, sampul `DISTINCT ON`), bukan subquery per baris.
- `q`: nama mengandung (ILIKE, karakter `%`/`_` diloloskan) **atau** awalan nomor setelah normalisasi
  (`domain.PhonePrefix`: `0812…`, `+62 812…`, `812…` → `62812…`); teks yang memuat huruf hanya dicari sebagai nama.
- Hanya submission yang masih punya foto `attached` yang tampil (yang sedang dibersihkan retensi/hapus disembunyikan).
- `review` sementara selalu `pending` dan `retention_deletes_at` = `created_at + retention.days`; diisi sungguhan di
  T46. Filter `review_status` juga di T46.
**Verification:** [x] `go vet`, `go build ./cmd/...`, `go test ./... -race` (domain: awalan nomor; usecase: semua
filter, hitungan + label + teks lama, sampul, retensi, urutan, cursor dua arah dengan created_at kembar, batas limit,
detail + annotator terhapus; e2e: mode otp dengan nomor utuh, URL sampul bisa diunduh, filter, halaman, 11 query
salah → 422, 404, 401 untuk token pengguna/tanpa token, mode none annotator null, log hygiene: respons memuat nomor
utuh tetapi log tidak)

#### Task 44: Hapus permanen (admin) — **SELESAI**
**Hasil:** migrasi `0008_submission_delete` (`submissions.deleted_at` + indeks parsial). `DELETE
/admin/submissions/{id}`, `DELETE /admin/photos/{id}`, `POST /admin/submissions/delete` (1–50 id, hasil per id).
Semua jalur baca/edit/daftar admin/export memfilter `deleted_at IS NULL`. Cleanup mendapat langkah baru: hapus
submission ber-`deleted_at` yang sudah tidak punya baris foto. Log `admin action` `submission.delete`/`photo.delete`
dengan id, status, jumlah foto/anotasi/file terhapus, dan kegagalan file.
**Keputusan:**
- Langkah 1 satu transaksi: kunci baris submission `FOR UPDATE` (edit berbarengan menunggu lalu `404`), foto →
  `deleted`, anotasi dihapus, `deleted_at` diisi (hapus submission / foto terakhir); hapus satu foto menaikkan
  `version` + `updated_at` dan merapatkan `position`. Langkah 2 setelah commit: hapus file (idempoten) lalu baris
  foto, lalu baris submission bila kosong. Langkah 2 berjalan lepas dari pembatalan request (batas 2 menit) agar
  klien yang memutus koneksi tidak meninggalkan pekerjaan setengah jalan.
- Foto lain milik submission yang masih punya baris (mis. dibuang lewat edit, menunggu cleanup) ikut dibersihkan
  saat submission dihapus.
- Edit yang kalah balapan dengan hapus kini selalu berakhir `404` (sebelumnya bisa `422 PHOTO_NOT_FOUND` karena
  foto sudah terhapus): kegagalan pemeriksaan foto/transaksi memeriksa ulang apakah submission masih ada.
- Hapus massal dipasang di luar grup JSON dengan deadline tulis 5 menit (seperti export), batas body 64 KB tetap.
- `client_request_id` submission yang dihapus: setelah barisnya hilang, retry membuat submission baru (sesuai spec);
  selama `cleanup_pending` retry mendapat `409 DUPLICATE_CLIENT_REQUEST`.
**Verification:** [x] `go vet`, `go build ./cmd/...`, `go test ./... -race` (usecase: baris + file hilang, pemilik
404, hapus foto: versi naik/position rapat/edit basi 409, foto bukan attached 404, foto terakhir menghapus submission,
SSD error → cleanup_pending + data tersembunyi + baris foto dipertahankan + cleanup menuntaskan, balapan edit vs hapus
5 putaran tanpa data hidup kembali, hapus massal + validasi; e2e: hapus foto/submission, file hilang dari SSD, pemilik
404 di GET/URL/edit, export tidak memuat, 404 berulang, 401 tanpa token, hapus massal dengan not_found + 202, log
hygiene)

#### Task 46: Review submission + pengecualian retensi — **SELESAI**
**Hasil:** migrasi `0009_review` (`review_status` default `pending`, `review_reason`, `reviewed_at`, CHECK konsistensi
status/alasan/waktu, indeks `(review_status, created_at)`). `PUT /admin/submissions/{id}/review` (dengan `version`
yang dilihat admin), `POST /admin/submissions/review` (massal, hasil `ok`/`version_conflict`/`not_found`). Edit
pengguna yang berhasil mereset review ke `pending`; hapus foto oleh admin tidak. `GET /submissions/{id}` memuat review
untuk pemilik. Daftar admin: `review` sungguhan, filter `review_status`, `retention_deletes_at = null` untuk accepted
(antrean review = `review_status=pending&sort=oldest`). Export: query `review_status` + field `review_status`. Retensi
melewati submission `accepted`. Log `admin action` `submission.review` (status saja; alasan tidak di-log).
**Keputusan:**
- Reset review dilakukan di `BumpVersion` (satu statement dengan kenaikan versi), sehingga tidak ada jalur edit yang
  lupa mereset. Hapus foto admin menaikkan versi lewat jalur lain (`DeletionRepo`) sehingga review tetap.
- `SetReview` bersyarat `version = $expected AND deleted_at IS NULL`; tidak cocok dibedakan `404` vs `409` dengan
  pemeriksaan keberadaan (sama dengan `BumpVersion`).
- Batal menerima submission yang sudah lewat 90 hari tidak menghapus apa pun di request itu; cleanup berikutnya yang
  menghapus (diuji).
**Verification:** [x] `go vet`, `go build ./cmd/...`, `go test ./... -race` (domain: aturan alasan/status; repo: CHECK,
SetReview konflik/404, BumpVersion mereset; usecase: alur review, versi lama 409, alasan wajib, hapus foto tidak
mereset, edit pemilik mereset (juga accepted), review massal + validasi, antrean review, retensi melewati accepted +
terhapus setelah batal diterima, export accepted; e2e: semua kode error, pemilik melihat status + alasan, filter daftar,
retensi null, review massal, export, edit mereset, 401/404, log hygiene tanpa alasan)
