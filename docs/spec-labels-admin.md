# Spec: Label Terkontrol & Dashboard Superadmin

> Status: Draft v0.4 (24 Okt 2026): keputusan §10 dikonfirmasi; pemilih label = dropdown; hapus permanen (§5.5); review + pengecualian retensi, kelola annotator, statistik label, status sistem (§5.7–§5.10); dashboard responsif desktop + HP (§7.3); tema terang hangat / gelap (§7.4); tanpa emoji, ikon Font Awesome (§7.5)
> Induk: [spec.md](spec.md) v0.5. Setelah disetujui, perubahan di sini digabung ke `spec.md` sebagai **v0.6**; bagian yang diganti dicantumkan di §11.

## 1. Ringkasan

Saat ini setiap kotak anotasi berisi **teks bebas**. Perubahan ini menggantinya dengan **label yang dipilih dari daftar**, dan daftar itu hanya bisa dikelola oleh admin. Admin juga mendapat **dashboard** untuk:

1. mengelola label (tambah, ubah, urutkan, arsipkan, hapus);
2. melihat hasil anotasi: foto beserta kotaknya, label tiap kotak, dan **siapa yang menganotasi**;
3. **menghapus permanen** foto/submission yang sudah dianotasi (dari database dan SSD);
4. **mereview** submission (terima / tolak); submission yang **diterima tidak ikut terhapus retensi 90 hari**;
5. **mengelola annotator** (lihat aktivitas, blokir, hapus data, hapus akun);
6. melihat **statistik per label** dan **status sistem** (WhatsApp, SSD, antrean, cleanup);
7. mengekspor dataset (export JSONL yang sudah ada, kini memuat label dan status review).

Akun admin dibuat **satu superadmin** yang ditulis di `config.json`. Login admin memakai **OTP WhatsApp ke nomor HP superadmin yang juga ditulis di `config.json`**, menggantikan username + password bcrypt.

## 2. Goals & Non-Goals

### Goals
- Anotasi baru wajib memakai satu label dari daftar label aktif; teks bebas tidak diterima lagi.
- Label dikelola admin lewat dashboard, tanpa deploy ulang dan tanpa menyentuh DB secara manual.
- Mengubah nama atau warna label langsung berlaku di semua anotasi yang memakainya (anotasi menyimpan `label_id`, bukan salinan nama).
- Label yang sudah dipakai tidak bisa hilang diam-diam: hanya bisa **diarsipkan** (tidak muncul di pilihan user, tetapi tetap tampil di anotasi lama dan di export).
- Superadmin login dengan nomor HP + OTP WhatsApp; nomor diatur di `config.json`. Berlaku di semua `auth.mode` (`none` dan `otp`).
- Dashboard admin menampilkan daftar submission (filter tanggal, label, annotator), detail foto dengan overlay kotak berwarna sesuai label, dan identitas annotator (nama, email, nomor HP).
- `id` label stabil sehingga bisa dipakai langsung sebagai indeks kelas saat export ke format model.
- Admin menyaring kualitas data lewat review; hanya data yang diterima yang disimpan melewati 90 hari, dan export bisa dibatasi ke data yang diterima.
- Admin bisa menghentikan annotator bermasalah (blokir) dan membersihkan datanya.
- Admin melihat keseimbangan dataset per label dan kesehatan sistem tanpa membuka Grafana.

### Non-Goals
- Banyak admin / peran admin bertingkat / manajemen admin dari UI (satu superadmin di config).
- Admin mengedit anotasi milik user (mengubah label, kotak, atau urutan foto). Satu-satunya tindakan admin atas data user adalah **hapus permanen** (§5.5).
- Tempat sampah / pemulihan setelah hapus. Hapus admin bersifat final.
- Review per kotak atau per foto (review berlaku untuk satu submission utuh), review bertingkat, atau beberapa reviewer.
- Statistik per annotator, per hari, atau grafik tren (hanya statistik per label).
- Menggantikan Grafana/alert: status sistem di dashboard hanya keadaan saat ini, tanpa riwayat.
- Hierarki atau grup label, label per proyek, sinonim.
- Catatan teks bebas tambahan per kotak (lihat §10, pertanyaan 1).
- Audit log di database (aksi admin dicatat di log aplikasi, §8).
- Thumbnail di server (tetap non-goal spec induk; dashboard memakai foto hasil proses FE yang sudah ±400 KB).

## 3. Superadmin & login OTP

### 3.1 Konfigurasi

Bagian `admin` di `config.json` berubah:

```json
"admin": {
  "enabled": true,
  "superadmin": {
    "name": "Super Admin",
    "phone": "0812-3456-7890"
  },
  "token_ttl": "8h",
  "otp_message_template": "*{code}* adalah kode masuk ADMIN Photo Annotated. Berlaku {ttl_minutes} menit. Jika Anda tidak sedang masuk, abaikan dan periksa keamanan akun."
}
```

Validasi saat start (fail fast, sama seperti config lain):
- `superadmin.phone` dinormalisasi dengan `domain.NormalizePhone` (§7.4 induk) dan harus nomor seluler Indonesia; gagal → API tidak start.
- `superadmin.name` 1–100 karakter, tanpa karakter kontrol.
- Field lama `admin.username` / `admin.password_bcrypt` **ditolak** dengan pesan jelas ("login admin kini lewat OTP; hapus field ini dan isi `admin.superadmin.phone`"), agar salah konfigurasi tidak lolos diam-diam.
- `admin.enabled = true` mewajibkan `wa_notif.base_url`, `secrets.wa_notif_token`, `secrets.otp_pepper`, dan `secrets.admin_token_secret` **di semua `auth.mode`**. Pada `auth.mode = none` dispatcher OTP dan probe `wa-notif` tetap dijalankan, khusus untuk login admin.
- `otp_message_template` opsional (default di atas); wajib memuat `{code}`.

Mengganti nomor di config lalu restart API langsung membatalkan semua token admin lama (§3.3).

### 3.2 Alur login

```
/admin/masuk: isi nomor HP  →  POST /admin/otp/request  →  202 { challenge_id }
          → polling GET /admin/otp/{challenge_id}  (queued → sent / failed)
          → isi 6 digit  →  POST /admin/otp/verify  →  200 { access_token }
```

- Memakai antrean OTP, dispatcher, kode turunan `challenge_id`, batas per nomor, `max_queue`, dan `daily_global_cap` yang sama dengan OTP user (§4.0, §9.5 induk). Tidak ada antrean kedua.
- **Anti-enumerasi:** nomor yang bukan nomor superadmin mendapat `202` berbentuk sama dan challenge palsu (mekanisme challenge palsu `password/forgot` yang sudah ada), tanpa pesan WhatsApp. Jadi nomor superadmin tidak bisa ditebak dari respons.
- Turnstile wajib pada `request` dan `resend` bila `turnstile.enabled`.
- Saat verify, nomor challenge dicocokkan ulang dengan `superadmin.phone` dari config yang sedang berjalan; jika berbeda (config diganti di tengah alur) → `410 OTP_EXPIRED`.
- Nomor superadmin boleh sama dengan nomor akun user biasa. Keduanya tetap terpisah: login user tidak pernah menghasilkan token admin, dan sebaliknya.

### 3.3 Token admin

Tetap stateless HMAC, tetapi versi baru mengikat nomor superadmin:

```
adm2.<exp unix>.<base64url(HMAC-SHA256(ADMIN_TOKEN_SECRET, "admin\n" ‖ superadmin_phone ‖ "\n" ‖ exp))>
```

- Berlaku `admin.token_ttl` (default 8 jam). Token `adm1` lama ditolak `401`.
- Mengganti `superadmin.phone` atau merotasi `ADMIN_TOKEN_SECRET` membatalkan semua token.
- Logout = FE membuang token (tidak ada pencabutan per token, sama seperti sekarang).
- Admin tetap **tidak bisa mengedit** data user: token admin pada `PUT /submissions/{id}`, presign, submit, `/auth/me` → `403 ADMIN_READ_ONLY` (tidak berubah). Yang boleh ditulis admin hanya label, dan admin boleh **menghapus permanen** submission/foto lewat endpoint khusus `/admin/*` (§5.5).

### 3.4 Perubahan pada `otp_challenges`

- `purpose` bertambah `admin_login`.
- `user_id` menjadi `NULL`-able; CHECK: `(purpose = 'admin_login') = (user_id IS NULL)`. Data registrasi wajib kosong untuk `admin_login` (CHECK `otp_registration_data` diperluas).
- Indeks unik parsial "satu challenge aktif per nomor" dipisah per jenis: `(phone, purpose = 'admin_login') WHERE status IN ('queued','sending','sent')`, agar login admin dan login/reset user di nomor yang sama tidak saling membatalkan.
- Pesan WhatsApp untuk `admin_login` memakai `admin.otp_message_template`.

## 4. Label

### 4.1 Aturan

| Atribut | Aturan |
|---|---|
| `id` | Integer, dibuat server, **tidak pernah dipakai ulang**. Stabil untuk indeks kelas di export. |
| `name` | 1–50 karakter setelah trim, tanpa karakter kontrol/bidi (aturan sama dengan teks anotasi lama). **Unik tanpa membedakan huruf besar-kecil**, termasuk terhadap label yang diarsipkan (untuk memakai nama itu lagi, buka arsipnya). |
| `description` | Opsional, ≤ 200 karakter. Ditampilkan sebagai petunjuk di pemilih label user. |
| `color` | `#rrggbb` huruf kecil. Bila kosong saat dibuat, server memilih dari palet bawaan secara bergiliran. Dipakai untuk warna kotak di FE user dan dashboard. |
| `sort_order` | Urutan tampil di pemilih label. |
| `archived_at` | `NULL` = aktif. Label arsip tidak muncul di `GET /labels`, tidak bisa dipilih untuk kotak baru, tetapi tetap tampil di anotasi lama dan di export. |

- Batas: maksimal **200 label aktif** (`limits.max_labels`).
- **Hapus** hanya boleh untuk label yang belum pernah dipakai anotasi mana pun (`409 LABEL_IN_USE`; FE menawarkan arsip). Retensi 90 hari yang menghapus anotasi bisa membuat label kembali bisa dihapus.
- **Ganti nama** berlaku retroaktif ke semua anotasi (anotasi menyimpan `label_id`). Karena itu dashboard menampilkan peringatan "dipakai di N kotak" saat mengganti nama. Ganti nama yang mengubah arti (mis. "Retak" → "Penyok") sebaiknya dilakukan dengan label baru + arsip label lama.

### 4.2 Anotasi memakai label

- Setiap kotak di `POST /submissions` dan `PUT /submissions/{id}` wajib membawa `label_id` (integer). Field `text` tidak diterima lagi (`400` field tak dikenal, sama dengan perlakuan field tak dikenal lain).
- `label_id` harus label yang ada dan **aktif** → selain itu `422 INVALID_LABEL` (tidak ada) atau `422 LABEL_ARCHIVED`.
- Pengecualian saat edit (`PUT`): label arsip tetap diterima **jika foto yang sama sudah punya kotak berlabel itu di versi sebelumnya**. Tanpa ini user tidak bisa mengedit submission miliknya hanya karena admin mengarsipkan label di tengah jendela edit 24 jam.
- Validasi label dilakukan di dalam transaksi submit/edit (baca label `FOR SHARE`), sehingga label yang diarsipkan/dihapus berbarengan tidak lolos.

### 4.3 Data lama (teks bebas)

Anotasi yang sudah tersimpan dengan teks bebas **tidak diubah dan tidak dipetakan otomatis**:
- Kolom `annotations.text` dipertahankan untuk baris lama; baris baru selalu `label_id` dengan `text = NULL`.
- `GET /submissions/{id}`, dashboard, dan export menampilkan baris lama dengan `label: null` dan `text` aslinya (ditandai "catatan lama" di UI).
- Submission lama yang masih dalam jendela edit 24 jam saat rilis: saat diedit, FE menampilkan teks lama sebagai petunjuk dan user wajib memilih label untuk setiap kotak sebelum menyimpan (karena `PUT` bersifat replace).
- Setelah 90 hari retensi, semua data teks bebas habis dengan sendirinya.

## 5. API

Base path `/api/v1`, format error sama dengan spec induk.

### 5.1 Untuk user

#### `GET /labels`
Daftar label **aktif**, terurut `sort_order`, lalu `name`.
```json
{ "labels": [ { "id": 3, "name": "Retak", "description": "Retakan pada dinding paket", "color": "#e4572e" } ] }
```
- Akses: sama dengan presign (`user` pada mode `otp`; publik pada mode `none`, tanpa Turnstile karena hanya baca). Token admin juga boleh.
- `Cache-Control: private, max-age=60`. FE memuat ulang daftar saat membuka layar anotasi dan setelah menerima `LABEL_ARCHIVED`/`INVALID_LABEL`.

#### `POST /submissions` dan `PUT /submissions/{id}` (berubah)
Bentuk anotasi:
```json
{ "label_id": 3, "x": 0.12, "y": 0.30, "w": 0.25, "h": 0.18 }
```
Aturan bbox, jumlah kotak, idempotensi, versi, dan jendela edit tidak berubah.

#### `GET /submissions/{id}` (berubah)
Anotasi memuat label lengkap agar FE tidak perlu mencari ke daftar aktif (label arsip tetap tampil benar):
```json
{ "label": { "id": 3, "name": "Retak", "color": "#e4572e", "archived": false }, "text": null,
  "x": 0.12, "y": 0.30, "w": 0.25, "h": 0.18 }
```
Baris lama: `"label": null, "text": "Kerusakan di sisi kiri"`.

### 5.2 Login admin

| Endpoint | Request | Response |
|---|---|---|
| `POST /admin/otp/request` | `{ "phone": "...", "turnstile_token": "..." }` | `202` sama dengan `/auth/register` (`challenge_id`, `status`, `resend_after_seconds`, `estimated_wait_seconds`) |
| `GET /admin/otp/{challenge_id}` | – | sama dengan `GET /auth/otp/{id}` (termasuk perilaku id palsu) |
| `POST /admin/otp/resend` | `{ "challenge_id": "...", "turnstile_token": "..." }` | `202` sama dengan `/auth/otp/resend` |
| `POST /admin/otp/verify` | `{ "challenge_id": "...", "code": "123456" }` | `200 { "access_token": "adm2…", "expires_at": "…", "admin": { "name": "Super Admin", "phone": "62812****7890" } }` |
| `GET /admin/me` | Bearer admin | `200 { "name": "…", "phone": "62812****7890", "expires_at": "…", "auth_mode": "otp" }` |

- Error sama dengan endpoint OTP user (`OTP_INVALID` + `attempts_left`, `OTP_EXPIRED`, `OTP_TOO_MANY_ATTEMPTS`, `OTP_NOT_SENT`, `OTP_BUSY`, `OTP_UNAVAILABLE`, `TURNSTILE_*`, `RATE_LIMITED`).
- Rate limit: `request` + `resend` berbagi `limits.rate_limit_per_min.admin_login` (5/menit per IP); `verify` memakai `otp_verify` (20/menit).
- `GET /auth/channel-status` saat ini `404` pada mode `none`; halaman login admin memakai `GET /admin/channel-status` (bentuk sama) yang selalu tersedia bila `admin.enabled`.
- `POST /admin/login` (username/password) **dihapus** → `404`.
- Jika `admin.enabled = false`, semua `/admin/*` → `404` (tidak berubah).

### 5.3 Kelola label (admin)

Semua butuh `Authorization: Bearer <token admin>`; tanpa/salah → `401`.

#### `GET /admin/labels`
Semua label termasuk arsip, dengan jumlah pemakaian:
```json
{ "labels": [ { "id": 3, "name": "Retak", "description": "…", "color": "#e4572e", "sort_order": 1,
                "archived": false, "usage_count": 128, "created_at": "…", "updated_at": "…" } ] }
```
`usage_count` = jumlah anotasi pada foto `attached` yang memakai label itu.

#### `POST /admin/labels`
Request `{ "name": "Retak", "description": "…", "color": "#e4572e" }` (`description`, `color` opsional). Label baru diletakkan di urutan terakhir. Response `201` berisi label. Error: `422 INVALID_LABEL_NAME` / `INVALID_LABEL_COLOR` / `INVALID_LABEL_DESCRIPTION`, `409 LABEL_NAME_TAKEN`, `422 LABEL_LIMIT`.

#### `PUT /admin/labels/{id}`
Replace: `{ "name": "…", "description": "…", "color": "#…", "archived": false }`. Membuka arsip juga lewat sini (tunduk pada `LABEL_LIMIT`). Response `200`. Id tak ada → `404`.

#### `PUT /admin/labels/order`
`{ "ids": [5, 3, 9, …] }` = urutan baru **semua label aktif**. Daftar yang tidak tepat sama dengan himpunan label aktif saat ini → `409 LABEL_ORDER_STALE` (FE memuat ulang).

#### `DELETE /admin/labels/{id}`
`204` bila belum pernah dipakai; `409 LABEL_IN_USE` (dengan `details.usage_count`) bila sudah. Dihitung terhadap **semua** anotasi (termasuk foto `deleted` yang belum dibersihkan), dijaga juga oleh FK `ON DELETE RESTRICT`.

CORS: tambahkan method `DELETE` (§9.4 induk). Di produksi FE dan API satu origin lewat nginx, tetapi aturan CORS tetap dijaga untuk dev.

### 5.4 Hasil anotasi (admin)

#### `GET /admin/submissions`
Query (semua opsional):

| Param | Arti |
|---|---|
| `from`, `to` | RFC 3339 atas `created_at` (from inklusif, to eksklusif), sama dengan export |
| `label_id` | hanya submission yang punya ≥ 1 kotak berlabel ini |
| `user_id` | hanya milik annotator ini |
| `q` | cari annotator berdasarkan nama (mengandung, tanpa beda huruf besar-kecil) atau nomor HP (awalan setelah normalisasi; `0812…` dan `62812…` sama) |
| `unlabeled` | `true` = hanya submission yang masih punya kotak teks lama |
| `review_status` | `pending` / `accepted` / `rejected` (§5.7) |
| `sort` | `newest` (default) atau `oldest`. `oldest` + `review_status=pending` = antrean review, yang paling dekat batas retensi lebih dulu |
| `limit` | 1–50, default 20 |
| `cursor` | dari `next_cursor` sebelumnya (keyset `(created_at, id)` sesuai arah `sort`, opak bagi FE) |

Response:
```json
{
  "items": [
    {
      "id": "7d2a…", "created_at": "…", "updated_at": "…", "version": 2,
      "review": { "status": "pending", "reason": null, "reviewed_at": null },
      "retention_deletes_at": "2027-01-22T12:00:00Z",
      "annotator": { "id": "…", "name": "Budi Santoso", "email": "budi@example.com", "phone": "6281234567890", "blocked": false },
      "photo_count": 3, "annotation_count": 7,
      "labels": [ { "id": 3, "name": "Retak", "color": "#e4572e", "count": 5 } ],
      "legacy_text_count": 0,
      "cover": { "photo_id": "0b6c…", "url": "https://…/photos/0b6c…/content?exp=…&sig=…", "width": 1920, "height": 1440,
                 "annotations": [ { "label_id": 3, "x": 0.12, "y": 0.30, "w": 0.25, "h": 0.18 } ] }
    }
  ],
  "next_cursor": "…"
}
```
- `annotator` bernilai `null` pada mode `none` (UI: "Anonim") atau bila akun user sudah dihapus.
- `retention_deletes_at` = `created_at + retention.days`, atau `null` bila `accepted` (tidak ikut retensi, §5.7).
- `cover` = foto pertama (`position` 0) dengan URL lihat bertanda tangan (`storage.view_url_ttl`) dan kotaknya, sehingga satu halaman daftar tidak butuh request tambahan per foto.
- Query salah → `422 INVALID_QUERY`. `Cache-Control: no-store` (berisi data pribadi).

#### `GET /admin/submissions/{id}`
Detail lengkap: bentuk sama dengan `GET /submissions/{id}` (§5.1), ditambah `annotator`, `review`, `retention_deletes_at`, dan, untuk tiap foto, `url` bertanda tangan dan `sha256`. Hanya foto `attached`. Id tak ada → `404`. `Cache-Control: no-store`.

`GET /submissions/{id}` dan `GET /photos/{id}/url` dengan token admin tetap berfungsi seperti sekarang.

### 5.5 Hapus permanen (admin)

Admin dapat menghapus **satu foto** atau **seluruh submission** yang sudah dianotasi. Hapusnya **hard delete**: baris di database (submission, foto, anotasi) dan file foto di SSD benar-benar dihapus, tidak ada tempat sampah, dan tidak bisa dipulihkan dari aplikasi. (Catatan: backup DB/foto belum ada, T33 ditunda; data yang dihapus benar-benar hilang.)

| Endpoint | Efek |
|---|---|
| `DELETE /admin/submissions/{id}` | Submission + semua fotonya + semua anotasinya + semua file fotonya |
| `DELETE /admin/photos/{id}` | Satu foto + anotasinya + filenya. Bila itu foto terakhir di submission, submission ikut dihapus |
| `POST /admin/submissions/delete` | Hapus banyak sekaligus: `{ "ids": ["…", …] }`, 1–50 id, tanpa duplikat. Diproses per submission; id yang tidak ada dilewati |

**Urutan kerja** (mengikuti aturan cleanup induk §8.3: *file dulu, baris kemudian*, sehingga tidak pernah ada baris yang menunjuk ke file yang sudah hilang):

1. **Satu transaksi:** kunci baris submission (`FOR UPDATE`, sehingga edit user yang berbarengan menunggu lalu mendapat `404`), tandai foto yang dihapus `deleted`, hapus anotasinya saat itu juga.
   - Hapus submission: semua fotonya (status apa pun yang masih punya baris) ditandai `deleted`, dan `submissions.deleted_at` diisi. Sejak commit, submission itu `404` di semua endpoint user dan admin, dan tidak ikut export.
   - Hapus satu foto: `version` submission dinaikkan dan `updated_at` diperbarui (tab edit user yang terbuka mendapat `409 VERSION_CONFLICT` lalu memuat ulang), `position` foto sisanya dirapatkan. Jika tidak ada lagi foto `attached`, submission ditandai `deleted_at` seperti di atas.
2. **Setelah commit, masih di dalam request:** hapus file tiap foto dari SSD (`PhotoStore.Delete`, idempoten: file yang sudah tidak ada dianggap berhasil), lalu hapus baris fotonya; lalu hapus baris submission yang sudah `deleted_at` dan tidak punya foto lagi.
3. **Bila langkah 2 gagal sebagian** (mis. error I/O SSD): baris yang filenya gagal dihapus tetap berstatus `deleted` (tidak terlihat oleh siapa pun) dan diselesaikan oleh cleanup job berkala (langkah 2 cleanup induk). Cleanup mendapat langkah baru: hapus submission ber-`deleted_at` yang sudah tidak punya foto.

Response:
- `200 { "status": "deleted", "photos_deleted": 3, "annotations_deleted": 7, "submission_deleted": true }` bila semua file dan baris sudah terhapus.
- `202 { "status": "cleanup_pending", … }` bila data sudah tersembunyi tetapi sebagian file menunggu cleanup. Dashboard menampilkan "Dihapus; sebagian file dibersihkan otomatis".
- Hapus massal: `200 { "results": [ { "id": "…", "status": "deleted" | "cleanup_pending" | "not_found" } ] }`.
- Id tidak ada / sudah dihapus → `404` (hapus massal: `not_found` per id). Foto yang bukan `attached` (mis. `pending` milik upload yang belum dikirim) → `404`; foto seperti itu sudah ditangani cleanup biasa.
- Hapus tidak dibatasi jendela edit 24 jam dan tidak dibatasi status annotator.

Dampak lain:
- `usage_count` label turun; label yang tidak dipakai lagi menjadi bisa dihapus.
- User yang membuka submission yang sudah dihapus mendapat `404`; FE user menampilkan "Submission ini sudah dihapus" dan membuang entri draft/`edit_token`-nya.
- Retry `POST /submissions` dengan `client_request_id` milik submission yang sudah dihapus admin membuat submission baru (kunci idempotensi ikut terhapus). Dapat diterima: retry hanya terjadi beberapa detik setelah kirim.
- Hapus massal 50 submission × 5 foto = 250 file; deadline tulis request diperpanjang seperti export agar tidak terpotong `WriteTimeout`.

### 5.6 Export (berubah)

`GET /admin/export` tetap JSONL satu baris per foto. Tiap anotasi kini:
```json
{"label_id":3,"label":"Retak","text":null,"bbox_norm":[0.12,0.30,0.25,0.18],"bbox_px":[230,432,480,259]}
```
- Baris lama: `"label_id": null, "label": null, "text": "…"`.
- `label` = nama **saat export** (bukan saat anotasi dibuat). Konsumen yang butuh kelas stabil memakai `label_id`.
- Query baru opsional `label_id` (hanya foto yang punya kotak berlabel itu; kotak lain di foto yang sama tetap ikut agar foto tidak dianggap "kosong" di area yang sebenarnya beranotasi).
- Export tetap **tanpa data pribadi**; identitas annotator hanya ada di dashboard.
- Query baru opsional `review_status` (`pending` / `accepted` / `rejected`; tanpa parameter = semua, seperti sekarang). Tiap baris memuat `"review_status"`. Tombol export di dashboard memakai `review_status=accepted` sebagai default.

### 5.7 Review submission & pengecualian retensi

Setiap submission punya status review: `pending` (default, belum direview) → `accepted` (diterima) atau `rejected` (ditolak). Review berlaku untuk **satu submission utuh**. Review bukan edit: admin tidak mengubah foto, kotak, atau label.

#### `PUT /admin/submissions/{id}/review`
```json
{ "status": "rejected", "reason": "Kotak tidak menutupi seluruh kerusakan", "version": 2 }
```
- `status`: `accepted`, `rejected`, atau `pending` (membatalkan review).
- `reason`: wajib untuk `rejected` (1–500 karakter, aturan teks sama dengan nama label), opsional untuk `accepted`, harus kosong untuk `pending`.
- `version` = versi submission yang dilihat admin. Bila user sudah mengedit sejak itu → `409 VERSION_CONFLICT`; admin memuat ulang dan melihat isi terbaru sebelum memutuskan. Dengan begitu admin tidak pernah menerima isi yang belum ia lihat.
- Response `200 { "review": { "status": "…", "reason": "…", "reviewed_at": "…" }, "retention_deletes_at": … }`. Id tak ada / sudah dihapus → `404`.

#### `POST /admin/submissions/review`
Review massal dari daftar: `{ "items": [ { "id": "…", "version": 2 } ], "status": "accepted", "reason": null }`, 1–50 item. Diproses per item; hasil `200 { "results": [ { "id": "…", "result": "ok" | "version_conflict" | "not_found" } ] }`.

#### Aturan
- **Edit oleh user mereset review.** `PUT /submissions/{id}` yang berhasil mengembalikan status ke `pending` (alasan dihapus), karena isinya berubah sejak direview. Ini berlaku juga untuk `accepted`: submission yang diterima lalu diedit user kembali masuk antrean review dan **kembali ikut retensi** sampai diterima lagi. (Edit hanya mungkin dalam 24 jam pertama, jadi kasus ini jarang.)
- Hapus foto oleh admin (§5.5) **tidak** mereset review (tindakan admin sendiri).
- **User melihat status review** submission miliknya: `GET /submissions/{id}` memuat `"review": { "status": "rejected", "reason": "…" }` (tanpa `reviewed_at`), dan riwayat user menampilkan lencana "Menunggu review" / "Diterima" / "Ditolak" beserta alasannya. User yang masih dalam jendela edit bisa memperbaiki submission yang ditolak; perbaikan itu kembali ke `pending`.

#### Pengecualian retensi
- Cleanup retensi (§8.3 induk, langkah 3) kini hanya menghapus submission yang `created_at < now − retention.days` **dan** `review_status <> 'accepted'`. Submission `accepted` disimpan tanpa batas waktu sampai admin menghapusnya (§5.5) atau membatalkan penerimaannya.
- Membatalkan penerimaan (`accepted` → `pending`/`rejected`) pada submission yang sudah lewat 90 hari membuatnya terhapus pada putaran cleanup berikutnya (paling lama `retention.cleanup_interval`, default 1 jam). Dashboard memperingatkan hal ini di dialog konfirmasi.
- Status sistem (§5.10) menampilkan jumlah submission belum diterima yang akan terhapus retensi dalam 7 hari, agar admin sempat mereview.
- Data yang diterima menumpuk di SSD. Penjagaan ruang yang sudah ada (`min_free_bytes`, `503 STORAGE_FULL`, alert SSD > 80%) tetap berlaku; status sistem menampilkan pemakaian SSD. Ukuran SSD 250 GB ≈ 650 ribu foto.
- Ini menjawab open question #1 spec induk (dataset vs retensi): data untuk model = submission `accepted`, disimpan di tempatnya, diekspor dengan `review_status=accepted`.

### 5.8 Kelola annotator (hanya `auth.mode = otp`)

Pada mode `none` tidak ada akun user: semua endpoint di bagian ini `404`, dan menu Annotator disembunyikan (`GET /admin/me` memuat `"auth_mode"` agar FE tahu).

#### `GET /admin/users`
Query: `q` (nama mengandung / awalan nomor, seperti §5.4), `status` (`active` / `blocked`; default keduanya; user `pending` yang belum verifikasi tidak ditampilkan), `limit` 1–50 (default 20), `cursor` (keyset `(created_at DESC, id DESC)`).
```json
{ "items": [ {
    "id": "…", "name": "Budi Santoso", "email": "budi@example.com", "phone": "6281234567890",
    "created_at": "…", "last_login_at": "…",
    "blocked": false, "blocked_at": null, "blocked_reason": null,
    "stats": { "submissions": 12, "annotations": 48, "accepted": 9, "rejected": 1, "pending": 2, "last_submission_at": "…" }
  } ], "next_cursor": "…" }
```
`Cache-Control: no-store`. `stats` dihitung dari submission yang belum dihapus.

#### `GET /admin/users/{id}`
Satu user dengan bentuk yang sama. Submission-nya dibuka lewat `GET /admin/submissions?user_id=…`.

#### `POST /admin/users/{id}/block`
`{ "reason": "Anotasi asal-asalan berulang kali" }` (1–500 karakter, wajib). Dalam satu transaksi: `users.blocked_at` dan `blocked_reason` diisi, dan **semua sesi user dicabut** (`revoked_at`). Akibatnya:
- Request berikutnya dengan token lama → `401` (FE kembali ke layar masuk).
- `POST /auth/login` dengan password **benar** → `403 ACCOUNT_BLOCKED` ("Akun Anda diblokir. Hubungi admin."). Password salah tetap `401 INVALID_CREDENTIALS`, sehingga status blokir tidak bocor ke orang yang tidak tahu passwordnya.
- `register` dengan nomor itu → `202` palsu seperti nomor aktif (tidak bisa mendaftar ulang). `password/forgot` → `202` palsu tanpa pesan WhatsApp (tidak bisa mereset password untuk membuka blokir).
- Pengecekan sesi menolak user yang diblokir walau sesinya belum sempat dicabut (join `users.blocked_at` pada lookup sesi; tetap satu query).
- Data user (submission, foto) **tidak** berubah. Blokir user yang sudah diblokir → `200` tanpa perubahan.

#### `POST /admin/users/{id}/unblock`
Mengosongkan `blocked_at`/`blocked_reason`. User login lagi dengan password lamanya (sesi lama tetap dicabut).

#### `POST /admin/users/{id}/delete-data`
Hapus permanen semua submission user ini, memakai mekanisme §5.5. Body: `{ "include_accepted": false }`; default **submission `accepted` dipertahankan** (data bagus tidak ikut hilang karena satu klik), `true` = hapus semua.
- Jumlahnya bisa ratusan, jadi request ini hanya menjalankan langkah 1 §5.5 untuk semuanya dalam satu transaksi (submission `deleted_at`, foto `deleted`, anotasi dihapus), lalu membalas `202 { "submissions": 40, "photos": 112, "annotations": 380, "status": "cleanup_pending" }`. File dan baris dibersihkan cleanup berkala (langkah 2 cleanup induk sudah menangani foto `deleted` usia berapa pun; dijalankan segera sekali setelah request ini, tidak menunggu interval).
- Akun user tidak dihapus; blokir tetap terpisah.

#### `DELETE /admin/users/{id}` (hapus akun; ditambahkan 25 Okt)
Menghapus akun annotator **beserta semua datanya**, termasuk submission `accepted` dan foto yang sudah diunggah tetapi belum dikirim. Dalam satu transaksi: baris user dikunci (`FOR UPDATE`, sehingga submit yang berbarengan menunggu lalu gagal atau sudah tercakup), langkah 1 §5.5 untuk semua submission-nya, foto lepasnya ditandai `deleted`, lalu baris `users` dihapus (sesi dan challenge OTP ikut terhapus lewat `ON DELETE CASCADE`). File dibersihkan cleanup yang dijalankan segera, sama seperti `delete-data`.
- Response `202 { "account_deleted": true, "submissions": 2, "photos": 4, "annotations": 4, "status": "cleanup_pending" }`, atau `200` dengan `"status": "deleted"` bila tidak ada foto sama sekali. User tidak ada / belum verifikasi → `404`.
- Sesudahnya token lama → `401`, login → `401 INVALID_CREDENTIALS` (seperti nomor tak terdaftar), dan **nomor itu bisa didaftarkan lagi** sebagai akun baru (kode WhatsApp terkirim). Untuk mencegah nomor dipakai lagi, admin cukup memblokir, bukan menghapus.
- Alasan: pemilik nomor yang ingin "mulai dari awal" tidak bisa mendaftar ulang selama akun lama ada (pendaftaran nomor aktif mendapat challenge palsu tanpa pesan, §5.8 blokir dan T35).

### 5.9 Statistik per label

#### `GET /admin/stats/labels`
Query: `from`, `to` (atas `created_at` submission, seperti export), `review_status` (opsional).
```json
{
  "totals": { "submissions": 420, "photos": 1130, "annotations": 3870, "legacy_text_annotations": 12 },
  "labels": [
    { "id": 3, "name": "Retak", "color": "#e4572e", "archived": false,
      "annotations": 1520, "photos": 610, "submissions": 300, "share": 0.393 }
  ]
}
```
- Semua label ikut (termasuk yang arsip dan yang belum pernah dipakai, dengan angka 0), urut `annotations` menurun, agar label yang kekurangan contoh terlihat.
- `annotations` = jumlah kotak; `photos` / `submissions` = jumlah foto / submission yang punya ≥ 1 kotak berlabel itu; `share` = porsi kotak label ini dari semua kotak berlabel.
- Hanya foto `attached` pada submission yang belum dihapus. Satu query `GROUP BY` memakai indeks `annotations(label_id)`.

### 5.10 Status sistem

#### `GET /admin/system`
Keadaan saat ini, dihitung saat diminta (ringan: beberapa query hitung + nilai yang sudah dipegang proses). Dashboard memuat ulang tiap 30 detik selama tab terlihat.
```json
{
  "checked_at": "…",
  "api": { "started_at": "…", "version": "3206339", "auth_mode": "otp" },
  "database": { "ok": true, "latency_ms": 3 },
  "storage": { "available": true, "total_bytes": 250000000000, "free_bytes": 212000000000, "used_pct": 15.2,
               "min_free_bytes": 5368709120, "photos_pending": 4 },
  "uploads": { "active": 2, "max": 200 },
  "whatsapp": { "otp_available": true, "last_probe_at": "…", "queued": 0, "max_queue": 200,
                "sent_24h": 31, "failed_24h": 1, "daily_cap": 500 },
  "cleanup": { "last_run_at": "…", "last_ok": true, "files_pending_delete": 0 },
  "data": { "submissions": 420, "review_pending": 37, "accepted": 300, "rejected": 83,
            "retention_due_7d": 5, "users_active": 58, "users_blocked": 2 },
  "host": { "cpu_temp_c": 52.1 }
}
```
- `storage.available = false` bila root foto tidak ter-mount / penanda hilang (`STORAGE_UNAVAILABLE`).
- `cleanup.files_pending_delete` = foto `deleted` yang filenya belum terhapus (mis. sisa `cleanup_pending`).
- `data.retention_due_7d` = submission belum `accepted` yang akan terhapus retensi dalam 7 hari.
- `host.cpu_temp_c` dibaca dari `/sys/class/thermal/thermal_zone0/temp` bila terbaca dari container; `null` bila tidak.
- `version` = commit build (diisi saat build image lewat `-ldflags`).
- Bila satu bagian gagal dihitung (mis. DB lambat), bagian itu berisi `{ "error": "…" }` dan bagian lain tetap dikirim; respons tetap `200`.
- Tidak memuat data pribadi; tetap `Cache-Control: no-store`.

## 6. Data Model

Migrasi dipecah per task (§12), mulai `0006_…`.

### 6.1 `labels`
| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | `integer GENERATED ALWAYS AS IDENTITY` PK | Tidak dipakai ulang |
| `name` | `text` NOT NULL | CHECK `char_length(name) BETWEEN 1 AND 50`; indeks unik `lower(name)` |
| `description` | `text` NULL | CHECK `char_length ≤ 200` |
| `color` | `text` NOT NULL | CHECK `color ~ '^#[0-9a-f]{6}$'` |
| `sort_order` | `integer` NOT NULL DEFAULT 0 | |
| `archived_at` | `timestamptz` NULL | |
| `created_at`, `updated_at` | `timestamptz` NOT NULL DEFAULT now() | |

Indeks: unik `lower(name)`; `(sort_order) WHERE archived_at IS NULL`.

### 6.2 `annotations` (berubah)
- Tambah `label_id integer NULL REFERENCES labels (id) ON DELETE RESTRICT`.
- `text` menjadi `NULL`-able (CHECK panjang tetap untuk nilai yang ada).
- CHECK `annotations_label_or_text`: `label_id IS NOT NULL OR text IS NOT NULL`. Aplikasi hanya menulis `label_id` untuk baris baru.
- Indeks `(label_id)` untuk `usage_count`, filter `label_id`, dan pengecekan hapus.

### 6.3 `otp_challenges` (berubah)
Lihat §3.4: `purpose` + `admin_login`, `user_id` nullable dengan CHECK, CHECK data registrasi diperluas, indeks unik challenge aktif per `(phone, purpose = 'admin_login')`.

### 6.4 `submissions` (berubah)
- Tambah `deleted_at timestamptz NULL`: diisi saat admin menghapus (§5.5). Semua query baca, edit, daftar admin, dan export memfilter `deleted_at IS NULL`. Baris ini hanya hidup sampai file fotonya selesai dihapus (biasanya di request yang sama).
- Indeks parsial `(deleted_at) WHERE deleted_at IS NOT NULL` untuk langkah cleanup.
- Tambah `review_status text NOT NULL DEFAULT 'pending'` CHECK `IN ('pending','accepted','rejected')`, `review_reason text NULL` (CHECK 1–500 karakter; wajib bila `rejected`, kosong bila `pending`), `reviewed_at timestamptz NULL` (kosong ⇔ `pending`). Submission lama otomatis `pending`.
- Indeks `(review_status, created_at)` untuk antrean review, filter, dan query retensi.

### 6.5 `users` (berubah)
- Tambah `blocked_at timestamptz NULL` dan `blocked_reason text NULL` (CHECK: keduanya terisi atau keduanya kosong; alasan 1–500 karakter).
- Lookup sesi ikut memeriksa `users.blocked_at IS NULL`.

Migrasi `Down` menghapus label hanya jika tidak ada anotasi yang memakai `label_id` (jika ada, `Down` gagal dengan pesan jelas, bukan menghapus data).

## 7. Frontend

### 7.1 User (perubahan pada layar anotasi)
- Isian teks per kotak diganti **dropdown label** (`<select>` native, ramah HP): opsi = label aktif dari `GET /labels` urut `sort_order`, opsi pertama "Pilih label…" (kosong, tidak valid untuk dikirim). Titik warna label terpilih tampil di samping dropdown, dan `description` label terpilih tampil di bawahnya sebagai petunjuk. Tidak ada isian teks bebas.
- Warna kotak di foto mengikuti warna label; kotak tanpa label digambar abu-abu putus-putus.
- Tombol Submit nonaktif sampai semua kotak punya label (menggantikan aturan "semua kotak punya teks").
- Daftar label kosong → pesan "Belum ada label. Hubungi admin." dan anotasi tidak bisa dikirim.
- Draft `localStorage` menyimpan `label_id`. Draft lama berisi teks: kotak dipertahankan, label dikosongkan. Label di draft yang sudah tidak aktif dikosongkan dan kotaknya ditandai.
- Menerima `422 INVALID_LABEL` / `LABEL_ARCHIVED`: muat ulang `GET /labels`, tandai kotak yang labelnya hilang, minta user memilih ulang.
- Tampilan baca (riwayat, setelah jendela edit) menampilkan nama + warna label; baris lama menampilkan teksnya dengan tanda "catatan lama".
- Riwayat dan halaman submission menampilkan lencana review ("Menunggu review" / "Diterima" / "Ditolak" + alasan). Submission ditolak yang masih bisa diedit diberi ajakan "Perbaiki".
- Login menerima `403 ACCOUNT_BLOCKED` → pesan "Akun Anda diblokir. Hubungi admin."
- Submission yang dihapus admin (`404` pada submission yang tercatat di riwayat lokal) → "Submission ini sudah dihapus", entri lokalnya dibuang.

### 7.2 Dashboard admin
Bagian dari SPA yang sama di bawah `/admin`, **dimuat terpisah (lazy chunk)** sehingga bundle user tidak membesar. Token admin di `sessionStorage` (hilang saat tab ditutup, berbeda dari token user); `401` → kembali ke `/admin/masuk`.

| Rute | Isi |
|---|---|
| `/admin/masuk` | Isi nomor HP + Turnstile → layar kode 6 digit (komponen OTP user dipakai ulang: polling status, hitung mundur kirim ulang, banner kanal WhatsApp) |
| `/admin` | **Beranda / status sistem** (§5.10): kartu Database, SSD (bar pemakaian), Upload aktif, WhatsApp (tersedia/tidak, antrean, terkirim vs batas harian), Cleanup, suhu CPU. Kartu diberi warna ok / waspada / bahaya dengan ambang yang sama dengan alert Grafana (SSD > 80%, sisa < `min_free_bytes`, kanal WhatsApp tidak tersedia, antrean > 80%, OTP harian > 80%, suhu > 75°C). Di bawahnya ringkasan pekerjaan: "37 menunggu review", "5 akan terhapus retensi dalam 7 hari" (tautan ke antrean review) |
| `/admin/anotasi` | Daftar submission terbaru: kartu dengan foto sampul + overlay kotak, nama & nomor annotator, waktu, chip label beserta jumlah. Filter: rentang tanggal, label, status review, cari annotator (nama/nomor), "masih ada catatan lama". Pintasan **Antrean review** (`review_status=pending`, `sort=oldest`). Lencana status review dan "terhapus dalam N hari" di tiap kartu. Tombol "Muat lagi" (cursor). Tombol **Export JSONL** untuk filter tanggal yang sedang aktif. Kotak centang per kartu + tombol **Terima terpilih**, **Tolak terpilih** (satu alasan untuk semua), dan **Hapus terpilih** (maks 50) |
| `/admin/anotasi/:id` | Detail: semua foto dengan overlay kotak berwarna + nomor; daftar kotak (label, ukuran); kartu annotator (nama, email, nomor HP, tautan "semua dari annotator ini" = filter `user_id`); waktu dibuat/diubah, versi. Panel review: status saat ini, tombol **Terima** / **Tolak** (alasan wajib) / **Batalkan review**; tombol pintas Terima dan Tolak lalu buka submission berikutnya di antrean. Tombol **Hapus submission** dan tombol **Hapus foto** per foto |
| `/admin/annotator` | Hanya mode `otp`. Tabel annotator: nama, nomor, email, jumlah submission / kotak / diterima / ditolak, aktivitas terakhir, status blokir. Cari nama/nomor, filter aktif/diblokir |
| `/admin/annotator/:id` | Profil + statistik annotator, tautan ke submission-nya, tombol **Blokir** (alasan wajib) / **Buka blokir**, **Hapus data annotator** (pilihan "ikut hapus yang sudah diterima", konfirmasi ketik `HAPUS`), dan **Hapus akun** (akun + semua data, konfirmasi ketik `HAPUS`) |
| `/admin/statistik` | Statistik per label (§5.9): batang horizontal per label (warna label, urut terbanyak), angka kotak / foto / submission dan persentase; filter rentang tanggal dan status review (default: diterima). Label dengan 0 contoh ditampilkan di bawah |
| `/admin/label` | Tabel label: warna, nama, deskripsi, jumlah pemakaian, status. Tambah label (nama, deskripsi, warna), ubah inline, naik/turunkan urutan, arsip/buka arsip, hapus (hanya bila pemakaian 0; selain itu tombol berubah jadi "Arsipkan"). Peringatan saat mengganti nama label yang sudah dipakai |

- Teks dari user dan admin dirender sebagai teks (bukan HTML), sama dengan aturan §9.4 induk.
- **Konfirmasi hapus:** dialog yang menyebut apa yang akan hilang ("3 foto, 7 kotak anotasi milik Budi Santoso") dan bahwa tindakan ini **permanen dan tidak bisa dibatalkan**. Tombol konfirmasi berwarna bahaya dan baru aktif setelah admin mengetik `HAPUS`. Setelah berhasil, item hilang dari daftar dan muncul notifikasi hasil (termasuk `cleanup_pending` bila ada).
- Overlay kotak memakai komponen render yang sama dengan tampilan baca user (koordinat ternormalisasi).
- Export diunduh lewat `fetch` dengan header `Authorization`, lalu disimpan sebagai berkas (`photo-annotated-<from>-<to>.jsonl`); FE memeriksa trailer/`X-Export-Complete` bila tersedia dan memperingatkan bila export tidak lengkap.

### 7.3 Tata letak dashboard: desktop dulu, tetap nyaman di HP

Dashboard dirancang untuk **desktop** (admin bekerja di laptop/PC), tetapi semua halaman dan aksi **harus bisa dipakai penuh di HP** (lebar minimum 360 px), misalnya untuk mereview atau memblokir saat jauh dari komputer.

| | Desktop (≥ 1024 px) | Tablet (768–1023 px) | HP (< 768 px) |
|---|---|---|---|
| Navigasi | Sidebar kiri tetap (Beranda, Anotasi, Label, Annotator, Statistik) + nama admin & Keluar | Sidebar bisa diciutkan jadi ikon | Bar atas berisi judul halaman + tombol menu (drawer) |
| Daftar anotasi | Grid kartu 3–4 kolom; filter di satu baris di atas | Grid 2 kolom | 1 kolom; filter di panel "Filter" yang bisa dibuka-tutup, filter aktif tampil sebagai chip |
| Detail anotasi | Foto di kiri (besar), panel di kanan (annotator, review, daftar kotak) | Bertumpuk | Bertumpuk; foto selebar layar; tombol review **menempel di bawah layar** |
| Tabel (label, annotator) | Tabel biasa | Tabel, kolom sekunder disembunyikan | Berubah jadi daftar kartu; aksi baris di menu titik tiga (ikon `ellipsis-vertical`) |
| Aksi massal | Kotak centang + bar aksi di atas daftar | Sama | Mode "Pilih" (tekan lama / tombol Pilih), bar aksi menempel di bawah |
| Dialog | Modal di tengah | Modal | Layar penuh (sheet dari bawah) |
| Statistik label | Batang horizontal + tabel angka berdampingan | Bertumpuk | Batang horizontal, angka di bawah tiap batang |

Aturan umum:
- Tidak ada scroll horizontal di halaman pada lebar 360 px (tabel lebar diganti kartu, bukan digulir).
- Target sentuh minimal 44 × 44 px; tidak ada aksi yang hanya bisa lewat hover.
- Desktop mendapat pintasan keyboard di halaman detail/antrean review: `A` terima, `R` tolak (membuka isian alasan), `J`/`K` submission berikutnya/sebelumnya. Pintasan ditampilkan di tooltip tombol dan tidak aktif saat fokus di isian teks.
- Foto di kartu dan detail memakai `loading="lazy"` dan ukuran tampil sesuai lebar layar; overlay kotak tetap pas di semua ukuran (koordinat ternormalisasi).
- Diuji pada 360 × 800 (HP), 768 × 1024 (tablet), dan 1440 × 900 (desktop).

### 7.4 Tema terang (hangat) dan gelap

Berlaku untuk **seluruh aplikasi**: layar anotasi user dan dashboard admin.

- **Pilihan tema:** `Sistem` (default, mengikuti setelan perangkat), `Terang`, `Gelap`. Tombol pilihan ada di header aplikasi user dan di sidebar/menu dashboard. Pilihan disimpan di `localStorage` (`pa_theme`). Bila storage tidak bisa dipakai (mode privat), aplikasi tetap jalan dengan `Sistem`.
- **Tanpa kedip:** skrip kecil inline di `index.html` memasang `data-theme` di `<html>` sebelum CSS/JS dimuat, sehingga halaman tidak sempat tampil dengan tema yang salah. `<meta name="theme-color">` (warna bar browser di HP) ikut diganti sesuai tema.
- **Gelap = tema gelap yang sekarang** (latar hampir hitam, aksen hijau toska `#2bb3a3`). Nilainya dipertahankan.
- **Terang = terang hangat**, seperti mode malam/"night light": latar krem, teks cokelat tua, sedikit warna biru, sehingga nyaman di mata saat lama menganotasi. Usulan token awal (final disetel saat implementasi dengan cek kontras):

| Token | Terang hangat (baru) | Gelap (tetap) |
|---|---|---|
| `--bg` | `#f5ecdd` | `#111416` |
| `--surface` | `#fcf6ec` | `#1b1f22` |
| `--text` | `#2e2419` | `#e8eaed` |
| `--muted` | `#6b5a47` | `#9aa3ad` |
| `--border` (garis pemisah dekoratif) | `#e2d3bc` | `#2f353a` |
| `--border-strong` (tepi isian, dropdown, kotak centang; **token baru**) | `#8f785c` | `#6b737c` |
| `--primary` | `#1f6b5c` (hijau tua, senada dengan gelap) | `#2bb3a3` |
| `--primary-text` | `#fffaf1` | `#062b27` |
| `--primary-soft` | `#e3ecdc` | `#12302c` |
| `--danger` / `--danger-soft` | `#a33a22` / `#f8e1d6` | `#f97066` / `#3a1714` |
| `--warn` / `--warn-soft` | `#8a5300` / `#f9e8c4` | `#f5c451` / `#33290f` |
| `--ok` | `#3f7a2c` | `#4ade80` |

- **Kontras:** semua pasangan teks/latar memenuhi WCAG AA (≥ 4.5:1 untuk teks biasa, ≥ 3:1 untuk teks besar dan tepi kontrol) di kedua tema; dicek dengan test otomatis atas token CSS. Usulan terang hangat di atas sudah dihitung: teks 13–14:1, `--muted` 5.6:1, `--primary` 5.4:1, tombol primer 6.1:1, `--danger`/`--warn`/`--ok` 4.8–6.1:1, `--border-strong` 3.6–3.9:1. `--border` (≈ 1.4:1) hanya untuk pemisah dekoratif; tepi kontrol yang bisa diisi/diklik memakai `--border-strong` (di tema gelap juga, karena `--border` gelap sekarang pun di bawah 3:1).
- **Kotak anotasi:** warna kotak = warna label dari admin dan **sama di kedua tema** (warna adalah identitas label). Agar tetap terlihat di foto terang maupun gelap dan di kedua tema, kotak digambar dengan garis warna label + garis tepi tipis kontras (halo). Kotak terpilih dibedakan dengan garis lebih tebal dan pegangan (handle), **tidak hanya lewat warna**.
- Foto tidak pernah diberi filter warna oleh tema (anotasi harus melihat warna asli foto).
- Grafik statistik dan kartu status sistem memakai token tema, sehingga ikut berganti.

### 7.5 Ikon: tanpa emoji, pakai Font Awesome

- **Tidak ada emoji di UI** (header, tombol, label, pesan, status, notifikasi), baik di aplikasi user maupun dashboard. Emoji tampil berbeda di tiap HP/OS, tidak ikut warna tema, dan sulit diatur ukurannya. (Nama label yang diketik admin tidak dibatasi; ini aturan untuk teks bawaan aplikasi.)
- **Sumber ikon: Font Awesome Free 6** lewat npm, versi dikunci persis di `package-lock.json`: `@fortawesome/fontawesome-svg-core`, `@fortawesome/free-solid-svg-icons`, `@fortawesome/free-regular-svg-icons`, `@fortawesome/react-fontawesome`.
  - Ikon di-import satu per satu (`import { faCamera } from "@fortawesome/free-solid-svg-icons"`), sehingga hanya ikon yang dipakai yang masuk bundle (tree-shaking), dirender sebagai **SVG inline**.
  - Tanpa CDN dan tanpa web font: tidak ada request ke luar, cocok dengan CSP sekarang (`font-src 'self'`, `script-src` tanpa domain ikon), dan tetap jalan di jaringan seluler lambat.
  - Lisensi ikon Font Awesome Free: CC BY 4.0; atribusi sudah tertanam di berkas SVG/JS paketnya, tanpa tautan yang harus tampil di UI.
- **Flaticon hanya cadangan** bila ikon yang dibutuhkan tidak ada di Font Awesome Free: lisensi gratisnya mewajibkan atribusi yang terlihat. Bila terpaksa dipakai, SVG-nya disimpan di repo (`web/src/assets/icons/`), dicatat sumber + lisensinya di `web/ICONS.md`, dan atribusi ditampilkan di halaman "Tentang".
- **Aturan pakai:**
  - Ikon mengikuti warna teks (`currentColor`), jadi otomatis ikut tema terang/gelap (§7.4).
  - Tombol aksi = ikon + teks. Tombol hanya-ikon (mis. menu, tutup, naik/turun urutan) wajib punya `aria-label` dan tooltip.
  - Ikon dekoratif di samping teks diberi `aria-hidden="true"`.
  - Ukuran ikon 1em di dalam teks, 20 px di tombol hanya-ikon (area sentuh tetap ≥ 44 px, §7.3).
  - Status tidak hanya dibedakan lewat warna: ok / waspada / bahaya selalu ikon + teks.
- **Pemetaan ikon** (nama Font Awesome, gaya solid kecuali disebut lain):

| Tempat | Ikon |
|---|---|
| Logo header (ganti 📷) | `camera` |
| Ambil foto (ganti 📷) / Pilih dari galeri (ganti 🖼️) | `camera` / `images` |
| Mode menggambar kotak (ganti ✏️) / Mode gulir (ganti 👆) | `vector-square` / `hand` |
| Menu (HP) / menu baris | `bars` / `ellipsis-vertical` |
| Tema Sistem / Terang / Gelap | `circle-half-stroke` / `sun` / `moon` |
| Beranda / Anotasi / Label / Annotator / Statistik / Keluar | `gauge` / `images` / `tags` / `users` / `chart-simple` / `right-from-bracket` |
| Terima / Tolak / Batalkan review / Menunggu review | `check` / `xmark` / `rotate-left` / `clock` (regular) |
| Hapus / Blokir / Buka blokir | `trash-can` / `ban` / `lock-open` |
| Export / Filter / Cari / Muat ulang | `download` / `filter` / `magnifying-glass` / `rotate` |
| Naik / turun urutan label, arsip / buka arsip | `arrow-up` / `arrow-down`, `box-archive` / `box-open` |
| Status ok / waspada / bahaya | `circle-check` / `triangle-exclamation` / `circle-xmark` |
| Kartu status: Database / SSD / Upload / WhatsApp-OTP / Cleanup / Suhu CPU | `database` / `hard-drive` / `cloud-arrow-up` / `message` / `broom` / `temperature-half` |
| Annotator diblokir / anonim | `user-slash` / `user-secret` |

## 8. Keamanan & Observability

- Nomor superadmin hanya ada di `config.json` (file mode 640, tidak di-commit). `config.json.example` memakai nomor placeholder yang **ditolak** validasi.
- Dashboard menampilkan nomor HP annotator **utuh** (admin perlu bisa menghubungi), tetapi log tetap hanya memuat bentuk tersamar. Respons admin yang memuat data pribadi memakai `Cache-Control: no-store`.
- Setiap perubahan label dicatat sebagai baris log `msg="admin action"` dengan `action` (`label.create` / `label.update` / `label.archive` / `label.unarchive` / `label.reorder` / `label.delete` / `submission.delete` / `photo.delete` / `submission.review` / `user.block` / `user.unblock` / `user.delete_data` / `user.delete_account`), id yang terkait, jumlah foto/anotasi/file yang dihapus, dan `request_id`. Karena tidak ada audit log di DB, baris log ini (Loki, retensi 30 hari) adalah satu-satunya jejak siapa menghapus apa. Login admin berhasil/gagal dicatat (`admin login`, nomor tersamar). Nama label boleh di-log (bukan data pribadi).
- Pesan WhatsApp login admin memberi tahu bahwa itu kode **admin**, sehingga superadmin sadar bila ada yang mencoba masuk.
- Test log hygiene (e2e) diperluas: alur login admin + kelola label tidak boleh membocorkan kode OTP, token `adm2`, atau nomor utuh.
- Alert baru opsional: lebih dari 5 permintaan OTP admin per jam (indikasi percobaan).
- Alasan review dan alasan blokir ditulis admin dan dibaca user (alasan review): dirender sebagai teks, tanpa HTML. Alasan tidak di-log (bisa memuat nama orang).
- `GET /admin/system` tidak memuat data pribadi maupun secret (tidak ada URL DB, token, atau nomor).

## 9. Acceptance Criteria

1. API menolak start bila `admin.superadmin.phone` kosong/tidak valid, atau bila `admin.username`/`password_bcrypt` masih ada.
2. Login admin: nomor superadmin menerima kode WhatsApp dan mendapat token `adm2`; nomor lain mendapat respons dan perilaku polling yang identik tanpa pesan terkirim; `POST /admin/login` → `404`. Berlaku pada `auth.mode = none` dan `otp`.
3. Mengganti `superadmin.phone` + restart membatalkan token admin lama (`401`).
4. Admin dapat membuat, mengganti nama, mengubah warna, mengurutkan, mengarsipkan, membuka arsip, dan menghapus label yang belum dipakai; menghapus label terpakai → `409 LABEL_IN_USE`; nama duplikat (beda huruf besar-kecil) → `409`.
5. Token user tidak bisa mengakses `/admin/*` (`401`), token admin tidak bisa submit/edit (`403 ADMIN_READ_ONLY`).
6. `POST /submissions` dengan `text` → `400`; dengan label arsip → `422 LABEL_ARCHIVED`; dengan label aktif → tersimpan dan `GET` mengembalikan nama + warna label.
7. `PUT` mempertahankan kotak berlabel arsip yang sudah ada di foto itu → diterima; menambah kotak baru dengan label arsip → `422`.
8. Ganti nama label langsung terlihat di `GET /submissions/{id}`, dashboard, dan export berikutnya.
9. Dashboard menampilkan daftar submission dengan annotator (nama, email, nomor) dan foto sampul beroverlay; filter tanggal/label/annotator bekerja; detail menampilkan semua foto dan kotak.
10. Export memuat `label_id` + `label`; baris lama memuat `text` dengan `label_id: null`; tidak ada data pribadi.
11. Data teks bebas lama tetap terbaca di semua tampilan setelah migrasi.
12. Bundle user tidak memuat kode dashboard (chunk terpisah).
13. Admin menghapus submission → baris submission, foto, anotasi, dan file di SSD hilang; user pemilik mendapat `404`; export tidak memuatnya. Hapus satu foto → foto + file hilang, foto lain tetap, `version` naik; hapus foto terakhir → submission ikut hilang.
14. Bila hapus file gagal (SSD disimulasikan error), respons `202 cleanup_pending`, data tidak terlihat di mana pun, dan cleanup berikutnya menghapus sisa file dan barisnya. Tidak pernah ada baris yang menunjuk ke file yang sudah dihapus.
15. Edit user yang berbarengan dengan hapus admin berakhir `404` atau `409`, tidak pernah menghidupkan kembali data yang dihapus.
16. Review: terima/tolak/batal dengan `version` lama → `409`; tolak tanpa alasan → `422`; review massal melaporkan hasil per item. User melihat status + alasan di riwayat. Edit user yang berhasil mengembalikan status ke `pending`.
17. Retensi: submission `accepted` berumur > 90 hari tidak terhapus cleanup; submission `pending`/`rejected` seumur itu terhapus; membatalkan penerimaan submission > 90 hari membuatnya terhapus pada putaran berikutnya.
18. Export `review_status=accepted` hanya memuat submission yang diterima; tiap baris memuat `review_status`.
19. Blokir annotator: sesi aktif langsung `401`; login dengan password benar → `403 ACCOUNT_BLOCKED`, password salah → `401` biasa; register/lupa password tidak mengirim WhatsApp; buka blokir memulihkan login. Hapus data annotator mempertahankan submission `accepted` kecuali `include_accepted=true`, dan semua filenya hilang dari SSD setelah cleanup.
20. Pada `auth.mode = none`, `/admin/users*` → `404` dan menu Annotator tidak tampil.
21. Statistik label cocok dengan hitungan manual pada data uji (termasuk label 0 contoh, filter tanggal dan status review).
22. Status sistem menampilkan SSD tidak tersedia, kanal WhatsApp mati, dan DB gagal dengan benar (diuji dengan fake), tanpa membuat seluruh respons gagal.
23. Dashboard bisa dipakai penuh di 360 px (semua aksi: login, review, hapus, blokir, label, statistik) tanpa scroll horizontal; di 1440 px memakai sidebar dan grid multi-kolom.
24. Pilihan tema Sistem/Terang/Gelap bekerja di aplikasi user dan dashboard, tersimpan setelah reload, tanpa kedip tema salah saat halaman dibuka; tema terang memakai palet hangat, tema gelap sama dengan yang sekarang; kontras AA lolos di kedua tema.
25. Tidak ada emoji di teks UI aplikasi user maupun dashboard (dicek test yang memindai `web/src` untuk karakter emoji di luar komentar); semua ikon berasal dari Font Awesome dan tidak ada request ikon/font ke domain luar.
26. `go test ./... -race` dan test FE hijau; e2e log hygiene mencakup alur admin.

## 10. Keputusan

Dikonfirmasi 24 Okt 2026:

1. **Label diisi admin, user memilih lewat dropdown.** Kotak hanya berisi label; tidak ada catatan teks bebas (§7.1).
2. **Satu superadmin saja** (`admin.superadmin: {…}` di config).
3. **Admin tidak bisa mengedit anotasi user.** Admin tidak bisa mengubah label pada anotasi milik user.
4. **Nomor HP annotator tampil utuh** di dashboard (log tetap tersamar).
5. **Login admin cukup OTP**, tanpa password.
6. **Admin bisa menghapus permanen** submission atau foto yang sudah dianotasi: hard delete dari database dan dari SSD (§5.5).
7. **Review submission** (terima / tolak) oleh admin (§5.7).
8. **Submission yang diterima tidak ikut terhapus retensi 90 hari** (§5.7).
9. **Kelola annotator**: daftar, blokir / buka blokir, hapus data, hapus akun (§5.8; hapus akun ditambahkan 25 Okt atas permintaan Anda).
10. **Statistik per label saja** (§5.9).
11. **Status sistem di dashboard** (§5.10).
12. **Dashboard untuk desktop, tetap bisa dipakai penuh di HP** (§7.3).
13. **Tema terang hangat (seperti mode malam) dan gelap** (gelap = hijau gelap yang sekarang), bisa dipilih user (§7.4).
14. **Tanpa emoji**; ikon dari Font Awesome Free (Flaticon hanya cadangan) (§7.5).

Default yang dipakai (belum dibahas, koreksi bila perlu):

- Review berlaku per submission utuh; edit user mereset review ke `pending`; user melihat status + alasan review.
- Hapus data annotator default mempertahankan submission yang sudah diterima.
- Annotator yang diblokir tidak bisa mendaftar ulang dengan nomor yang sama.

15. **Data teks bebas lama dibiarkan** sampai habis oleh retensi 90 hari, tidak dipetakan ke label.
16. **Satu daftar label global** untuk semua foto dan semua user.

## 11. Bagian spec induk yang berubah

| Bagian `spec.md` | Perubahan |
|---|---|
| §1, §2 | Teks bebas → label terkontrol; "daftar label terkontrol" dihapus dari Non-Goals |
| §3.2 | "Teks anotasi disimpan verbatim, pemetaan ke kelas saat export" → `label_id` stabil sebagai kelas; teks hanya untuk data lama |
| §4.1 langkah 7 | Pilih label per kotak |
| §5.2–5.4 | Bentuk anotasi `label_id` |
| §5.7 | Field `label_id`, `label`, `text`; filter `label_id` |
| §5.9 | Diganti §3 dan §5.2–5.4 dokumen ini |
| §6.3, §6.4 | Aturan teks → aturan label; Submit menunggu semua kotak berlabel |
| §7.1, §7.3, §7.5 | §6 dokumen ini (`submissions.deleted_at`); "Tidak ada tabel `labels`" dihapus |
| §9.1, §9.2, §9.4 | Admin via OTP, matriks akses ditambah `/labels` dan `/admin/*`, CORS `DELETE` |
| §8.3 | Langkah cleanup baru: hapus submission ber-`deleted_at` yang tidak punya foto lagi; retensi melewati submission `accepted` |
| §7.4, §5.8 (`/auth/login`) | `users.blocked_at`; `403 ACCOUNT_BLOCKED`; register/forgot untuk nomor diblokir → `202` palsu |
| §12 | Status sistem di dashboard melengkapi Grafana |
| §14.2 #1 | Terjawab: dataset = submission `accepted`, tidak ikut retensi |
| §11.4 | Bagian `admin` baru, `limits.max_labels`, `max_annotation_text_len` dihapus |
| §14.1 #3, #15 | Keputusan label dan admin diganti |

## 12. Rencana task (usulan)

Melanjutkan penomoran `docs/tasks/todo.md`. Satu commit per task.

| Task | Isi | Bergantung |
|---|---|---|
| **T37** | Config `admin.superadmin` + validasi; migrasi `otp_challenges` (`admin_login`); endpoint `/admin/otp/*`, `/admin/me`, `/admin/channel-status`; token `adm2`; hapus `/admin/login`; dispatcher berjalan juga di mode `none` bila admin aktif | – |
| **T38** | Migrasi `labels` + `annotations.label_id`; domain label; `GET /labels`; CRUD `/admin/labels` + urutan; CORS `DELETE` | T37 |
| **T39** | Submit/edit/get memakai `label_id` (aturan arsip saat edit), export berlabel, tampilan data lama | T38 |
| **T40** | `GET /admin/submissions` (filter, cursor, cover) + `GET /admin/submissions/{id}` | T39 |
| **T41** | FE user: pemilih label, warna kotak, migrasi draft, penanganan `LABEL_ARCHIVED` | T39 |
| **T42** | FE admin: login OTP, layout lazy-loaded **responsif** (sidebar desktop, drawer HP, §7.3), halaman label | T38, T52, T53 |
| **T43** | FE admin: daftar & detail anotasi, export | T40, T42 |
| **T44** | Backend hapus permanen: `submissions.deleted_at`, `DELETE /admin/submissions/{id}`, `DELETE /admin/photos/{id}`, hapus massal, langkah cleanup baru, log `admin action` | T40 |
| **T45** | FE admin: hapus submission/foto/massal dengan konfirmasi; FE user: pesan "sudah dihapus" | T43, T44 |
| **T46** | Backend review: kolom review, `PUT /admin/submissions/{id}/review` + massal, reset saat edit user, `review` di `GET /submissions/{id}`, filter/sort di daftar admin, export `review_status`, retensi melewati `accepted` | T40 |
| **T47** | Backend kelola annotator: `users.blocked_*`, `/admin/users*`, blokir (cabut sesi, `ACCOUNT_BLOCKED`, register/forgot palsu), hapus data annotator | T44 |
| **T48** | Backend `GET /admin/stats/labels` + `GET /admin/system` (versi build, suhu CPU, status cleanup) | T46 |
| **T49** | FE review: panel review + antrean di dashboard, aksi massal; lencana review + "Perbaiki" di sisi user; pesan akun diblokir | T43, T46 |
| **T50** | FE admin: halaman annotator (daftar, profil, blokir, hapus data) | T42, T47 |
| **T51** | FE admin: beranda status sistem + halaman statistik label | T42, T48 |
| **T52** | FE tema: token terang hangat + gelap, pilihan Sistem/Terang/Gelap, skrip anti-kedip, `theme-color`, kotak dengan halo, test kontras (§7.4). Tidak bergantung backend, bisa dikerjakan lebih dulu | – |
| **T53** | FE ikon: pasang Font Awesome Free (npm, import per ikon), ganti 5 emoji yang ada (header, pilih foto, mode gambar/gulir), komponen `Icon` dengan aturan aksesibilitas, test pemindai emoji (§7.5). Dikerjakan bersama/sebelum T52; task FE lain memakai ikon dari sini | – |
| Checkpoint 9 | Deploy ke Pi (backend + FE **bersamaan**, karena kontrak submit berubah), uji login admin ke nomor asli, buat label, anotasi dari HP, review di dashboard, blokir & buka blokir akun uji, hapus satu submission uji dan pastikan filenya hilang dari SSD, cek status sistem dan statistik | T37–T51 |
