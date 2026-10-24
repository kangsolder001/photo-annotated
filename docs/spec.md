# Spec: Form dengan Upload & Anotasi Foto

> Status: Draft v0.5
> Target deploy: Raspberry Pi 4 (backend API + PostgreSQL shared + wa-notif + foto di SSD lokal) + CDN (frontend)
>
> Perubahan v0.5: foto disimpan di **SSD lokal Raspi** (bukan Cloudflare R2 / object storage eksternal); upload lewat URL bertanda tangan ke API yang men-stream byte ke disk; isi file divalidasi di server; kontrak presign ke FE tetap sama. Dasar keputusan: Cloudflare R2 butuh kartu kredit, dan internet Pi terukur ±187 Mbps download / ±179 Mbps upload.
>
> Perubahan v0.2: arsitektur Go diubah ke Clean Architecture + Gin; open questions §14 dijawab (auth configurable, form free text, label bebas, retensi 3 bulan, export untuk model, edit oleh pemilik).
>
> Perubahan v0.4: jendela edit 24 jam; admin memakai akun statis di `config.json` (independen dari mode auth); kegagalan pengiriman OTP WhatsApp ditampilkan di FE; open questions dijawab; hanya nomor Indonesia.
>
> Perubahan v0.3: registrasi dengan nama, email, dan nomor HP, diverifikasi OTP yang dikirim lewat WhatsApp (service `wa-notif` yang sudah berjalan di Raspi); PostgreSQL dijalankan sebagai satu container shared di `/home/myrpi/docker/postgres` untuk banyak service.

## 1. Ringkasan

Aplikasi web untuk mengunggah satu atau lebih foto, lalu membuat anotasi berupa bounding box (kotak) di atas foto, dengan **teks bebas pada setiap anotasi**. Tidak ada field form terstruktur: isi submission adalah foto + anotasi + teks anotasi.

Semua pekerjaan berat (ambil foto, resize/kompresi, anotasi) dilakukan di browser. Foto di-upload dari browser ke API di Raspberry Pi memakai **URL bertanda tangan berumur pendek**, lalu API men-stream byte-nya langsung ke **SSD lokal** (tanpa di-buffer di memori dan tanpa diproses ulang). Data anotasi disimpan terstruktur di PostgreSQL agar bisa diekspor ke format yang dapat dibaca model.

## 2. Goals & Non-Goals

### Goals
- Mendukung **500–1000 user aktif bersamaan** di atas satu Raspberry Pi 4. Throughput foto masuk dibatasi bandwidth internet rumah: dengan ±187 Mbps download (terukur) dan foto ±400 KB, target berkelanjutan **±30 foto/detik** (batas teoretis ±58).
- Backend hanya me-**stream** byte foto ke disk: tidak di-buffer di memori (memori per upload konstan, tidak bergantung ukuran file), tidak di-resize, dan tidak di-decode pikselnya (hanya header dibaca untuk validasi).
- Anotasi disimpan sebagai data terstruktur (bisa di-query, di-render ulang, di-export).
- Pemilik submission bisa mengedit submission-nya setelah dikirim.
- Autentikasi bisa diatur lewat `config.json`: `none` (anonim) atau `otp` (registrasi nama + email + nomor HP, verifikasi OTP via WhatsApp).
- Satu instance PostgreSQL di Raspi dipakai bersama banyak service (database + role terpisah per service).
- Data siap diekspor ke format yang bisa dibaca model (JSONL, lihat §5.7).
- Bekerja dengan baik di mobile browser (kamera belakang, koneksi lambat).

### Non-Goals (untuk v1)
- Image processing di server (thumbnail generation, ML inference, dsb).
- Redundansi/replikasi foto: v1 memakai satu SSD; perlindungan data lewat backup (§11.5).
- Object storage eksternal (S3/R2/B2): ditunda; port `PhotoStore` memungkinkan menambahkannya nanti (§3.1).
- Bentuk anotasi selain rectangle (polygon, freehand, keypoint).
- Field form terstruktur dan daftar label terkontrol.
- Export format COCO / YOLO (hanya disiapkan lewat interface, lihat §3.2).
- Verifikasi email (email hanya data kontak; identitas diverifikasi lewat nomor HP).
- Channel OTP selain WhatsApp (SMS, email) dan password.
- Editor anotasi kolaboratif / real-time.
- Offline mode penuh (PWA dengan sync queue) — kandidat v2.

## 3. Arsitektur

```
                         ┌────────────────────────────┐
                         │  CDN / Cloudflare Pages     │
                         │  (static FE: HTML/JS/CSS)   │
                         └──────────────┬─────────────┘
                                        │ load app
                                        ▼
┌──────────────┐  (1) presign    ┌──────────────────────────────────┐
│   Browser    │ ──────────────▶ │ Cloudflare Tunnel                │
│              │ ◀────────────── │        ▼                         │
│ - kamera     │  URL bertanda   │ Raspberry Pi 4                   │
│ - resize     │  tangan         │  Go API (Gin)                    │
│ - anotasi    │ (2) PUT foto    │   ├─▶ SSD lokal (foto, ext4)     │
│              │ ──────────────▶ │   ├─▶ shared-postgres            │
│              │ (3) submit      │   └─▶ wa-notif :3030 ─▶ WhatsApp │
│              │ ──────────────▶ │                                  │
└──────────────┘                 └──────────────────────────────────┘
```

### Komponen

| Komponen | Teknologi | Lokasi | Tanggung jawab |
|---|---|---|---|
| Frontend | SPA statis (mis. React/Svelte + Vite) | CDN | Kamera, resize, anotasi, upload, edit |
| Library anotasi | Annotorious (alternatif: Konva.js) | FE | Gambar & edit bounding box + teks |
| API | Go (≥ 1.24), **Gin**, `pgx`, Clean Architecture | Raspi | URL bertanda tangan, terima & validasi upload foto, simpan & edit submission, layani foto, export |
| Database | **PostgreSQL 17 shared** (container `shared-postgres`, §11.1) | Raspi | User, OTP, session, submission, foto, anotasi |
| WhatsApp gateway | `wa-notif` (Go + whatsmeow, sudah berjalan, port 3030) | Raspi | Mengirim pesan OTP lewat `POST /api/v1/messages/send` |
| Penyimpanan foto | Filesystem lokal (ext4) di **SSD USB 3.0**, di-mount ke container `api` (§8) | Raspi | Simpan file foto |
| Ingress | Cloudflare Tunnel (`cloudflared`) | Raspi | Expose API tanpa port forwarding, TLS, rate limit edge |
| Observability | Promtail → Loki → Grafana | Raspi (`pisee`) | Log & dashboard |

**Aturan penting:** FE bundle tidak boleh di-serve dari Raspi. Bandwidth internet rumah (terukur ±187 Mbps down / ±179 Mbps up, ping ±20 ms) dipakai untuk upload foto, jadi tidak boleh dihabiskan untuk file statis.

### 3.1 Clean Architecture (backend)

Aturan dependensi: **panah selalu mengarah ke dalam**. `domain` tidak meng-import apa pun dari luar; `usecase` hanya mengenal `domain` dan interface (port) yang ia definisikan sendiri; `adapter` dan `delivery` mengimplementasikan/memanggil port.

```
delivery/http (Gin) ──▶ usecase ──▶ domain
adapter/postgres ─────▶ (implements usecase ports)
adapter/localfs, signer, wanotif, auth, turnstile ──▶ (implements usecase ports)
```

Struktur direktori:

```
cmd/
  api/main.go            # composition root: load config, wiring manual (DI), start server + cleanup job
  migrate/main.go        # jalankan migrasi DB (terpisah dari server)
internal/
  domain/                # entity: User, OTPChallenge, Session, Submission, Photo, Annotation, Principal
                         # aturan murni: normalisasi/validasi nomor HP, validasi bbox & teks,
                         #               status foto, status OTP, error domain
  usecase/
    ports.go             # interface: UserRepo, OTPRepo, SessionRepo, SubmissionRepo, PhotoRepo,
                         #            PhotoStore, URLSigner, OTPSender, TxManager,
                         #            TurnstileVerifier, Clock, TokenSigner, Exporter
    register.go  otp_request.go  otp_verify.go  otp_dispatch.go  session.go
    presign.go  upload.go  submit.go  edit.go  get.go  photourl.go  photo_content.go
    export.go  cleanup.go
  adapter/
    postgres/            # implementasi repo (pgx) + TxManager
    localfs/             # implementasi PhotoStore: tulis aman (temp→fsync→rename), Open, Stat, Delete,
                         #   free space; memakai os.Root agar path tidak bisa keluar dari root
    signer/              # implementasi URLSigner: HMAC-SHA256 untuk URL upload & lihat foto
    wanotif/             # implementasi OTPSender: HTTP client ke wa-notif
    auth/                # Authenticator: none (edit_token), otp (session bearer token)
    turnstile/           # verifikasi token Turnstile
    export/              # implementasi Exporter (jsonl)
  delivery/http/
    router.go            # gin.Engine, grouping route, middleware
    handler/             # handler tipis: bind → panggil usecase → map hasil
    dto/                 # request/response JSON (terpisah dari entity domain)
    middleware/          # request_id, slog logger, recovery, auth, rate limit, body limit
  config/                # load & validasi config.json + override env
migrations/              # SQL migrasi (golang-migrate atau goose)
config.json.example
```

Aturan implementasi:
- **Handler tipis.** Handler Gin hanya bind + validasi bentuk request, memanggil satu usecase, lalu memetakan hasil/error. Tidak ada logika bisnis dan tidak ada akses DB/disk langsung di handler.
- **Error domain → HTTP** dipetakan di satu tempat di `delivery/http` (mis. `ErrNotFound`→404, `ErrForbidden`→403, `ErrVersionConflict`→409, `ErrValidation`→422). Usecase tidak mengenal kode HTTP.
- **Penyimpanan foto di balik port `PhotoStore`** (`Save`, `Open`, `Stat`, `Delete`, `FreeSpace`). v1 hanya punya adapter `localfs` (`storage.driver = "local"`). Jika nanti pindah ke S3/B2, port diperluas dengan presigned URL dan FE tidak berubah, karena kontrak `presign` tetap sama (hanya host pada `url` yang berbeda).
- **Transaksi** lewat port `TxManager.WithTx(ctx, func(ctx) error)`; usecase menentukan batas transaksi, adapter postgres mengimplementasikannya.
- **Otorisasi (owner/admin)** diputuskan di usecase. Middleware auth hanya mengisi `domain.Principal` ke context.
- **Gin:** `gin.SetMode(gin.ReleaseMode)`, `gin.New()` (bukan `Default()`) dengan middleware sendiri. IP klien ditentukan oleh middleware sendiri: header `CF-Connecting-IP` hanya dipercaya jika koneksi berasal dari `app.trusted_proxies` (alamat/CIDR `cloudflared`); selain itu dipakai alamat koneksi langsung. `TrustedPlatform` bawaan Gin tidak dipakai karena mempercayai header itu dari siapa pun. Body request JSON dibatasi 64 KB lewat `http.MaxBytesReader`; rute upload foto memakai batas sendiri (§5.1b).
- **Pengujian:** usecase diuji dengan fake port (tanpa DB/disk); repo postgres diuji terhadap database uji (`TEST_DATABASE_URL`); `localfs` diuji di direktori sementara (`t.TempDir()`); handler diuji dengan `httptest`.

### 3.2 Jalur ekstensi export

Export dimodelkan sebagai port `Exporter` (`Format() string` dan `Write(ctx, io.Writer, iter)`), dengan satu implementasi di v1: `jsonl`. Menambah COCO/YOLO nanti berarti menambah implementasi baru, tanpa mengubah usecase atau schema.

Agar data yang disimpan sekarang selalu bisa diekspor ke format model yang dibutuhkan nanti, penyimpanan menjaga aturan berikut:
- Koordinat disimpan **apa adanya dalam bentuk ternormalisasi** (`x,y,w,h`), bersama dimensi foto hasil proses (`photos.width/height`). Dari keduanya bisa dihitung piksel, COCO (`[x,y,w,h]` piksel), atau YOLO (pusat + ukuran ternormalisasi) tanpa kehilangan informasi.
- Teks anotasi disimpan verbatim (hanya `trim`), tanpa dipetakan ke kelas. Pemetaan teks → kelas dilakukan saat export, bukan saat simpan.
- Foto disimpan dalam format hasil proses FE (WebP/JPEG, sisi terpanjang ≤ 1920) dengan `storage_key` stabil (path relatif di bawah root SSD); anotasi tidak pernah di-burn ke gambar. `sha256` tiap foto dicatat untuk verifikasi integritas dan deteksi duplikat.
- Urutan foto (`position`) dan `created_at` dipertahankan.
- **Perhatian:** retensi 90 hari menghapus foto dan anotasi (§8.3). Export v1 hanya menghasilkan referensi (`storage_key`/URL sementara), bukan salinan permanen. Karena foto ada di SSD lokal, menyalin dataset cukup dengan `rsync` dari root foto memakai `storage_key` dari export; untuk dataset yang dipakai lebih lama dari 90 hari, salinan itu harus dibuat sebelum retensi berjalan (§14.2).

## 4. Alur Utama

### 4.0 Registrasi & login (hanya `auth.mode = otp`)

Registrasi dan login memakai alur OTP yang sama, tanpa password.

1. User mengisi **nama, email, dan nomor HP** lalu submit → `POST /auth/register` (Turnstile wajib, §9.5).
2. Jika kanal WhatsApp sedang tidak tersedia (§9.5), API langsung membalas `503 OTP_UNAVAILABLE` tanpa membuat challenge. Jika tersedia, API menormalisasi nomor HP (§7.4), membuat `users` berstatus `pending` (atau memakai user yang sudah ada), membuat `otp_challenges` berstatus `queued`, lalu langsung membalas `202` dengan `challenge_id`. **Pengiriman WhatsApp tidak dilakukan di dalam request.**
3. Worker dispatcher (satu goroutine di proses API) mengambil challenge `queued` satu per satu, lalu memanggil `wa-notif` (`POST /api/v1/messages/send`). Berhasil → status `sent`, `sent_at` dan `expires_at = sent_at + 5 menit` diisi. Gagal → retry maksimal 3 percobaan dengan jeda singkat (3 detik, 10 detik) agar user cepat tahu, lalu status `failed` dengan `failure_reason` (§9.5): `WA_UNAVAILABLE` (gateway tidak terhubung/timeout) atau `SEND_FAILED`. Dispatcher dibangunkan segera setelah challenge diantrekan (selain memeriksa antrean tiap detik). Klaim memakai `FOR UPDATE SKIP LOCKED`, jadi satu challenge tidak pernah dikirim berbarengan. Bila proses mati di tengah kirim, challenge `sending` diantrekan ulang setelah 3× timeout `wa-notif` dan bisa terkirim dua kali, **dengan kode yang sama** (kode diturunkan dari `challenge_id`, §9.5); percobaan yang sudah habis langsung `failed` tanpa kirim. Saat shutdown, challenge yang sedang dikirim dikembalikan ke antrean.
4. FE memantau hasil pengiriman lewat `GET /auth/otp/{challenge_id}` (tiap 2 detik): `queued` → "menunggu giliran", `sent` → "kode dikirim, cek WhatsApp Anda", `failed` → pesan gagal sesuai `failure_reason` (mis. "Layanan WhatsApp sedang bermasalah, coba lagi beberapa saat") dan tombol kirim ulang langsung aktif. User menerima pesan WhatsApp berisi kode 6 digit, memasukkannya di FE → `POST /auth/otp/verify`.
5. Kode benar → user menjadi `active` (`phone_verified_at` diisi), API membuat session dan mengembalikan `access_token` (bearer, berlaku 30 hari).
6. Login berikutnya (perangkat baru, token habis): user hanya memasukkan nomor HP → `POST /auth/otp/request` → verifikasi OTP seperti di atas.
7. FE menyimpan `access_token` dan mengirimnya sebagai `Authorization: Bearer <token>` di semua request lain. Menerima `401` → FE kembali ke layar login.

Catatan kapasitas: `wa-notif` mengirim pesan **satu per satu dengan jeda 1,5 detik** (`rate_limit_seconds`), yaitu sekitar 40 OTP per menit. Karena itu antrean dan pembatasan pada §9.5 wajib ada, dan masa berlaku OTP dihitung dari saat terkirim, bukan saat diminta.

### 4.1 Buat submission

1. User membuka app (FE dari CDN). Pada `auth.mode = otp`, user harus sudah login (§4.0); pada `none`, FE memakai Turnstile (§9).
2. User mengambil/memilih foto.
3. FE memproses foto (lihat §6): koreksi orientasi, resize, encode ulang.
4. FE memanggil `POST /api/v1/uploads/presign` dengan `content_type`, `size_bytes`, `width`, `height` hasil proses.
5. API memvalidasi, membuat record `photos` berstatus `pending`, lalu mengembalikan **URL upload bertanda tangan** yang menunjuk ke API sendiri (§5.1).
6. FE melakukan `PUT` ke URL itu. API men-stream body ke SSD, memvalidasi isi file (§5.1b), lalu mencatat `uploaded_at`. Progress ditampilkan ke user.
7. User menggambar kotak di atas foto dan mengisi **teks bebas** untuk tiap kotak. Anotasi bisa dibuat sebelum atau sesudah upload selesai.
8. User menekan Submit. FE memanggil `POST /api/v1/submissions` berisi daftar `photo_id` beserta anotasi (koordinat + teks).
9. API memverifikasi setiap foto (sudah ter-upload: `uploaded_at` terisi dan file ada di disk dengan ukuran yang cocok), lalu menyimpan submission, foto, dan anotasi dalam **satu transaksi**. Status foto berubah menjadi `attached`.
10. Pada `auth.mode = none`, response berisi `edit_token` yang disimpan FE di `localStorage` untuk edit di kemudian hari (§4.4).

### 4.2 Upload gagal / user batal
- Jika PUT gagal, FE retry maksimal 3 kali dengan exponential backoff untuk kegagalan jaringan, `408`, `503 UPLOAD_BUSY`, `409 UPLOAD_IN_PROGRESS`, dan `422 SIZE_MISMATCH` (hormati `Retry-After`). Tidak diulang: `403`, `410` (minta presign baru), `413`, `422 INVALID_IMAGE`/`INVALID_CONTENT_TYPE`, `409 PHOTO_NOT_AVAILABLE`. PUT ulang pada foto yang masih `pending` aman: file ditimpa secara atomik. Jika URL expired, FE meminta presign baru. `503 STORAGE_FULL` / `STORAGE_UNAVAILABLE` tidak di-retry; FE menampilkan pesan bahwa layanan sedang penuh/bermasalah.
- Foto yang tetap `pending` lebih dari 24 jam dihapus oleh cleanup job (§8.3).

### 4.3 Melihat submission
- Foto **tidak pernah publik**. FE memanggil `GET /api/v1/photos/{id}/url` (dicek pemilik/admin) dan menerima URL bertanda tangan berumur 5 menit ke `GET /api/v1/photos/{id}/content`, yang bisa langsung dipakai di `<img src>` tanpa header Authorization.
- Yang boleh melihat: pemilik submission atau admin (§9.2).

### 4.4 Edit setelah submit
- Hanya **pemilik** (user yang mengunggah foto) yang boleh mengedit. Admin hanya bisa membaca.
- FE memuat submission lewat `GET /submissions/{id}`, user mengubah anotasi/teks, menambah foto baru (alur presign biasa), atau menghapus foto, lalu FE memanggil `PUT /submissions/{id}` berisi **keadaan akhir** (bukan diff) dan `version` terakhir yang dilihat.
- Foto yang ada sebelumnya tetapi tidak ada di request menjadi `deleted`, dan filenya dihapus oleh cleanup job.
- Konflik (`version` tidak cocok, mis. edit dari dua tab) → `409 VERSION_CONFLICT`.
- **Jendela edit 24 jam** sejak `created_at` submission (`edit.window`, default `24h`). Edit tidak memperpanjang jendela. Setelah itu `PUT` ditolak `403 EDIT_WINDOW_CLOSED`; submission tetap bisa dibaca sampai retensi (§8.3).

## 5. API

Base path: `/api/v1`. Semua request & response berformat JSON, kecuali export (NDJSON), upload foto (byte mentah), dan konten foto. Error memakai format:

```json
{ "error": { "code": "INVALID_CONTENT_TYPE", "message": "content_type must be image/webp or image/jpeg" } }
```

### 5.1 `POST /uploads/presign`

Request:
```json
{
  "content_type": "image/webp",
  "size_bytes": 412345,
  "width": 1920,
  "height": 1440
}
```

Validasi:
- `content_type` ∈ {`image/webp`, `image/jpeg`}
- `0 < size_bytes ≤ 2 MB` (setelah kompresi di FE)
- `width`, `height` > 0 dan sisi terpanjang ≤ 2048

Response `201`:
```json
{
  "photo_id": "0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11",
  "upload": {
    "method": "PUT",
    "url": "https://api.example.com/api/v1/uploads/0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11?exp=1761214500&sig=...",
    "headers": { "Content-Type": "image/webp" },
    "expires_at": "2026-10-23T10:15:00Z"
  }
}
```

Catatan:
- `storage_key` **selalu di-generate server**: `{yyyy}/{mm}/{dd}/{uuid}.{ext}` (relatif terhadap root SSD, §8.1). Client tidak pernah menentukan key.
- Expiry URL upload: 10 menit (`storage.upload_url_ttl`).
- Tanda tangan: `sig = base64url(HMAC-SHA256(STORAGE_SIGNING_SECRET, "upload\n" ‖ photo_id ‖ "\n" ‖ exp ‖ "\n" ‖ content_type ‖ "\n" ‖ size_bytes))`. Mengubah ukuran atau tipe membuat tanda tangan tidak valid.
- Jika sisa ruang SSD di bawah `storage.local.min_free_bytes` atau SSD tidak ter-mount, presign ditolak `503 STORAGE_FULL` / `503 STORAGE_UNAVAILABLE` (§8.2).
- Foto mencatat `owner_id` pembuatnya (pada mode dengan auth) agar tidak bisa dipakai submission milik orang lain.

### 5.1b `PUT /uploads/{photo_id}?exp=…&sig=…`

Menerima **byte foto mentah** (bukan multipart). Tidak memakai header `Authorization`: otorisasi ada pada tanda tangan URL, yang hanya diterbitkan setelah pengecekan auth/Turnstile di presign.

Header wajib: `Content-Type` **persis** sama dengan deklarasi (tanpa parameter, huruf kecil) dan `Content-Length` sama dengan `size_bytes`. Body chunked (tanpa `Content-Length`) ditolak `411 LENGTH_REQUIRED`.

Pemrosesan di server, berurutan:
1. **Otorisasi lebih dulu.** `sig` tidak valid, `exp`/`sig` hilang atau cacat, **dan id foto yang tidak dikenal** semuanya dijawab sama: `403 INVALID_SIGNATURE`, supaya keberadaan foto tidak bisa ditebak. Tanda tangan sah tetapi `now > exp` → `410 URL_EXPIRED` (diperiksa setelah tanda tangan, supaya kedaluwarsa tidak jadi oracle). Foto bukan `pending` → `409 PHOTO_NOT_AVAILABLE`.
2. Header: `Content-Type` salah → `422 INVALID_CONTENT_TYPE`; `Content-Length` ≠ `size_bytes` → `422 SIZE_MISMATCH`. `Content-Length` yang jelas melebihi `limits.max_upload_bytes` ditolak `413 PAYLOAD_TOO_LARGE` sebelum DB disentuh atau body dibaca.
3. **Slot upload** (`limits.max_concurrent_uploads`, default 200), tanpa antrean. Penuh → `503 UPLOAD_BUSY` + `Retry-After: 2`.
4. **Satu upload per foto pada satu waktu** (menjaga hash di DB selalu cocok dengan file di disk). Upload kedua pada foto yang sedang diunggah → `409 UPLOAD_IN_PROGRESS` + `Retry-After: 2`; FE memperlakukannya sebagai dapat diulang.
5. Penyimpanan siap (`503 STORAGE_UNAVAILABLE` / `STORAGE_FULL`).
6. Body di-stream ke `tmp/` dengan buffer tetap 32 KB sambil menghitung SHA-256 (nama file sementara memuat bagian acak, sehingga dua upload tidak bertabrakan). Jumlah byte aktual ≠ `size_bytes` (lebih atau kurang, termasuk koneksi putus di tengah) → `422 SIZE_MISMATCH`; deadline baca total `limits.upload_timeout` (60 detik) terlewati → `408 UPLOAD_TIMEOUT`. Pada kedua kasus file sementara dihapus dan **foto tetap `pending`** sehingga bisa diulang.
7. Validasi isi tanpa decode piksel: magic bytes JPEG (`FF D8 FF`) atau WebP (`RIFF….WEBP`) harus cocok dengan `Content-Type`, header gambar dibaca (`jpeg.DecodeConfig` / `webp.DecodeConfig`), dan dimensinya harus sama dengan `width`/`height` yang dideklarasikan. Gagal → `422 INVALID_IMAGE`, file dihapus, foto menjadi **`rejected`** (klien harus presign ulang; PUT berikutnya pada foto itu → `409`).
8. `fsync` file, `rename` atomik ke lokasi akhir, `fsync` direktori, lalu catat `uploaded_at` + `sha256` di DB. Respons `204`. Tahap akhir ini tidak ikut dibatalkan bila klien memutus koneksi. Urutannya sengaja file dulu baru DB: bila proses mati di antaranya, yang tersisa hanya file yatim (disapu cleanup), bukan baris DB tanpa file.
9. PUT ulang pada foto `pending` menimpa file sebelumnya secara atomik dan memperbarui `sha256`/`uploaded_at`. Jika PUT ulang itu gagal, isi lama tetap utuh.

Catatan: FE membaca progres dari event upload XHR. Request lintas-origin dengan `Content-Type` memicu preflight `OPTIONS` yang harus diizinkan CORS (§9.4). Respons `PUT` memuat header CORS dan mengekspos `Retry-After` agar FE bisa melakukan backoff.

### 5.2 `POST /submissions`

Request:
```json
{
  "photos": [
    {
      "photo_id": "0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11",
      "annotations": [
        { "text": "Kerusakan di sisi kiri paket", "x": 0.12, "y": 0.30, "w": 0.25, "h": 0.18 },
        { "text": "Label resi terlipat", "x": 0.55, "y": 0.10, "w": 0.30, "h": 0.12 }
      ]
    }
  ],
  "client_request_id": "c1f7e0a4-1111-4222-8333-444444444444"
}
```
JSON dibaca secara ketat: field tak dikenal, UUID cacat, atau tipe salah → `400 INVALID_JSON`; `Content-Type` bukan `application/json` → `415`; body > 64 KB → `413`.

Urutan pemeriksaan dan hasilnya:
1. **Bentuk** (tanpa I/O), semuanya `422`: `client_request_id` wajib dan bukan UUID nol (`INVALID_CLIENT_REQUEST_ID`); 1–5 foto (`INVALID_PHOTO_COUNT`); `photo_id` tidak boleh kembar (`DUPLICATE_PHOTO`); 0–50 anotasi per foto (`TOO_MANY_ANNOTATIONS`); tiap anotasi lolos §6.3 (`INVALID_BBOX`, `INVALID_ANNOTATION_TEXT`). Pesan error memuat lokasi field, mis. `photos[0].annotations[1]: …`. Urutan array foto = urutan tampil (`position`).
2. **Idempotensi.** `client_request_id` yang sudah pernah dipakai mengembalikan submission yang sama dengan `200` (bukan `201`), berisi `edit_token` yang sama dan `version` **terkini** (dibutuhkan untuk `PUT` berikutnya), tanpa membuat baris baru dan tanpa memeriksa ulang foto. Jika id itu dipakai pemilik lain → `409 DUPLICATE_CLIENT_REQUEST` (tidak membocorkan submission orang lain). Dua request identik yang datang bersamaan menghasilkan tepat satu submission; yang kalah dijawab sebagai replay.
3. **Foto** (dimuat dan diperiksa sebelum transaksi dimulai, sehingga I/O disk tidak menahan transaksi): foto tidak ada **atau milik orang lain** → `422 PHOTO_NOT_FOUND` (jawaban sama, agar keberadaan foto orang lain tidak bisa ditebak); belum ter-upload → `422 PHOTO_NOT_UPLOADED` (tetap `pending`, bisa di-upload); bukan `pending` (sudah dipakai, `rejected`, `deleted`) → `409 PHOTO_NOT_AVAILABLE`.
4. **File di SSD** untuk setiap foto: harus ada dan berukuran sama dengan `size_bytes` (`Stat`, tanpa membaca isi). Hilang/ukuran beda → `422 PHOTO_FILE_INVALID` dan foto itu ditandai `rejected` (foto lain pada request yang sama tidak disentuh). Kegagalan penyimpanan itu sendiri (SSD lepas) → `503 STORAGE_UNAVAILABLE` tanpa menandai foto.
5. **Satu transaksi**: buat submission, lampirkan tiap foto (`UPDATE … WHERE status='pending' AND uploaded_at IS NOT NULL AND` pemilik sama, sehingga dua submission tidak bisa memakai satu foto walau berbarengan), lalu simpan anotasi. Kegagalan di langkah mana pun membatalkan semuanya (submission, status foto, dan anotasi kembali seperti semula). Foto yang direbut submission lain di tengah jalan → `409 PHOTO_NOT_AVAILABLE`.

Response `201` (submission baru) atau `200` (replay):
```json
{
  "submission_id": "7d2a...",
  "version": 1,
  "created_at": "2026-10-23T10:05:12Z",
  "edit_token": "..."
}
```
`edit_token` hanya ada jika `auth.mode = none`. `created_at` berpresisi mikrodetik dan identik pada respons pertama maupun replay. Alamat IP klien (`CF-Connecting-IP` dari proxy tepercaya) dicatat pada submission.

Rate limit: 10 request per menit per IP (`limits.rate_limit_per_min.submissions`).

### 5.3 `GET /submissions/{id}`
Mengembalikan submission dengan foto (berurutan menurut `position`, hanya yang berstatus `attached`) dan anotasinya sebagai satu snapshot konsisten:
```json
{
  "id": "7d2a...", "version": 2,
  "created_at": "2026-10-24T12:00:00Z", "updated_at": "2026-10-24T13:00:00Z",
  "editable_until": "2026-10-25T12:00:00Z", "editable": true,
  "photos": [
    { "photo_id": "0b6c...", "position": 0, "content_type": "image/webp", "size_bytes": 412345, "width": 1920, "height": 1440,
      "annotations": [ { "text": "Kerusakan di sisi kiri", "x": 0.12, "y": 0.30, "w": 0.25, "h": 0.18 } ] }
  ]
}
```
- `editable_until` = `created_at + edit.window`; `editable` dihitung dengan **jam server** (bukan jam klien) sehingga tepat pada batas dianggap sudah tertutup.
- Field internal (`storage_key`, `sha256`, `owner_id`, `client_ip`, `client_request_id`) tidak pernah dikirim. Foto tanpa anotasi memuat `"annotations": []`.
- Akses: pemegang `X-Edit-Token` yang sah (mode `none`), pemilik (mode `otp`), atau admin (§9.2). Tanpa kredensial yang cocok **dan** id yang tidak ada dijawab `404` dengan badan yang persis sama, sehingga keberadaan submission tidak bisa ditebak.

### 5.4 `PUT /submissions/{id}`

Request: `{ "version": <int>, "photos": [ … ] }`, dengan `photos` seperti §5.2, **tanpa** `client_request_id` (field itu ditolak `400`). Semantik: **replace**. Daftar `photos` dan `annotations` di request menjadi keadaan akhir, bukan diff; urutan array menentukan `position`.

Urutan pemeriksaan:
1. **Bentuk** (`422`): `version` harus ≥ 1 (`INVALID_VERSION`); aturan foto/anotasi sama dengan §5.2 (1–5 foto, tanpa duplikat, ≤ 50 anotasi, bbox dan teks valid).
2. **Otorisasi.** Id tak ada atau kredensial tak cocok → `404` seragam (badan sama persis). Yang boleh membaca tetapi tidak boleh mengedit (admin) → `403 EDIT_NOT_ALLOWED`: admin sudah tahu submission itu ada, jadi tidak perlu disamarkan.
3. **Jendela edit** 24 jam dari `created_at` (§4.4); edit tidak memperpanjangnya. `now ≥ created_at + window` → `403 EDIT_WINDOW_CLOSED`. Diperiksa **sebelum** versi, sehingga edit di luar jendela tidak pernah terlihat sebagai konflik versi.
4. **Foto.** Foto yang sudah `attached` ke submission ini boleh dipertahankan tanpa pemeriksaan ulang. Foto baru diperiksa seperti §5.2 langkah 3–4 (ada dan milik pemilik yang sama `PHOTO_NOT_FOUND`; sudah ter-upload `PHOTO_NOT_UPLOADED`; masih `pending` `PHOTO_NOT_AVAILABLE`; file ada di SSD `PHOTO_FILE_INVALID`). Foto yang pernah dibuang (`deleted`) tidak bisa dihidupkan lagi (`409 PHOTO_NOT_AVAILABLE`).
5. **Satu transaksi.** `UPDATE submissions SET version = version + 1, updated_at = … WHERE id = $1 AND version = $2` dijalankan **lebih dulu**; ia sekaligus mengunci baris submission sampai commit, sehingga dua edit serentak diserialkan dan yang memegang versi basi gagal `409 VERSION_CONFLICT` (dari 10 edit dengan versi sama, tepat satu menang). Lalu: foto yang tidak ada di request menjadi `deleted` dan **anotasinya dihapus saat itu juga** (file fisiknya dihapus cleanup §8.3); foto yang dipertahankan diperbarui `position` dan anotasinya diganti seluruhnya; foto baru di-`attach` secara atomik lalu anotasinya disimpan. Kegagalan di langkah mana pun membatalkan semuanya (versi, status foto, posisi, anotasi).

Response `200`: `{ "submission_id": "...", "version": 2, "updated_at": "..." }`.

Rate limit: berbagi kuota dengan `POST /submissions` (10 request per menit per IP, §9.4).

### 5.5 `GET /photos/{id}/url`
Response: `{ "url": "...", "expires_at": "..." }` — URL bertanda tangan ke `GET /photos/{id}/content?exp=…&sig=…`, berlaku 5 menit (`storage.view_url_ttl`). Akses: sama dengan §5.3, dicek saat URL diterbitkan. Hanya foto `attached` yang punya URL; foto `pending` (belum punya submission), `deleted`, `rejected`, foto milik submission lain, dan id yang tidak ada semuanya `404` seragam.

#### `GET /photos/{id}/content?exp=…&sig=…`
Melayani file foto lewat `http.ServeContent` (mendukung `Range` → `206`, `If-None-Match`/`If-Modified-Since` → `304`). Otorisasi hanya tanda tangan URL (tanpa header `Authorization`, agar bisa dipakai di `<img>`). Tanda tangan salah, parameter hilang/cacat, atau **id yang tidak dikenal** → `403 INVALID_SIGNATURE` (jawaban sama); tanda tangan sah tetapi lewat `exp` → `410 URL_EXPIRED`. Header respons: `Content-Type` dari DB, `ETag` = SHA-256 foto (isi tidak berubah setelah `attached`), `X-Content-Type-Options: nosniff`, `Content-Disposition: inline`, `Cache-Control: private, max-age=300`. Foto yang kemudian dihapus → `404`; file hilang dari SSD → `404 PHOTO_FILE_MISSING`. Tanda tangan lihat dan tanda tangan upload memakai domain pesan berbeda, jadi tidak saling berlaku. Deadline tulis diperpanjang lewat `ResponseController` karena `WriteTimeout` 15 detik terlalu pendek untuk klien lambat.

### 5.6 Operasional
- `GET /healthz` — liveness (proses hidup).
- `GET /readyz` — readiness (DB bisa di-ping).

### 5.7 `GET /admin/export`

Query: `format=jsonl` (default & satu-satunya di v1), `from`, `to` (filter `created_at`, opsional), `include_urls=true|false` (default `false`; jika `true`, tiap foto menyertakan URL bertanda tangan ke `/photos/{id}/content` berumur 1 jam).

Akses: admin saja (token dari `POST /admin/login`, §5.9), di mode auth apa pun.

Response: `Content-Type: application/x-ndjson`, **satu baris JSON per foto**, di-stream dengan keyset pagination `(created_at, id)` agar memori tetap kecil:

```json
{"submission_id":"7d2a...","photo_id":"0b6c...","image":{"storage_key":"2026/10/23/0b6c....webp","sha256":"9f2c…","url":null,"width":1920,"height":1440},"annotations":[{"text":"Kerusakan di sisi kiri paket","bbox_norm":[0.12,0.30,0.25,0.18],"bbox_px":[230,432,480,259]}],"created_at":"2026-10-23T10:05:12Z"}
```

- `bbox_norm` = `[x, y, w, h]` ternormalisasi (nilai asli di DB); `bbox_px` = `[x, y, w, h]` dalam piksel foto hasil proses, dihitung saat export. Konsumen tidak perlu mengetahui aturan normalisasi.
- Teks anotasi disertakan apa adanya sehingga cocok untuk dataset grounding (gambar + teks + kotak). Pemetaan teks bebas ke kelas (untuk COCO/YOLO) sengaja ditunda (§14).
- Karena `WriteTimeout` server 15 detik, handler ini memperpanjang deadline per-request lewat `http.ResponseController.SetWriteDeadline`: 60 detik per halaman (500 foto), digeser setiap halaman, sehingga export panjang tetap berjalan tetapi klien yang berhenti membaca diputus.
- Urutan: `created_at` submission, lalu `submission_id`, lalu `position` foto (keyset `(created_at, submission_id, position, photo_id)`). `from` inklusif, `to` eksklusif, keduanya RFC 3339 atas `created_at` submission. Hanya foto `attached`. `bbox_px` dibulatkan ke piksel terdekat dan dijaga tetap di dalam foto. `sha256` dalam hex; `url` bernilai `null` bila `include_urls=false`.
- Query salah (`format` selain `jsonl`, waktu bukan RFC 3339, `from ≥ to`, `include_urls` bukan boolean) → `422`. Rentang tanpa data → `200` dengan badan kosong.
- Status `200` terkirim sebelum data, jadi kegagalan di tengah jalan tidak bisa mengubah status. Hasilnya dilaporkan lewat **HTTP trailer** `X-Export-Complete: true|false` dan `X-Export-Rows: <n>` (mis. `curl --raw` atau klien HTTP yang membaca trailer); konsumen wajib memeriksa `X-Export-Complete`.
- Export **tidak memuat data pribadi** (nama, email, nomor HP, `owner_id`); hanya foto, anotasi, dan teks.
- `storage_key` adalah path relatif di bawah root foto (§8.1): admin dengan akses ke Pi dapat menyalin dataset langsung dengan `rsync --files-from`. `sha256` memungkinkan verifikasi salinan dan deteksi duplikat.

### 5.8 Auth (hanya `auth.mode = otp`; pada mode `none` semua endpoint ini mengembalikan `404`)

Semua request yang membutuhkan login memakai header `Authorization: Bearer <access_token>`.

#### `POST /auth/register`
Request:
```json
{ "name": "Budi Santoso", "email": "budi@example.com", "phone": "0812-3456-7890", "turnstile_token": "..." }
```
Validasi:
- `name`: trim, 1–100 karakter.
- `email`: format email, maks 254 karakter, disimpan lowercase. Tidak diverifikasi dan tidak harus unik.
- `phone`: dinormalisasi server-side (§7.4); format yang diterima `08…`, `+628…`, `628…` (spasi dan tanda hubung diabaikan). Nilai yang mengandung `@` atau bukan nomor seluler Indonesia ditolak `422 INVALID_PHONE`.
- `turnstile_token` wajib jika `turnstile.enabled`.

Response `202` (selalu berbentuk sama, baik nomor baru maupun yang sudah terdaftar, agar nomor yang terdaftar tidak bisa ditebak):
```json
{ "challenge_id": "5e1c...", "status": "queued", "resend_after_seconds": 60, "estimated_wait_seconds": 8 }
```
- `estimated_wait_seconds` = (jumlah challenge `queued` di depan + 1) × `wa_notif.send_interval` (default 1,5 detik); FE menampilkannya sebagai perkiraan.
- Turnstile gagal tertutup: token salah/kosong → `403 TURNSTILE_FAILED`; Cloudflare tidak terjangkau → `503 TURNSTILE_UNAVAILABLE` (permintaan tidak diteruskan).
- Jika nomor sudah terdaftar dan aktif, `name`/`email` dari request **tidak menimpa** data yang ada; hasilnya diperlakukan sebagai login.
- Error: `422 INVALID_*`, `429 RATE_LIMITED` (dengan `Retry-After`), `503 OTP_BUSY` (antrean penuh), `503 OTP_UNAVAILABLE` (kanal WhatsApp sedang bermasalah, lihat `GET /auth/channel-status`).

#### `POST /auth/otp/request`
Request: `{ "phone": "081234567890", "turnstile_token": "..." }`. Dipakai untuk login dan kirim ulang OTP. Response `202` sama dengan di atas. Untuk nomor yang tidak terdaftar, API membalas `202` dengan `challenge_id` acak tanpa mengirim apa pun. Request baru untuk nomor yang sama membatalkan challenge sebelumnya. Cooldown 60 detik **tidak berlaku** jika challenge sebelumnya berstatus `failed`, sehingga user bisa langsung mencoba lagi setelah kegagalan kirim.

#### `POST /auth/otp/verify`
Request: `{ "challenge_id": "5e1c...", "code": "123456" }`

Response `200`:
```json
{
  "access_token": "…",
  "expires_at": "2026-11-23T10:05:12Z",
  "user": { "id": "…", "name": "Budi Santoso", "email": "budi@example.com", "phone": "62812****7890", "role": "user" }
}
```
Error: `422 OTP_INVALID` dengan `error.details.attempts_left` (mis. `{"error":{"code":"OTP_INVALID","message":"…","details":{"attempts_left":3}}}`), `422 INVALID_CODE` (bukan digit; tidak dihitung sebagai percobaan), `410 OTP_EXPIRED` (kedaluwarsa, sudah dipakai, dibatalkan, atau id tidak dikenal), `429 OTP_TOO_MANY_ATTEMPTS` (challenge dibatalkan; user harus meminta kode baru), `409 OTP_NOT_SENT` (kode belum terkirim atau pengiriman gagal).
- Percobaan dinaikkan atomik **sebelum** kode dibandingkan; konsumsi challenge, aktivasi user, dan pembuatan sesi terjadi dalam satu transaksi. Percobaan berlebih yang berbarengan hanya ditolak (tidak membatalkan), agar tidak mendahului login sah yang sedang berjalan.
- `challenge_id` palsu (nomor tak terdaftar) berperilaku sama: `409` sebelum "terkirim", lalu `OTP_INVALID` dengan `attempts_left` menurun, `429` pada percobaan ke-5, lalu `410`.
- `access_token`: 32 byte acak, base64url tanpa padding (43 karakter). Response memakai `Cache-Control: no-store`.

#### `GET /auth/otp/{challenge_id}`
Dipakai FE untuk memantau pengiriman (polling 2 detik). Response `200`:
```json
{ "status": "failed", "failure_reason": "WA_UNAVAILABLE", "resend_after_seconds": 0, "expires_at": null }
```
- `status`: `queued` / `sent` / `failed` / `expired` / `consumed`.
- `failure_reason` (hanya saat `failed`): `WA_UNAVAILABLE` (gateway WhatsApp tidak terhubung, timeout, atau token ditolak), `SEND_FAILED` (gateway menolak pengiriman).
- Tidak mengembalikan data pengguna atau kode. `challenge_id` tidak dapat ditebak (UUID acak); `challenge_id` yang tidak dikenal (termasuk bukan UUID) mengembalikan `status: "queued"` agar tidak membuka informasi. Status internal `sending` ditampilkan sebagai `queued`; `sent` yang melewati `expires_at` ditampilkan `expired`. Header `Cache-Control: no-store`.
- **Anti-enumerasi lewat polling:** `challenge_id` palsu yang dibagikan untuk nomor tak terdaftar dicatat di memori (maks 10.000, yang tertua dibuang) dan statusnya berjalan seperti challenge asli: `queued` → `sent` (setelah `estimated_wait_seconds`, dengan `expires_at`) → `expired`. Tanpa ini, memantau status akan membedakan nomor terdaftar (menjadi `sent`) dari yang tidak. Batas sisa: setelah restart id palsu terlihat `queued` selamanya, dan kegagalan kirim hanya mungkin terjadi pada nomor asli.

#### `GET /auth/channel-status`
Publik, ringan, tanpa Turnstile. Response: `{ "otp_available": true }`. FE memanggilnya saat layar login dibuka dan menampilkan banner "Pengiriman kode WhatsApp sedang bermasalah" bila `false`. Nilainya berasal dari probe berkala ke `wa-notif` dan hasil pengiriman terakhir (§9.5).

#### `GET /auth/me`
Mengembalikan profil user dan `role` (nomor HP disamarkan). Tanpa token, token salah format, dicabut, atau kedaluwarsa → `401 UNAUTHORIZED` dengan `WWW-Authenticate: Bearer`. Skema `Bearer` tidak peka huruf besar. Rute auth publik (`register`, `otp/*`, `channel-status`) tidak memeriksa header `Authorization`, sehingga token basi di FE tidak menghalangi login ulang.

#### `POST /auth/logout`
Mencabut session saat ini. Response `204`.

### 5.9 Admin (semua mode auth)

#### `POST /admin/login`
Request: `{ "username": "admin", "password": "…" }`. Dibandingkan dengan `admin.username` dan hash bcrypt `admin.password_bcrypt` di `config.json` (§9.1). Response `200`: `{ "access_token": "…", "expires_at": "…" }`; token bertanda tangan HMAC, stateless, berlaku `admin.token_ttl` (default 8 jam). Kredensial salah → `401 INVALID_CREDENTIALS` (pesan sama untuk username atau password salah). Rate limit 5 / menit per IP. Jika `admin.enabled = false`, endpoint ini `404`.

Token dipakai sebagai `Authorization: Bearer <token>` pada `GET /admin/export`, serta `GET /submissions/{id}` dan `GET /photos/{id}/url` (baca saja). Admin **hanya baca**: token admin pada `PUT /submissions/{id}`, presign, submit, atau `/auth/me` → `403 ADMIN_READ_ONLY` (juga pada mode `none`, walau membawa `edit_token` yang sah).

Bentuk token: `adm1.<exp unix>.<base64url(HMAC-SHA256(ADMIN_TOKEN_SECRET, "admin\n" ‖ exp))>`. Username dibandingkan constant-time atas hash-nya dan bcrypt selalu dijalankan, sehingga username salah dan password salah memakan waktu dan pesan yang sama. Token tidak bisa dicabut satu per satu; merotasi `ADMIN_TOKEN_SECRET` membatalkan semuanya. Header `Authorization` yang ada tetapi tidak sah selalu `401` (tidak diabaikan), juga pada mode `none`.

## 6. Pemrosesan di Frontend

### 6.1 Ambil foto
- Mobile: `<input type="file" accept="image/*" capture="environment">`.
- Desktop: file picker biasa (opsional: `getUserMedia` untuk webcam).

### 6.2 Resize & kompresi
1. Decode dengan `createImageBitmap(file)` (orientasi EXIF diterapkan otomatis di browser modern).
2. Resize sehingga sisi terpanjang **maks 1920 px** (tidak upscale).
3. Encode via `canvas.toBlob()`:
   - Coba `image/webp` quality 0.8.
   - Jika `blob.type` bukan `image/webp` (beberapa versi Safari tidak mendukung encode WebP), fallback ke `image/jpeg` quality 0.8.
4. Jika hasil > 2 MB, turunkan quality bertahap (0.7, 0.6) sebelum menyerah.

Efek samping yang diinginkan: re-encode lewat canvas **menghapus metadata EXIF termasuk GPS**, sehingga lokasi user tidak ikut ter-upload.

Target ukuran hasil: 200–500 KB per foto.

### 6.3 Format anotasi
- Bentuk: rectangle, axis-aligned, dengan satu teks bebas per kotak.
- Koordinat **dinormalisasi 0–1** relatif terhadap dimensi foto **hasil proses** (yang di-upload), origin di kiri atas.
- `x`, `y` = pojok kiri atas; `w`, `h` = lebar & tinggi.
- Aturan validasi (FE & BE): `0 ≤ x,y < 1`, `0 < w,h ≤ 1`, `x + w ≤ 1`, `y + h ≤ 1`, dan ukuran minimum `w,h ≥ 0.005` untuk mencegah kotak tak sengaja. Batas `x + w ≤ 1` dan `y + h ≤ 1` memakai toleransi pembulatan `1e-6` (`x + w ≤ 1.000001`), baik di aplikasi maupun di CHECK constraint, karena konversi piksel→0..1 di browser dapat menghasilkan 1.0000000002 untuk kotak yang sah.
- `text`: wajib, 1–1000 karakter (rune) setelah `trim`; `CRLF` dinormalisasi ke `LF`; UTF-8 valid; tanpa karakter kontrol selain newline dan tanpa karakter kontrol arah teks (U+202A–202E, U+2066–2069) yang bisa menipu tampilan di layar admin. Karakter ZWJ (emoji majemuk) tetap diizinkan.
- Anotasi tidak di-"burn" ke gambar. Render overlay dilakukan di client.

### 6.4 UX
- **Registrasi/login (mode `otp`):** satu layar isi nama, email, nomor HP; layar berikutnya input 6 digit dengan hitung mundur kirim ulang (60 detik) dan pesan "kode dikirim lewat WhatsApp, bisa memakan waktu hingga `estimated_wait_seconds` detik saat ramai". `access_token` disimpan di `localStorage`; saat `401`, token dihapus dan user diarahkan ke login. Teks input OTP memakai `autocomplete="one-time-code"` dan `inputmode="numeric"`.
- **Kegagalan WhatsApp:** jika `channel-status` false, atau status challenge `failed`, tampilkan pesan jelas ("Layanan WhatsApp sedang bermasalah, coba lagi beberapa saat") dan tombol "Kirim ulang"; jangan biarkan user menunggu kode yang tidak akan datang.
- **Edit:** tampilkan sisa waktu edit dari `editable_until`; setelah lewat, layar menjadi baca-saja.
- Tampilkan progress upload per foto; tombol Submit nonaktif sampai semua upload selesai dan semua kotak punya teks.
- Simpan draft anotasi & teks di `localStorage` agar tidak hilang saat reload (file foto tidak disimpan di draft v1).
- Pada `auth.mode = none`, simpan `{submission_id, edit_token}` di `localStorage` agar user bisa kembali mengedit. Jika storage hilang, submission tidak bisa diedit lagi (§14).
- Mode edit memakai `version` dari `GET`; saat menerima `409`, tawarkan muat ulang.

## 7. Data Model (PostgreSQL)

Memakai database khusus `photo_annotated` (role khusus) di instance PostgreSQL 17 shared (§11.1). Migrasi dikelola lewat `cmd/migrate`.

### 7.1 `submissions`
| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | `uuid` PK | |
| `owner_id` | `uuid` FK → users NULL, ON DELETE SET NULL | Pemilik (`users.id`); NULL pada `auth.mode = none` |
| `client_request_id` | `uuid` UNIQUE NOT NULL | Idempotency key |
| `client_ip` | `inet` | Dari header `CF-Connecting-IP` |
| `version` | `int` NOT NULL DEFAULT 1 | Optimistic locking untuk edit |
| `created_at` | `timestamptz` NOT NULL DEFAULT now() | Dasar perhitungan retensi |
| `updated_at` | `timestamptz` NOT NULL DEFAULT now() | |

Index: `(owner_id)`, `(created_at)` untuk cleanup retensi.

### 7.2 `photos`
| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | `uuid` PK | |
| `submission_id` | `uuid` FK → submissions NULL, ON DELETE CASCADE | NULL selama `pending` |
| `owner_id` | `uuid` FK → users NULL, ON DELETE SET NULL | Pembuat presign; NULL pada `auth.mode = none` |
| `position` | `smallint` NOT NULL DEFAULT 0 | Urutan dalam submission |
| `storage_key` | `text` UNIQUE NOT NULL | Path relatif di bawah root SSD: `{yyyy}/{mm}/{dd}/{uuid}.{ext}` |
| `content_type` | `text` NOT NULL | |
| `size_bytes` | `int` NOT NULL | Dideklarasikan saat presign; harus sama dengan byte aktual saat upload |
| `width`, `height` | `int` NOT NULL | Dideklarasikan saat presign; dicocokkan dengan header gambar saat upload |
| `uploaded_at` | `timestamptz` NULL | Diisi setelah PUT sukses dan lolos validasi isi |
| `sha256` | `bytea` NULL | Dihitung saat streaming upload |
| `status` | `text` NOT NULL | `pending` / `attached` / `rejected` / `deleted` |
| `created_at` | `timestamptz` NOT NULL DEFAULT now() | |

Index: `(status, created_at)` untuk cleanup job; `(submission_id)`.

### 7.3 `annotations`
| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | `bigserial` PK | |
| `photo_id` | `uuid` FK → photos NOT NULL | ON DELETE CASCADE |
| `text` | `text` NOT NULL | CHECK `char_length(text) BETWEEN 1 AND 1000` |
| `x`, `y`, `w`, `h` | `double precision` NOT NULL | Dengan CHECK constraint sesuai §6.3 (termasuk toleransi `1e-6`). Bukan `real`: float32 tidak menyimpan 0.12 secara persis, sehingga nilai tidak kembali sama saat dibaca/diekspor |
| `created_at` | `timestamptz` NOT NULL DEFAULT now() | |

Index: `(photo_id)`.

Tidak ada tabel `labels` dan tidak ada schema form: isi submission sepenuhnya foto + anotasi + teks anotasi.

### 7.4 `users` (mode `otp`)
| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | `uuid` PK | |
| `name` | `text` NOT NULL | CHECK `char_length(name) BETWEEN 1 AND 100` |
| `email` | `text` NOT NULL | Lowercase, maks 254; **tidak unik, tidak diverifikasi** |
| `phone` | `text` UNIQUE NOT NULL | Bentuk kanonik `62…` tanpa `+`, spasi, atau `-` |
| `status` | `text` NOT NULL | `pending` (OTP pertama belum diverifikasi) / `active` |
| `phone_verified_at` | `timestamptz` NULL | |
| `created_at` | `timestamptz` NOT NULL DEFAULT now() | |
| `last_login_at` | `timestamptz` NULL | |

**Normalisasi nomor HP (`domain.NormalizePhone`, bukan di handler):** hanya karakter yang lazim pada penulisan nomor yang diterima: angka, spasi, `-`, `.`, `(`, `)`, dan satu `+` di awal; **apa pun selain itu ditolak** (huruf, `@`, JID, karakter kontrol, angka non-ASCII, `+` di tengah). Bentuk yang diterima: `08…`, `+628…`, `628…`, `8…`, `0062…`, dan `+62 0812…` (kesalahan umum: 0 sesudah kode negara). Awalan internasional lain (`00…` selain `0062`) dan nomor negara lain ditolak. Hasilnya harus cocok `^628[0-9]{8,12}$` (nomor seluler Indonesia, 11–15 digit) dan normalisasinya idempoten. Pola yang sama dipakai di CHECK constraint `users.phone` dan `otp_challenges.phone`. Ini penting karena `wa-notif` menerima JID mentah, termasuk JID grup (`…@g.us`); API ini **tidak boleh pernah meneruskan input user apa adanya** ke `wa-notif`, hanya nomor kanonik hasil normalisasi.

Semua baris `users` berperan `user`. Admin bukan baris `users`; admin memakai akun statis di `config.json` (§9.1).

`name`: 1–100 karakter setelah `trim`, tanpa karakter kontrol (termasuk newline) dan tanpa karakter kontrol arah teks. `email`: satu alamat polos (tanpa nama tampilan/komentar/spasi), domain bertitik, ≤ 254 karakter, disimpan huruf kecil (CHECK memastikan `email = lower(email)`); spasi/CRLF di ujung dipangkas, newline di tengah ditolak (mencegah injeksi header). Status `active` ⇔ `phone_verified_at` terisi (CHECK). Menghapus pengguna tidak menghapus submission/fotonya (`owner_id` → NULL).

### 7.5 `otp_challenges`
| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | `uuid` PK | = `challenge_id` |
| `user_id` | `uuid` FK → users NOT NULL, ON DELETE CASCADE | |
| `phone` | `text` NOT NULL | Salinan nomor kanonik saat dibuat |
| `code_hash` | `bytea` NOT NULL | `HMAC-SHA256(OTP_PEPPER, id ‖ code)`; kode asli tidak disimpan |
| `status` | `text` NOT NULL | `queued` / `sending` / `sent` / `failed` / `consumed` / `expired` |
| `failure_reason` | `text` NULL | `WA_UNAVAILABLE` / `SEND_FAILED`; hanya saat `failed` |
| `verify_attempts` | `smallint` NOT NULL DEFAULT 0 | Maks `auth.otp.max_verify_attempts` |
| `send_attempts` | `smallint` NOT NULL DEFAULT 0 | Maks 3 |
| `next_attempt_at` | `timestamptz` NOT NULL DEFAULT now() | Untuk backoff retry kirim |
| `wa_message_id` | `text` NULL | Dari response `wa-notif` |
| `client_ip` | `inet` | |
| `created_at` | `timestamptz` NOT NULL DEFAULT now() | |
| `sent_at` | `timestamptz` NULL | |
| `expires_at` | `timestamptz` NULL | `sent_at + auth.otp.ttl`; NULL selama belum terkirim |

Index: `(status, next_attempt_at)` untuk worker (`SELECT … FOR UPDATE SKIP LOCKED`); `(phone, created_at)` untuk batas per nomor. Satu nomor hanya punya satu challenge aktif: **indeks unik parsial** `otp_one_active_per_phone` (`WHERE status IN ('queued','sending','sent')`) menegakkannya; request baru menandai yang lama `expired` dalam transaksi yang sama, dan dua request berbarengan untuk nomor yang sama menghasilkan satu pemenang (yang kalah mendapat `409 OTP_REQUEST_IN_PROGRESS`). Saat diklaim worker, `next_attempt_at` diisi waktu klaim sebagai penanda; challenge `sending` yang lebih tua dari batas dikembalikan ke `queued` (worker mati). `verify_attempts` dinaikkan secara atomik (`UPDATE … RETURNING`) dan `consumed` lewat `UPDATE … WHERE status='sent'`, sehingga tebakan paralel tidak bisa melewati batas percobaan dan satu kode hanya sah sekali walau diverifikasi berbarengan.

### 7.6 `sessions`
| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | `uuid` PK | |
| `user_id` | `uuid` FK → users NOT NULL, ON DELETE CASCADE | |
| `token_hash` | `bytea` UNIQUE NOT NULL | SHA-256 dari token acak 32 byte; token asli hanya dikirim sekali ke client |
| `created_at` | `timestamptz` NOT NULL DEFAULT now() | |
| `expires_at` | `timestamptz` NOT NULL | `created_at + auth.session_ttl` (default 30 hari) |
| `revoked_at` | `timestamptz` NULL | Diisi saat logout |
| `client_ip` | `inet` | |

Index: `(user_id)`. Validasi token = satu lookup indeks unik per request (tidak ada penulisan per request).

## 8. Penyimpanan Foto (SSD Lokal) & Retensi

### 8.1 Lokasi dan layout
- Foto disimpan di SSD USB 3.0 yang di-mount di Raspi, mis. `/mnt/ssd`, dengan root foto `/mnt/ssd/photo-annotated/photos` (config `storage.local.root`). Di container `api`, root itu di-bind-mount ke `/data/photos`.
- Layout: `{root}/{yyyy}/{mm}/{dd}/{uuid}.{ext}` untuk file akhir dan `{root}/tmp/` untuk upload yang sedang berjalan. Pembagian per hari menjaga jumlah file per direktori tetap kecil (ratusan, bukan puluhan ribu).
- Sistem berkas ext4, mount dengan `noatime`, memakai UUID di `/etc/fstab`.
- **Akses file lewat `os.Root`** (Go ≥ 1.24) sehingga path tidak bisa keluar dari root lewat `..` atau symlink. `storage_key` selalu dibuat server dari UUID dan tanggal, tidak pernah dari input client.
- Container `api` berjalan sebagai user non-root; direktori root foto dimiliki UID yang sama.

### 8.2 Penjagaan SSD (fail fast)
- **Wajib SSD yang benar (file penanda):** dengan `storage.local.require_marker = true` (wajib di produksi), root foto harus berisi file penanda `.photo-annotated-store` (file biasa, bukan symlink/direktori) yang dibuat operator sekali saat setup di SSD (`touch`). Tanpa penanda, API menolak start. Tujuannya mencegah upload diam-diam menulis ke SD card ketika SSD lepas/belum ter-mount. Pembandingan device dengan direktori induk **tidak dipakai** karena tidak andal: diuji di Pi, bind mount Docker dari direktori SD card tetap tampak sebagai device terpisah dari induknya di dalam container (dev 45826 vs 47), sehingga cek itu lolos padahal SSD tidak ter-mount. Penanda ikut hilang saat SSD lepas, jadi cek berkala juga mendeteksi pencabutan saat runtime. Saat pengembangan, `require_marker = false` dengan direktori biasa.
- **Cek berkala (tiap 30 detik) dan saat presign:** root masih ter-mount dan dapat ditulis, sisa ruang ≥ `storage.local.min_free_bytes` (default 5 GB). Jika tidak, presign dan upload dijawab `503 STORAGE_FULL` / `503 STORAGE_UNAVAILABLE`, dan alert menyala (§12).
- **Penulisan aman:** file ditulis ke `tmp/`, `fsync`, `rename` atomik ke lokasi akhir, lalu `fsync` direktori. Mati listrik tidak meninggalkan file separuh di lokasi akhir; sisa `tmp/` dibersihkan §8.3.
- **Tidak ada redundansi:** satu SSD adalah satu titik kegagalan; mitigasinya backup (§11.5).
- **Kapasitas:** foto ±400 KB; SSD 256 GB muat ±650 ribu foto, jauh di atas kebutuhan retensi 90 hari pada beban realistis.

### 8.3 Cleanup & retensi

Dua pekerjaan berkala berjalan di dalam proses API (`internal/scheduler`; putaran tidak pernah tumpang tindih, panic ditangkap, berhenti saat shutdown). Semua kueri memakai `FOR UPDATE SKIP LOCKED` dan semua langkah idempoten, jadi aman dijalankan bersama instance lain atau lewat `cmd/maintenance`. Setiap putaran dibatasi `MaxBatches` (50 × `cleanup_batch`); sisanya dikerjakan putaran berikutnya.

**Aturan utama: file dulu, baris kemudian.** Baris hanya dihapus setelah filenya terhapus, sehingga kegagalan menghapus file tidak pernah meninggalkan file tanpa catatan.

**Pembersihan + retensi** (tiap `retention.cleanup_interval`, default 1 jam; putaran pertama 1 menit setelah start):
1. **Foto pending basi:** `pending` dengan `created_at < now - pending_photo_ttl` (default 24 jam) **di-claim secara atomik** (`status → deleted`, `FOR UPDATE SKIP LOCKED`). Setelah di-claim foto tidak bisa lagi dilampirkan (`Attach` hanya menerima `pending`), sehingga klien yang melampirkan foto tepat pada detik cleanup tidak kehilangan filenya.
2. **Foto `rejected`/`deleted`** (hasil validasi gagal, claim di atas, atau dibuang lewat edit), usia berapa pun: `Delete` file (idempoten; file yang sudah tidak ada dianggap sukses), lalu hapus barisnya (anotasi ikut lewat `CASCADE`). Kegagalan hapus file → baris dipertahankan dan dicoba lagi pada putaran berikutnya; baris itu tidak dicoba ulang dalam putaran yang sama dan tidak membuat putaran berputar tanpa henti.
3. **Retensi** (`retention.days`, default 90): submission dengan `created_at < now - days` (batas ketat; tepat pada batas belum dihapus) → seluruh fotonya ditandai `deleted` (foto tak lagi terlihat klien), file dan barisnya dihapus seperti langkah 2, dan **baru setelah submission tidak punya baris foto lagi** barisnya dihapus. Submission yang file-nya gagal dihapus bertahan sampai penyimpanan pulih, tanpa menghalangi submission lain.
4. **Langkah tambahan** sebagai `CleanupStep`; untuk data auth (`usecase.AuthCleanup`, aktif hanya pada `auth.mode = otp`): `otp_challenges` yang sudah selesai (bukan `queued`/`sending`) > 24 jam, `users` berstatus `pending` > 24 jam (challenge-nya ikut lewat CASCADE), dan `sessions` yang kedaluwarsa/dicabut lebih dari 7 hari yang lalu. Yang masih berjalan atau dalam masa tenggang tidak disentuh. Kegagalan satu langkah tidak menghentikan langkah lain.
5. **Sisa tmp:** `tmp/*.part` dan `.probe-*` yang lebih tua dari `retention.tmp_max_age` (default 1 jam).

**Sapuan file yatim** (tiap `retention.orphan_sweep_interval`, default 24 jam; pertama 5 menit setelah start): file di SSD yang namanya berbentuk kunci kanonik, lebih tua dari `retention.orphan_min_age` (default 24 jam, tidak boleh lebih pendek dari `storage.upload_url_ttl`), dan **tidak punya baris foto berstatus apa pun** → dihapus (mis. proses mati antara file terbit dan baris dicatat, atau upload yang kalah balapan dengan cleanup). Kunci diperiksa ke DB per 200 sekaligus; bila pengecekan DB gagal, **tidak ada file yang dihapus**. Berkas asing (penanda, catatan admin, dsb.) tidak pernah disentuh.

Hasil tiap putaran (jumlah di-claim, ditandai, file dihapus/gagal, baris dihapus, submission dihapus, tmp dibersihkan) dicatat ke log untuk dashboard §12. `cmd/maintenance cleanup|sweep-orphans` menjalankan pekerjaan yang sama sekali jalan untuk keperluan operasional.

## 9. Keamanan & Autentikasi

### 9.1 Mode autentikasi (`config.json` → `auth.mode`)

| Mode | Identitas | Keterangan |
|---|---|---|
| `none` | Anonim | Form publik. Wajib dikombinasikan dengan Turnstile (`turnstile.enabled`) dan rate limit. Kepemilikan lewat `edit_token` (§9.3). Tidak ada admin. |
| `otp` | User terdaftar | Registrasi nama + email + nomor HP, verifikasi OTP lewat WhatsApp (§4.0), lalu bearer session token (§7.6). |

**Admin tidak bergantung pada `auth.mode`.** Admin adalah satu akun statis di `config.json` (`admin.username`, `admin.password_bcrypt`), login lewat `POST /admin/login` (§5.9), dan menerima token bertanda tangan HMAC (stateless, kunci `ADMIN_TOKEN_SECRET`, default berlaku 8 jam). Password di config disimpan sebagai **hash bcrypt**, bukan plaintext; hash dibuat dengan `htpasswd -bnBC 10 "" '<password>' | tr -d ':\n'`. Perbandingan username memakai constant-time, login dibatasi 5 percobaan/menit per IP. Admin boleh membaca submission/foto dan mengekspor, tetapi tidak boleh mengedit submission.

Peran: `user` (mode `otp`), pemilik anonim (mode `none`, dibuktikan `edit_token`), dan `admin`. Config divalidasi saat startup; konfigurasi mode yang tidak lengkap (mis. `otp` tanpa `wa_notif.base_url` atau `OTP_PEPPER`) membuat API **gagal start** (fail fast), tidak jatuh diam-diam ke `none`. Mode lain (mis. `static_token`, `jwt`/Cloudflare Access) bisa ditambahkan nanti lewat port `Authenticator` tanpa mengubah usecase.

### 9.2 Matriks akses

| Endpoint | `none` | `otp` |
|---|---|---|
| `/auth/*` | `404` | Publik (Turnstile + rate limit, §9.5), kecuali `me` dan `logout` yang butuh session |
| `POST /uploads/presign` | Publik + Turnstile | `user` |
| `PUT /uploads/{photo_id}` | Tanda tangan URL dari presign | Tanda tangan URL dari presign |
| `POST /submissions` | Publik + Turnstile | `user` |
| `GET /submissions/{id}`, `GET /photos/{id}/url` | Pemilik (`edit_token`) atau `admin` | Pemilik (`owner_id` = user) atau `admin` |
| `GET /photos/{id}/content` | Tanda tangan URL dari `/url` | Tanda tangan URL dari `/url` |
| `PUT /submissions/{id}` | Pemilik (`edit_token`), dalam 24 jam | Pemilik saja, dalam 24 jam |
| `POST /admin/login` | Publik (rate limit) | Publik (rate limit) |
| `GET /admin/export` | `admin` | `admin` |
| `/healthz`, `/readyz` | Publik | Publik |

Akses ke resource milik orang lain mengembalikan `404` (bukan `403`) agar ID tidak bisa di-probe; memakai foto orang lain di submission dijawab `422 PHOTO_NOT_FOUND`, sama persis dengan foto yang tidak ada. Pada mode `otp`, rute pemilik tanpa sesi yang sah → `401` (FE kembali ke login), `owner_id` foto dan submission diisi dari sesi, `edit_token` tidak diterbitkan dan header `X-Edit-Token` diabaikan. `PUT /uploads/{id}` dan `GET /photos/{id}/content` tetap diotorisasi tanda tangan URL saja.

### 9.3 `edit_token` (hanya `auth.mode = none`)
- Dihitung **stateless**: `edit_token = base64url(HMAC-SHA256(EDIT_TOKEN_SECRET, submission_id))`. Tidak ada kolom token di DB, dan retry idempotent (§5.2) mengembalikan token yang sama.
- Dikirim client lewat header `X-Edit-Token`, dibandingkan dengan `hmac.Equal` (constant-time).
- Merotasi `EDIT_TOKEN_SECRET` membatalkan semua token yang sudah beredar.

### 9.4 Kontrol lain
- **Turnstile** (`turnstile.enabled`): pada mode `none` diverifikasi saat presign dan submit lewat header `X-Turnstile-Token`; pada mode `otp` diverifikasi saat `register` dan `otp/request` (field `turnstile_token`, §9.5). Cloudflare menolak token yang diverifikasi dua kali, padahal satu submission butuh hingga 5 presign (paralel) + 1 submit. Karena itu pada mode `none` **satu token yang sudah lolos boleh dipakai `max_photos_per_submission + 1` kali selama 5 menit dari IP yang sama**: siteverify dipanggil sekali (request paralel menunggu hasil yang sama), kegagalan tidak disimpan, dan cache dibatasi 10.000 token. Edit dan baca pada mode `none` dibuktikan `edit_token`, tanpa Turnstile.
- **Rate limit** (per IP dari `CF-Connecting-IP`; nilai bisa diubah di config):
  - presign: 30 / menit
  - submissions (POST & PUT): 10 / menit
  - `auth/register` + `auth/otp/request`: 5 / menit; `auth/otp/verify`: 20 / menit; `admin/login`: 5 / menit
  - Tambahan rate limit di edge Cloudflare.
- **CORS API:** `allowed_origins` dari config; method `GET, POST, PUT, OPTIONS`; header yang diizinkan `Content-Type`, `Authorization`, `X-Edit-Token`; preflight di-cache 1 jam (`Access-Control-Max-Age`). Upload `PUT` lintas-origin dengan `Content-Type` memicu preflight, jadi rute `/uploads/*` wajib tercakup.
- `STORAGE_SIGNING_SECRET` (kunci HMAC URL upload dan lihat foto, ≥ 32 byte acak) hanya ada di env Raspi. Merotasinya membatalkan semua URL yang belum kedaluwarsa (umurnya pendek, dampaknya kecil). Pesan yang ditandatangani diberi awalan tujuan (`upload`/`view`) agar tanda tangan satu tujuan tidak bisa dipakai untuk yang lain.
- **Secret tidak disimpan di `config.json`** (lihat §11.4).
- URL bertanda tangan berumur pendek (upload 10 menit, lihat foto 5 menit).
- Validasi berlapis: saat presign (deklarasi), saat upload (`Content-Length` dan jumlah byte aktual), dan setelah tulis (magic bytes + dimensi dari header gambar). Server benar-benar memeriksa isi file, sehingga file non-gambar dengan `Content-Type` palsu ditolak.
- Request body JSON dibatasi 64 KB (`http.MaxBytesReader`); body upload dibatasi tepat `size_bytes` (maks 2 MB).
- Foto disajikan dengan `X-Content-Type-Options: nosniff` dan `Content-Disposition: inline`, hanya dari file yang sudah divalidasi sebagai gambar.
- Semua query memakai parameterized query (`pgx`).
- Teks anotasi diperlakukan sebagai data tak tepercaya: FE merender sebagai teks (bukan HTML), dan export tidak menjalankan interpretasi apa pun.

### 9.5 OTP, session, dan integrasi `wa-notif`

Risiko utama: endpoint OTP publik membuat API ini bisa dipakai untuk **mengirim WhatsApp ke nomor sembarang dari satu nomor pengirim**. Penyalahgunaan bisa membuat nomor pengirim ditandai spam atau diblokir WhatsApp, dan itu juga mematikan notifikasi lain yang memakai `wa-notif`. Karena itu:

- **Kode OTP:** 6 digit, diturunkan dari `challenge_id` (UUID acak dari `crypto/rand`) dengan PRF: `HMAC-SHA256(OTP_PEPPER, "otp-code"‖id) mod 10^6`. Dengan begitu kode asli tidak perlu disimpan (juga tidak terenkripsi) tetapi dispatcher tetap dapat membuatnya saat mengirim; tanpa `OTP_PEPPER` kode tidak dapat ditebak dari `challenge_id`. Berlaku 5 menit sejak terkirim, sekali pakai. Disimpan sebagai HMAC dengan `OTP_PEPPER` (6 digit tanpa pepper mudah di-brute-force dari hash). Perbandingan memakai `hmac.Equal`. Maksimal 5 percobaan verifikasi per challenge, setelah itu challenge dibatalkan.
- **Pembatasan kirim:**
  - cooldown 60 detik per nomor, maksimal 5 OTP per nomor per jam. Dihitung di memori per nomor kanonik dan berlaku **sama untuk nomor terdaftar maupun tidak** (menghitung dari tabel OTP hanya mungkin untuk nomor terdaftar, sehingga `429` akan membocorkan pendaftaran). Ditolak karena validasi atau kapasitas tidak memakai jatah; gagal kirim permanen mengembalikan jatah (sehingga cooldown tidak berlaku setelah `failed`). State hilang saat restart (satu instance);
  - rate limit per IP (§9.4) + Turnstile wajib pada `register` dan `otp/request`;
  - panjang antrean `queued` dibatasi (`max_queue`, default 200); penuh → `503 OTP_BUSY`;
  - **batas harian global** (`daily_global_cap`, default 500) sebagai rem darurat; terlampaui → `503 OTP_BUSY` dan alert.
- **Nomor tujuan** hanya nomor kanonik hasil normalisasi §7.4 (tidak pernah JID atau input mentah).
- **Anti-enumerasi:** `register` dan `otp/request` selalu membalas `202` berbentuk sama, baik nomor sudah terdaftar atau belum.
- **Session token:** 32 byte acak, hanya hash SHA-256 yang disimpan, berlaku 30 hari, dicabut saat logout. Dikirim sebagai `Authorization: Bearer`. Karena disimpan di `localStorage`, FE wajib memakai CSP ketat dan merender teks user sebagai teks.
- **Kredensial `wa-notif`:** bearer token `wa-notif` ada di env `WA_NOTIF_TOKEN` (bukan di `config.json`) dan tidak pernah dikirim ke FE.
- **Pemetaan hasil `wa-notif` → `failure_reason`** (berdasarkan kode `wa-notif` saat ini):

  | Respons `wa-notif` | Perlakuan | `failure_reason` |
  |---|---|---|
  | `200` `success: true` | `sent` | – |
  | `503` `WA_NOT_CONNECTED` | retry, lalu `failed` | `WA_UNAVAILABLE` |
  | timeout / koneksi gagal | retry, lalu `failed` | `WA_UNAVAILABLE` |
  | `401` (token ditolak) | `failed` tanpa retry + alert (salah konfigurasi) | `WA_UNAVAILABLE` |
  | `500` `SEND_FAILED` (termasuk nomor ditolak; `wa-notif` memetakan semua error selain tidak-terhubung ke 500) | retry, lalu `failed` | `SEND_FAILED` |

  `200` hanya berarti protokol WhatsApp menerima pesan, bukan bahwa pesan dibaca atau diterima oleh nomor tujuan. Selain tabel di atas: `429` dan 5xx lain (mis. 502/504 dari proxy) → `WA_UNAVAILABLE` dapat diulang; 4xx lain (`400`, `404`, `405`) → `SEND_FAILED` tanpa ulang dan memicu alert (request kita ditolak, artinya bug/konfigurasi); badan `200` yang bukan bentuk `{success:true,data:{message_id}}` (mis. portal captive) → `WA_UNAVAILABLE` dapat diulang. Adapter tidak mengikuti redirect, membatasi badan respons 64 KB, dan tidak pernah memasukkan isi pesan (kode OTP), token, atau URL lengkap ke pesan error/log.
- **Pagar tujuan:** adapter menolak (tanpa menyentuh jaringan) setiap tujuan yang bukan nomor kanonik `628…`. `wa-notif` menerima JID mentah termasuk grup (`…@g.us`), jadi ini pagar terakhir setelah normalisasi §7.4.
- **Kesehatan nyata `wa-notif` (diamati 24 Okt 2026):** proses hidup dan `/health` UP, tetapi sesi WhatsApp `DISCONNECTED` sejak 20 Okt 09:12 WIB: koneksi awal gagal ("failed to dial whatsapp web websocket… will retry automatically") dan tidak pernah dicoba lagi (tidak ada log sejak itu). Ini persis kasus yang dilindungi probe dan `channel-status`. Pemulihan: `docker restart wa-notif-service` (sesi tersimpan di SQLite, tanpa QR); perbaikan permanen ada di `wa-notif` (coba sambung ulang di latar belakang saat koneksi awal gagal).
- **Status kanal (`otp_available`):** API menjalankan probe `GET /api/v1/session/status` ke `wa-notif` setiap 30 detik (probe pertama segera saat start). Endpoint itu **publik di `wa-notif`** (tidak butuh token, berbeda dari dokumentasi OpenAPI-nya; sama halnya `/api/v1/session/qr`), jadi probe tidak mengirim token. `otp_available = false` jika probe menunjukkan tidak `connected` / tidak terjangkau, atau 3 percobaan kirim terakhir berturut-turut gagal **karena gateway** (`WA_UNAVAILABLE`). `SEND_FAILED` tidak dihitung: kegagalan itu bisa khusus satu nomor (mis. tanpa WhatsApp), dan bila dihitung, satu nomor buruk yang dicoba 3× akan menutup kanal untuk semua orang. Kanal dianggap tersedia sampai ada bukti sebaliknya. **Probe yang sukses juga mereset hitungan kegagalan kirim**: saat tidak tersedia tidak ada request yang diantrekan, sehingga tanpa itu tidak akan pernah ada pengiriman yang bisa memulihkan status; satu probe sukses mengizinkan beberapa pengiriman percobaan. Saat `false`, `register` dan `otp/request` langsung `503 OTP_UNAVAILABLE` tanpa mengantrekan.
- **Jaringan:** API memanggil `wa-notif` hanya lewat jaringan internal Raspi. Port `3030` **tidak boleh** di-expose lewat Cloudflare Tunnel. Halaman `/login` `wa-notif` menampilkan QR pairing tanpa autentikasi, jadi batasi akses ke port itu (firewall/LAN saja), atau sebaiknya bergabung lewat network Docker bersama sehingga port tidak perlu dipublikasikan.
- **Privasi:** nama, email, dan nomor HP adalah data pribadi. Nomor di log dan response disamarkan (`62812****7890`); kode OTP dan isi pesan tidak pernah dicatat di log.

## 10. Non-Functional Requirements

| Metrik | Target |
|---|---|
| User aktif bersamaan | 1000 |
| Throughput puncak API (JSON) | ≥ 150 req/s tanpa error |
| Foto masuk berkelanjutan | ≥ 30 foto/s (≈ 96 Mbps); dibatasi internet rumah (terukur ±187 Mbps down) |
| Upload bersamaan | ≥ 200 koneksi (`max_concurrent_uploads`), memori per upload konstan |
| p95 latency `presign` (di Raspi) | < 50 ms |
| p95 latency `PUT /uploads/{id}` untuk 400 KB (sisi server, tidak termasuk jaringan klien) | < 300 ms |
| p95 latency `submissions` (termasuk cek file di disk) | < 500 ms |
| Error rate saat load test | < 0.5% |
| Ukuran foto setelah kompresi | rata-rata ≤ 500 KB, maks 2 MB |
| Retensi data | 90 hari (configurable) |
| Kapasitas OTP | ≈ 40 OTP/menit (dibatasi jeda 1,5 detik `wa-notif`); estimasi tunggu ditampilkan ke user |
| p95 latency `auth/register`, `auth/otp/request` | < 100 ms (hanya menulis antrean; kirim WhatsApp asinkron) |
| Waktu OTP terkirim saat tidak ramai | p95 < 10 detik sejak diminta |

## 11. Deployment

### 11.1 Hardware Raspi & PostgreSQL shared
- Raspberry Pi 4 (aarch64), RAM 4 GB (kondisi saat ini ±3,8 GB, dibagi dengan `wa-notif` dan service lain).
- **Ethernet** (bukan WiFi) untuk produksi.
- Heatsink + fan untuk mencegah thermal throttling.
- **Storage:** saat ini hanya SD card (`/dev/mmcblk0p2`, ±12 GB bebas). **SSD untuk foto (§8) sudah terpasang dan siap** (24 Okt 2026): Samsung 970 EVO 250 GB (NVMe) di enclosure USB 3.0 berchip RTL9210, driver `uas`, link 5 Gbps, `throttled=0x0`. Partisi tunggal **ext4** (label `photos`, UUID `76b14e6c-3abf-448f-969d-5350020b7e76`), ter-mount di `/mnt/ssd` lewat `/etc/fstab` (`defaults,noatime,nofail,x-systemd.device-timeout=10s`). Terukur: tulis sekuensial ±211 MB/s, baca ±353 MB/s. Adapter `localfs` dengan urutan penuh (fsync file, mkdir, rename, fsync direktori) untuk file 400 KB: 38–54 file/s sekuensial, 85 file/s (4 upload paralel), **312 file/s / ±128 MB/s (16 upload paralel)**, jauh di atas target 30 foto/s. **Harus di port USB 3 (biru)**: pada USB 2.0 kecepatannya hanya ±35 MB/s (periksa `lsusb -t`: `Driver=uas`, 5000M). Direktori `/mnt/ssd/photo-annotated/photos` (dengan `tmp/`) dimiliki UID 65532 (mode 750). Data PostgreSQL tetap di SD card untuk sementara (keputusan #18) dan bisa dipindah ke SSD yang sama nanti dengan mengubah bind mount `./data` di compose shared.
- **Internet (terukur dari Pi, Okt 2026):** Ethernet 1 Gbps; ±187 Mbps download / ±179 Mbps upload (multi-koneksi), ±117 / ±92 Mbps (satu koneksi), ping ±20 ms. Foto masuk memakai sisi download; melihat foto, export, dan backup memakai sisi upload.
- **PostgreSQL 17 berjalan sebagai satu container shared** yang didefinisikan di `/home/myrpi/docker/postgres/docker-compose.yml` (di luar repo ini):
  - Image `postgres:17-alpine`, container `shared-postgres`, `max_connections=100`, `shared_buffers=256MB`.
  - Port hanya di `127.0.0.1:5432` (admin lewat SSH). Service lain bergabung ke network Docker eksternal **`shared-db`** dan memakai host `shared-postgres:5432`.
  - Satu **database + role per service**, dibuat dengan `/home/myrpi/docker/postgres/create-service-db.sh <nama>` (mencetak `DATABASE_URL` dengan password acak). Untuk project ini: `./create-service-db.sh photo_annotated`.
  - Password superuser ada di `/home/myrpi/docker/postgres/.env` (mode 600, tidak di-commit). Service tidak pernah memakai superuser.
  - Batas memori `1g` di compose belum berlaku karena kernel Pi belum mengaktifkan memory cgroup; aktifkan (`cgroup_enable=memory cgroup_memory=1` di `cmdline.txt`) bila ingin batas itu ditegakkan.
- Prasyarat project ini: jalankan `create-service-db.sh photo_annotated`, simpan `DATABASE_URL` ke env `api`, lalu jalankan `cmd/migrate` sebelum API pertama kali dijalankan.
- Karena instance dipakai bersama, jumlah `database.max_conns` semua service digabung harus < `max_connections` (100) dikurangi cadangan untuk admin.

### 11.2 Container (Docker Compose)
- `api` — binary Go (image arm64, multi-stage build, distroless/alpine).
- `cloudflared` — Cloudflare Tunnel ke `api`.
- Tidak ada service `postgres` di compose project ini. `api` bergabung ke dua network eksternal:
  - `shared-db` → PostgreSQL (`DATABASE_URL=postgres://photo_annotated:…@shared-postgres:5432/photo_annotated?sslmode=disable`).
  - `wa-notif_default` → `wa-notif` (`wa_notif.base_url = http://wa-notif-service:3030`), sehingga tidak bergantung pada port host.
- `config.json` di-mount read-only ke container `api`.
- Root foto di-bind-mount dari SSD: `/mnt/ssd/photo-annotated/photos:/data/photos` (baca-tulis). Jika SSD belum ter-mount, bind mount mengarah ke direktori kosong di SD card; karena itu `storage.local.require_marker` wajib `true` di produksi dan penanda `.photo-annotated-store` harus ada di SSD (§8.2).
- `ulimits: nofile: 65536` untuk `api` (banyak koneksi dan file terbuka bersamaan).
- Implementasi (`Dockerfile`, `docker-compose.yml` di root repo): image `distroless/static` nonroot (UID 65532) ±16 MB berisi `api`, `migrate`, `maintenance`, `healthcheck`; dibangun silang di mesin dev (`docker buildx build --platform linux/arm64`) lalu dikirim ke Pi (`docker save | ssh … docker load`). Service `migrate` (one-shot) berjalan dulu, `api` start setelah migrasi sukses. Container `read_only`, `cap_drop: ALL`, `no-new-privileges`, HEALTHCHECK lewat biner `healthcheck` (distroless tanpa curl). Port API hanya `127.0.0.1:8080` di Pi; akses publik lewat `cloudflared` (profile `tunnel`, `TUNNEL_TOKEN` di `.env`, ingress ke `http://api:8080`).
- **Catatan Pi:** kernel di-boot dengan `cgroup_disable=memory`, sehingga batas memori container (`api` 512 MB, `shared-postgres` 1 GB) **tidak ditegakkan** (`docker info`: "No memory limit support"). Mengaktifkannya: tambahkan `cgroup_enable=memory` di `/boot/firmware/cmdline.txt` lalu reboot (belum dilakukan; butuh persetujuan).

### 11.3 Tuning awal
- Go `pgxpool`: `MaxConns = 20` (config: `database.max_conns`).
- HTTP server: `ReadHeaderTimeout 10s`, `IdleTimeout 60s`; **tanpa** `ReadTimeout`/`WriteTimeout` global. Rute JSON memasang deadline baca 10 detik dan tulis 15 detik lewat `http.ResponseController`; rute upload memakai deadline baca 60 detik (`limits.upload_timeout`); rute `export` dan `photos/{id}/content` memperpanjang deadline tulis sendiri. Alasannya: `ReadTimeout` global 10 detik akan memutus upload dari koneksi HP yang lambat.
- Upload: `max_concurrent_uploads` 200 (slot dilepas saat selesai/timeout), buffer salin 32 KB per upload, hanya header gambar yang di-decode.
- Tuning PostgreSQL dikelola di compose shared (§11.1), bukan di project ini.
- Dispatcher OTP: satu worker, timeout panggilan `wa-notif` 10 detik, maksimal 3 percobaan dengan jeda 3s → 10s (kegagalan permanen diketahui user dalam ±25 detik), probe status `wa-notif` tiap 30 detik.

### 11.4 Konfigurasi

Konfigurasi non-rahasia di `config.json` (path dari env `CONFIG_PATH`, default `./config.json`). **Secret hanya lewat env** dan tidak boleh ada di file config yang di-commit; env menimpa nilai file.

`config.json.example`:
```json
{
  "app": {
    "env": "production",
    "http_addr": ":8080",
    "public_base_url": "https://api.example.com",
    "allowed_origins": ["https://app.example.com"],
    "trusted_proxies": ["127.0.0.1/32", "::1/128", "172.16.0.0/12"]
  },
  "auth": {
    "mode": "otp",
    "session_ttl": "720h",
    "otp": {
      "length": 6,
      "ttl": "5m",
      "max_verify_attempts": 5,
      "resend_cooldown": "60s",
      "max_per_phone_per_hour": 5,
      "max_queue": 200,
      "daily_global_cap": 500,
      "message_template": "*{code}* adalah kode verifikasi Photo Annotated Anda. Berlaku {ttl_minutes} menit. Jangan bagikan kode ini ke siapa pun."
    }
  },
  "wa_notif": {
    "base_url": "http://wa-notif-service:3030",
    "timeout": "10s",
    "health_probe_interval": "30s",
    "send_interval": "1.5s"
  },
  "admin": {
    "enabled": true,
    "username": "admin",
    "password_bcrypt": "$2y$10$<hash>",
    "token_ttl": "8h"
  },
  "edit": { "window": "24h" },
  "turnstile": { "enabled": true },
  "database": { "max_conns": 20 },
  "storage": {
    "driver": "local",
    "local": {
      "root": "/data/photos",
      "require_marker": true,
      "min_free_bytes": 5368709120
    },
    "upload_url_ttl": "10m",
    "view_url_ttl": "5m"
  },
  "limits": {
    "max_upload_bytes": 2097152,
    "max_photos_per_submission": 5,
    "max_annotations_per_photo": 50,
    "max_annotation_text_len": 1000,
    "max_concurrent_uploads": 200,
    "upload_timeout": "60s",
    "rate_limit_per_min": { "presign": 30, "submissions": 10, "otp_request": 5, "otp_verify": 20, "admin_login": 5 }
  },
  "retention": {
    "days": 90,
    "pending_photo_ttl": "24h",
    "cleanup_interval": "1h",
    "cleanup_batch": 100,
    "orphan_sweep_interval": "24h",
    "tmp_max_age": "1h",
    "orphan_min_age": "24h"
  }
}
```

Env (secret):
```
DATABASE_URL                # dari create-service-db.sh photo_annotated
STORAGE_SIGNING_SECRET      # kunci HMAC URL upload & lihat foto (≥ 32 byte acak)
TURNSTILE_SECRET            # jika turnstile.enabled
EDIT_TOKEN_SECRET           # wajib jika auth.mode = none
OTP_PEPPER                  # wajib jika auth.mode = otp (kunci HMAC kode OTP, ≥ 32 byte acak)
WA_NOTIF_TOKEN              # wajib jika auth.mode = otp (bearer token wa-notif)
ADMIN_TOKEN_SECRET          # wajib jika admin.enabled (kunci HMAC token admin, ≥ 32 byte acak)
CONFIG_PATH
```

### 11.5 Backup
- **Database:** backup PostgreSQL shared belum ada. Minimal: `pg_dump` harian per database (termasuk `photo_annotated`) dari `shared-postgres`, dikompres, retensi 14 hari. Data DB masih di SD card, jadi backup ini wajib sebelum go-live.
- **Foto:** satu SSD tanpa replika adalah satu titik kegagalan. Minimal salinan berkala (`rsync` inkremental) ke disk atau lokasi lain. Tujuan backup (disk kedua, mesin lain di LAN, atau cloud) belum ditentukan (§14.2).
- Uji restore minimal sekali sebelum go-live.
- Catatan: data lebih tua dari retensi (90 hari) memang dihapus dari DB dan storage; backup tidak dimaksudkan untuk menyimpannya lebih lama.

## 12. Observability

- Log terstruktur JSON (`log/slog`) ke stdout → Promtail → Loki.
- Field log minimum: `request_id`, `method`, `path`, `status`, `duration_ms`, `client_ip`, `user_id` (jika ada). **Jangan** log `edit_token`, header `Authorization`, kode OTP, isi pesan WhatsApp, atau isi teks anotasi. Nomor HP hanya dalam bentuk tersamar.
- Dashboard Grafana: request rate, error rate, latency per endpoint, jumlah foto `pending`, hasil cleanup job (foto yatim & retensi), suhu CPU Raspi, pemakaian dan sisa ruang SSD, upload aktif vs `max_concurrent_uploads`, latency dan ukuran upload, jumlah `503 UPLOAD_BUSY`/`STORAGE_FULL`, trafik jaringan `eth0`, jumlah challenge OTP per status (`queued`/`sent`/`failed`), panjang antrean dan waktu tunggu OTP, OTP terkirim hari ini vs `daily_global_cap`.
- Alert: error rate > 2% selama 5 menit; suhu CPU > 75°C; cleanup job gagal berturut-turut; SSD terpakai > 80% atau sisa ruang < `min_free_bytes`; root foto tidak ter-mount (`STORAGE_UNAVAILABLE`); error I/O disk di log kernel; trafik masuk `eth0` > 80% kapasitas ISP selama 5 menit; panggilan `wa-notif` gagal > 20% selama 5 menit atau `503 NOT_CONNECTED` (sesi WhatsApp terputus, perlu scan ulang QR); antrean OTP > 80% `max_queue`; OTP harian > 80% `daily_global_cap`.
- `GET /readyz` hanya memeriksa DB. `wa-notif` yang mati tidak membuat API dianggap tidak siap (fitur lain tetap berjalan); statusnya dipantau lewat alert di atas.

## 13. Load Testing

Tool: k6, dijalankan dari mesin lain (bukan Raspi).

Skenario utama:
1. Ramp up ke 1000 VU dalam 2 menit, tahan 5 menit.
2. Tiap VU: `presign` → PUT 400 KB ke URL upload (**harus gambar WebP/JPEG valid dengan dimensi sesuai deklarasi**, karena server memvalidasi isi file) → `submissions` dengan 3 anotasi (masing-masing bertext) → sekitar 10% VU melakukan `PUT /submissions/{id}` → sleep **25–40 detik**. Dengan 1000 VU ini menghasilkan ±25–40 foto/s (±80–130 Mbps), di bawah kapasitas internet rumah. Sleep 5–15 detik (±100 foto/s, ±320 Mbps) akan melampaui kapasitas ISP dan hanya mengukur internet, bukan aplikasi.
3. Skenario burst: 1000 VU submit dalam 10 detik **memakai foto yang sudah ter-upload** (burst upload 1000 foto dalam 10 detik ≈ 320 Mbps melebihi kapasitas internet).
   - Uji klien lambat: 200 koneksi upload bersamaan dibatasi ±200 kbps (mis. `tc` atau proxy pembatas) untuk memastikan slot `max_concurrent_uploads`, timeout 60 detik, dan `503 UPLOAD_BUSY` berperilaku benar tanpa lonjakan memori.
4. Uji terpisah: `GET /admin/export` pada dataset besar saat beban berjalan, memastikan memori API stabil.
5. Alur registrasi/login diuji dengan **stub `wa-notif`** (server HTTP tiruan yang meniru `POST /api/v1/messages/send` dengan jeda 1,5 detik), **jangan pernah** terhadap `wa-notif` asli: beban uji akan mengirim WhatsApp sungguhan dan bisa membuat nomor pengirim diblokir. Tujuannya memastikan antrean, `OTP_BUSY`, dan estimasi tunggu berperilaku benar. Pengujian terhadap `wa-notif` asli cukup manual ke nomor sendiri (beberapa pesan).
6. Untuk skenario 1–3, VU memakai session yang sudah dibuat sebelumnya (seed lewat DB) agar tidak melewati OTP.

Penguji dijalankan dari luar jaringan rumah (lewat internet dan Tunnel) agar jalur ISP ikut teruji, dan tidak ada pemakaian produksi nyata saat tes.

Kriteria lulus: target di §10 terpenuhi, tanpa thermal throttling, CPU (API dan `cloudflared`) tidak jenuh, SSD tidak I/O-bound (iowait rendah), koneksi DB tidak habis (termasuk dampaknya ke layanan lain di PostgreSQL yang sama).

## 14. Keputusan & Open Questions

### 14.1 Keputusan (dari v0.1)

| # | Pertanyaan | Keputusan |
|---|---|---|
| 1 | Autentikasi | Configurable lewat `config.json`: `none` atau `otp` (registrasi nama + email + nomor HP, OTP via WhatsApp/`wa-notif`), §4.0, §9 |
| 2 | Field form | Tidak ada field terstruktur: foto + anotasi + teks bebas per anotasi |
| 3 | Label anotasi | Bebas (teks bebas pada anotasi); tidak ada tabel/endpoint `labels` |
| 4 | Retensi data | 3 bulan (90 hari), configurable, dihapus oleh cleanup job (§8.3) |
| 5 | Export | Tidak ada export untuk user; disiapkan export admin ke JSONL yang bisa dibaca model (§5.7), format lain lewat port `Exporter` |
| 6 | Edit setelah submit | Boleh, hanya oleh pemilik (§4.4, §9) |

### 14.1b Keputusan (dari v0.3)

| # | Topik | Keputusan |
|---|---|---|
| 7 | Registrasi | Nama + email + nomor HP; nomor diverifikasi OTP 6 digit via WhatsApp lewat `wa-notif`; tanpa password; login berikutnya juga lewat OTP (§4.0) |
| 8 | Pengiriman OTP | Asinkron lewat antrean di DB + satu worker, karena `wa-notif` mengirim satu pesan per 1,5 detik (§4.0, §9.5) |
| 9 | Mode auth | Hanya `none` dan `otp` di v1; `static_token`/`jwt` dihapus dari scope (bisa ditambah lewat port `Authenticator`) |
| 10 | PostgreSQL | Versi 17, satu container shared (`shared-postgres`) di `/home/myrpi/docker/postgres`, database + role per service, network `shared-db` |
| 11 | Nomor HP | Hanya nomor seluler Indonesia (`62 8…`); nomor internasional di luar scope v1 (§7.4) |
| 12 | Jendela edit | 24 jam sejak `created_at`, configurable `edit.window` (§4.4) |
| 13 | Pemulihan `edit_token` | Tidak perlu; token hilang = tidak bisa edit (mode `none`) |
| 14 | Export | COCO/YOLO belum perlu, tetapi penyimpanan dijaga tanpa kehilangan informasi agar bisa diekspor ke format model apa pun nanti (§3.2) |
| 15 | Admin | Satu akun statis (username + password bcrypt) di `config.json`, login `/admin/login`, independen dari `auth.mode` (§5.9, §9.1); menggantikan keputusan "admin tidak ada di mode `none`" |
| 16 | Kegagalan WhatsApp | FE menampilkan masalah (status challenge + `channel-status`) dan menawarkan kirim ulang; tidak ada kanal cadangan (§4.0, §9.5) |
| 17 | Retensi data user | Tanpa batas untuk sekarang (ditinjau ulang sebelum go-live karena berisi data pribadi) |
| 18 | Storage PostgreSQL | Tetap di SD card untuk sementara |
| 19 | Penyimpanan foto | SSD lokal di Raspi (ext4), bukan Cloudflare R2/object storage eksternal. Alasan: R2 butuh kartu kredit; internet Pi terukur ±187 Mbps down / ±179 Mbps up, cukup untuk ±30 foto/s (§8) |
| 20 | Mekanisme upload | `POST /uploads/presign` tetap, tetapi URL menunjuk ke API sendiri (`PUT /uploads/{id}` bertanda tangan HMAC); API men-stream ke disk dan memvalidasi isi file (§5.1, §5.1b). Kontrak ke FE sama dengan presign S3 |
| 21 | SSD | Terpasang dan ter-mount di `/mnt/ssd` (ext4, USB 3), dengan file penanda `.photo-annotated-store` di root foto. Pengembangan di mesin dev memakai direktori biasa (`require_marker=false`); produksi wajib `require_marker=true` (§8.2) |
| 22 | Storage eksternal | Tidak di v1; port `PhotoStore` memungkinkan adapter S3/B2 nanti tanpa mengubah FE |

### 14.2 Open Questions

Sisa pertanyaan yang belum diputuskan:

1. **Dataset untuk model vs retensi 90 hari:** foto dan anotasi dihapus setelah 90 hari, sedangkan export v1 hanya memberi referensi/URL sementara. Jika data akan dipakai melatih model, dari mana salinan foto diambil dan di mana disimpan? Karena foto ada di SSD lokal, menyalin murah. Opsi: (a) job export berkala yang menyalin foto + JSONL ke direktori dataset terpisah di SSD (di luar jangkauan retensi) sebelum hari ke-90; (b) perpanjang retensi (SSD 256 GB muat ±650 ribu foto); (c) admin menyalin manual dengan `rsync` memakai `storage_key` dari export. Menentukan apakah export perlu menyalin foto, bukan hanya mereferensikannya.
2. **Nomor tanpa WhatsApp:** hasil pengecekan kode `wa-notif`: tidak ada pengecekan `IsOnWhatsApp` sebelum kirim; `SendMessage` whatsmeow dipanggil langsung. Perilaku sebenarnya untuk nomor tanpa akun WhatsApp belum teruji (bisa mengembalikan error, bisa juga `SENT` tanpa pernah sampai). Perlu satu uji manual dengan nomor tanpa WhatsApp. Opsi perbaikan: menambah endpoint cek nomor di `wa-notif` (`IsOnWhatsApp`) sehingga FE bisa memberi tahu "nomor ini tidak memakai WhatsApp" sebelum OTP dikirim. Ini mengubah project `wa-notif`; belum dilakukan.
3. **Backup (DB dan foto):** DB di SD card (keputusan #18) dan foto di satu SSD tanpa replika. Belum ditentukan tujuan backup (disk kedua, mesin lain di LAN, atau cloud), jadwal `pg_dump` harian, dan `rsync` foto (§11.5).
4. ~~Nasib data lama di SSD dan mount~~ **Selesai (24 Okt 2026):** data lama sudah di-backup pengguna, SSD diformat ext4 dan ter-mount (§11.1). Tersisa: uji `require_marker` pada binary API (tugas T10) dan konfirmasi UID container `api` = 65532 saat image dibuat (T31).
5. **CPU Pi di bawah beban upload:** `cloudflared` (Pi 4 tanpa instruksi crypto hardware) dan hashing SHA-256 saat upload belum terukur; ukur lewat load test §13 (target 30 foto/s). Jika CPU jenuh: turunkan `max_concurrent_uploads`, hitung SHA-256 secara asinkron, atau pindah ke storage eksternal lewat adapter baru.

Dijawab di v0.4 (lihat §14.1b): batas waktu edit, pemulihan `edit_token`, export COCO/YOLO, admin di mode `none`, cadangan nomor WhatsApp, nomor internasional, retensi data user, dan lokasi storage.

## 15. Milestones

0. **M0 — Infrastruktur:** PostgreSQL 17 shared di `/home/myrpi/docker/postgres` (compose sudah dibuat dan berjalan), buat database `photo_annotated` lewat `create-service-db.sh`. Pasang SSD (ext4, mount by UUID, `noatime`) dan buat `/mnt/ssd/photo-annotated/photos` dengan pemilik UID container `api`; sampai SSD terpasang, pengembangan memakai direktori biasa (`require_marker=false`).
1. **M1 — Backend inti:** kerangka Clean Architecture + Gin, config loader, migrasi DB, presign + upload ke disk (`PhotoStore` adapter `localfs`, validasi isi file, URL bertanda tangan, penjagaan mount/ruang), submissions (POST), cleanup job (foto yatim, file yatim di disk, retensi).
2. **M2 — Auth OTP:** `users`/`otp_challenges`/`sessions`, normalisasi nomor, adapter `wanotif`, dispatcher antrean, `auth/*`, middleware session, pembatasan §9.5, uji manual ke `wa-notif` asli. Dikerjakan sebelum FE karena presign dan submit membutuhkan session.
3. **M3 — Frontend:** layar registrasi/login OTP, kamera, resize/kompresi, upload dengan progress.
4. **M4 — Anotasi:** integrasi Annotorious, teks per kotak, validasi koordinat, render ulang.
5. **M5 — Edit:** mode `none` dengan `edit_token`, `GET`/`PUT /submissions/{id}`, `GET /photos/{id}/url` + `/content`, kontrol akses pemilik, jendela edit 24 jam.
6. **M6 — Admin, export & hardening:** `POST /admin/login`, `GET /admin/export` (jsonl), rate limit, Turnstile, backup, alert.
7. **M7 — Load test & go-live:** jalankan skenario §13 (dengan stub `wa-notif`), tuning, deploy.
