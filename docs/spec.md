# Spec: Form dengan Upload & Anotasi Foto

> Status: Draft v0.1
> Target deploy: Raspberry Pi 4 (backend API) + Object Storage eksternal + CDN (frontend)

## 1. Ringkasan

Aplikasi web untuk mengisi form yang menyertakan satu atau lebih foto. User bisa mengambil/memilih foto, lalu membuat anotasi berupa bounding box (kotak) dengan label di atas foto tersebut. Semua pekerjaan berat (ambil foto, resize/kompresi, anotasi) dilakukan di browser. Upload foto dilakukan langsung dari browser ke object storage memakai presigned URL, sehingga backend di Raspberry Pi hanya menangani request JSON yang ringan.

## 2. Goals & Non-Goals

### Goals
- Mendukung **500–1000 user aktif bersamaan** di atas satu Raspberry Pi 4.
- Backend tidak pernah menerima/memproses byte file foto.
- Anotasi disimpan sebagai data terstruktur (bisa di-query, di-render ulang, di-export).
- Bekerja dengan baik di mobile browser (kamera belakang, koneksi lambat).

### Non-Goals (untuk v1)
- Image processing di server (thumbnail generation, ML inference, dsb).
- Bentuk anotasi selain rectangle (polygon, freehand, keypoint).
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
┌──────────────┐  (1) presign    ┌─────────────────────┐
│   Browser    │ ──────────────▶ │ Cloudflare Tunnel   │
│              │ ◀────────────── │        ▼            │
│ - kamera     │   URL + key     │ Raspberry Pi 4      │
│ - resize     │                 │  - Go API           │
│ - anotasi    │  (3) submit     │  - PostgreSQL (SSD) │
│              │ ──────────────▶ │                     │
└──────┬───────┘                 └──────────┬──────────┘
       │ (2) PUT foto                       │ HeadObject (verifikasi)
       ▼                                    ▼
┌────────────────────────────────────────────────────┐
│ Object Storage (S3-compatible, rekomendasi: R2)    │
└────────────────────────────────────────────────────┘
```

### Komponen

| Komponen | Teknologi | Lokasi | Tanggung jawab |
|---|---|---|---|
| Frontend | SPA statis (mis. React/Svelte + Vite) | CDN | Form, kamera, resize, anotasi, upload |
| Library anotasi | Annotorious (alternatif: Konva.js) | FE | Gambar & edit bounding box + label |
| API | Go (`net/http` / chi), `aws-sdk-go-v2`, `pgx` | Raspi | Presign, validasi, simpan form & anotasi |
| Database | PostgreSQL 16 | Raspi (SSD) | Submission, foto, anotasi |
| Object storage | Cloudflare R2 (S3-compatible) | Eksternal | Simpan file foto |
| Ingress | Cloudflare Tunnel (`cloudflared`) | Raspi | Expose API tanpa port forwarding, TLS, rate limit edge |
| Observability | Promtail → Loki → Grafana | Raspi (`pisee`) | Log & dashboard |

**Aturan penting:** FE bundle tidak boleh di-serve dari Raspi. Uplink internet rumah adalah resource paling terbatas.

## 4. Alur Utama

### 4.1 Isi form + upload foto

1. User membuka app (FE dari CDN).
2. User mengisi field form.
3. User mengambil/memilih foto.
4. FE memproses foto (lihat §6): koreksi orientasi, resize, encode ulang.
5. FE memanggil `POST /api/v1/uploads/presign` dengan `content_type` dan `size_bytes` hasil proses.
6. API memvalidasi, membuat record `photos` berstatus `pending`, lalu mengembalikan presigned PUT URL.
7. FE melakukan `PUT` langsung ke object storage. Progress ditampilkan ke user.
8. User membuat anotasi (kotak + label) di atas foto. Anotasi bisa dibuat sebelum atau sesudah upload selesai.
9. User menekan Submit. FE memanggil `POST /api/v1/submissions` berisi data form, daftar `photo_id`, dan anotasi tiap foto.
10. API memverifikasi setiap foto (`HeadObject`: ada, ukuran & content-type cocok), lalu menyimpan submission, foto, dan anotasi dalam **satu transaksi**. Status foto berubah menjadi `attached`.

### 4.2 Upload gagal / user batal
- Jika PUT gagal, FE retry maksimal 3 kali dengan exponential backoff. Jika URL expired, FE meminta presign baru.
- Foto yang tetap `pending` lebih dari 24 jam dihapus oleh cleanup job (lihat §8.3).

### 4.3 Melihat foto (admin / review)
- Bucket **private**. Foto diakses via `GET /api/v1/photos/{id}/url` yang mengembalikan presigned GET URL berumur pendek.

## 5. API

Base path: `/api/v1`. Semua request & response berformat JSON. Error memakai format:

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
    "url": "https://<account>.r2.cloudflarestorage.com/forms/uploads/2026/10/0b6c....webp?X-Amz-...",
    "headers": { "Content-Type": "image/webp" },
    "expires_at": "2026-10-23T10:15:00Z"
  }
}
```

Catatan:
- Object key **selalu di-generate server**: `uploads/{yyyy}/{mm}/{uuid}.{ext}`. Client tidak pernah menentukan key.
- Expiry presigned URL: 10 menit.
- `Content-Type` ikut di-sign, jadi client wajib mengirim header yang sama.

### 5.2 `POST /submissions`

Request:
```json
{
  "form": {
    "nama": "Budi",
    "lokasi": "Gudang A",
    "catatan": "Paket rusak di sisi kiri"
  },
  "photos": [
    {
      "photo_id": "0b6c1c2e-6a5e-4c55-9c0b-2f6c2d8e9a11",
      "annotations": [
        { "label": "kerusakan", "x": 0.12, "y": 0.30, "w": 0.25, "h": 0.18 },
        { "label": "label_resi", "x": 0.55, "y": 0.10, "w": 0.30, "h": 0.12 }
      ]
    }
  ],
  "client_request_id": "c1f7e0a4-..."
}
```

Validasi:
- Field form sesuai schema form (lihat §7.4).
- Jumlah foto: 1–5 per submission.
- Setiap `photo_id` harus ada, berstatus `pending`, dan belum dipakai submission lain.
- `HeadObject` tiap foto: object ada, `ContentLength` ≤ batas dan sama dengan yang dideklarasikan, `ContentType` cocok. Jika tidak cocok → tolak dengan `422` dan tandai foto `rejected`.
- Anotasi: maksimal 50 per foto; koordinat valid (§6.3); `label` ada di daftar label yang diizinkan.
- `client_request_id` dipakai sebagai **idempotency key** agar double-submit (tap dua kali, retry jaringan) tidak membuat duplikat.

Response `201`:
```json
{ "submission_id": "7d2a...", "created_at": "2026-10-23T10:05:12Z" }
```

### 5.3 `GET /submissions/{id}`
Mengembalikan data form, metadata foto, dan anotasi. (Akses dibatasi — lihat §9.)

### 5.4 `GET /photos/{id}/url`
Response: `{ "url": "...", "expires_at": "..." }` — presigned GET, expiry 5 menit.

### 5.5 `GET /labels`
Daftar label yang diizinkan untuk anotasi (di-cache FE).

### 5.6 Operasional
- `GET /healthz` — liveness (proses hidup).
- `GET /readyz` — readiness (DB bisa di-ping).

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
- Bentuk: rectangle, axis-aligned.
- Koordinat **dinormalisasi 0–1** relatif terhadap dimensi foto **hasil proses** (yang di-upload), origin di kiri atas.
- `x`, `y` = pojok kiri atas; `w`, `h` = lebar & tinggi.
- Aturan validasi (FE & BE): `0 ≤ x,y < 1`, `0 < w,h ≤ 1`, `x + w ≤ 1`, `y + h ≤ 1`, dan ukuran minimum `w,h ≥ 0.005` untuk mencegah kotak tak sengaja.
- Anotasi tidak di-"burn" ke gambar. Render overlay dilakukan di client.

### 6.4 UX
- Tampilkan progress upload per foto; tombol Submit nonaktif sampai semua upload selesai.
- Simpan draft form & anotasi di `localStorage` agar tidak hilang saat reload (file foto tidak disimpan di draft v1).

## 7. Data Model (PostgreSQL)

### 7.1 `submissions`
| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | `uuid` PK | |
| `form_data` | `jsonb` NOT NULL | Isi field form, tervalidasi terhadap schema |
| `form_version` | `int` NOT NULL | Versi schema form |
| `client_request_id` | `uuid` UNIQUE NOT NULL | Idempotency key |
| `client_ip` | `inet` | Dari header `CF-Connecting-IP` |
| `created_at` | `timestamptz` NOT NULL DEFAULT now() | |

### 7.2 `photos`
| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | `uuid` PK | |
| `submission_id` | `uuid` FK → submissions NULL | NULL selama `pending` |
| `object_key` | `text` UNIQUE NOT NULL | |
| `content_type` | `text` NOT NULL | |
| `size_bytes` | `int` NOT NULL | Dideklarasikan saat presign |
| `width`, `height` | `int` NOT NULL | |
| `status` | `text` NOT NULL | `pending` / `attached` / `rejected` / `deleted` |
| `created_at` | `timestamptz` NOT NULL DEFAULT now() | |

Index: `(status, created_at)` untuk cleanup job; `(submission_id)`.

### 7.3 `annotations`
| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | `bigserial` PK | |
| `photo_id` | `uuid` FK → photos NOT NULL | ON DELETE CASCADE |
| `label_id` | `int` FK → labels NOT NULL | |
| `x`, `y`, `w`, `h` | `real` NOT NULL | Dengan CHECK constraint sesuai §6.3 |
| `created_at` | `timestamptz` NOT NULL DEFAULT now() | |

Index: `(photo_id)`, `(label_id)`.

### 7.4 `labels` & schema form
- `labels(id serial PK, code text UNIQUE, display_name text, active bool)`.
- Schema field form v1 didefinisikan di kode/config (bukan DB) dan di-versioning lewat `form_version`.

## 8. Object Storage

### 8.1 Bucket
- Satu bucket private, misal `forms`.
- Rekomendasi Cloudflare R2: S3-compatible dan tanpa biaya egress.
- **Jangan** menjalankan MinIO di Raspi — bertentangan dengan tujuan arsitektur.

### 8.2 CORS
```json
[
  {
    "AllowedOrigins": ["https://app.example.com"],
    "AllowedMethods": ["PUT", "GET"],
    "AllowedHeaders": ["Content-Type"],
    "MaxAgeSeconds": 3600
  }
]
```

### 8.3 Cleanup
- Job periodik di API (tiap 1 jam): ambil `photos` berstatus `pending` dengan `created_at < now() - 24h`, panggil `DeleteObject`, set status `deleted`. Batch 100 per putaran.
- Foto `rejected` dihapus dengan cara yang sama.

## 9. Keamanan

- **Akses aplikasi:** _belum diputuskan_ (lihat §14). Default v1: form publik dengan Cloudflare Turnstile diverifikasi saat presign dan submit.
- **Endpoint baca** (`GET /submissions/{id}`, `GET /photos/{id}/url`): wajib autentikasi admin (mis. Cloudflare Access di depan path admin, atau token).
- **Rate limit** (per IP, dari `CF-Connecting-IP`):
  - presign: 30 / menit
  - submissions: 10 / menit
  - Tambahan rate limit di edge Cloudflare.
- Kredensial object storage hanya ada di Raspi (env/secret), dengan scope token minimum: hanya bucket `forms`, izin object read/write/delete.
- Presigned URL berumur pendek (PUT 10 menit, GET 5 menit).
- Validasi ukuran ganda: saat presign (deklarasi) dan saat submit (`HeadObject`).
- Request body API dibatasi 64 KB (`http.MaxBytesReader`).
- Semua query memakai parameterized query (`pgx`).

## 10. Non-Functional Requirements

| Metrik | Target |
|---|---|
| User aktif bersamaan | 1000 |
| Throughput puncak API | ≥ 150 req/s tanpa error |
| p95 latency `presign` (di Raspi) | < 50 ms |
| p95 latency `submissions` (termasuk HeadObject) | < 500 ms |
| Error rate saat load test | < 0.5% |
| Ukuran foto setelah kompresi | rata-rata ≤ 500 KB, maks 2 MB |

## 11. Deployment

### 11.1 Hardware Raspi
- Raspberry Pi 4, RAM minimal 4 GB.
- **Ethernet** (bukan WiFi) untuk produksi.
- SSD via USB 3.0 untuk data PostgreSQL (bukan SD card).
- Heatsink + fan untuk mencegah thermal throttling.

### 11.2 Container (Docker Compose)
- `api` — binary Go (image arm64, multi-stage build, distroless/alpine).
- `postgres` — PostgreSQL 16, volume di SSD.
- `cloudflared` — Cloudflare Tunnel ke `api`.

### 11.3 Tuning awal
- PostgreSQL: `max_connections = 50`, `shared_buffers = 512MB` (4 GB RAM) atau `1GB` (8 GB RAM).
- Go `pgxpool`: `MaxConns = 20`.
- HTTP server: `ReadTimeout 10s`, `WriteTimeout 15s`, `IdleTimeout 60s`.
- Client S3: reuse satu instance; timeout `HeadObject` 3 detik.

### 11.4 Konfigurasi (env)
```
APP_ENV, HTTP_ADDR
DATABASE_URL
S3_ENDPOINT, S3_REGION, S3_BUCKET, S3_ACCESS_KEY_ID, S3_SECRET_ACCESS_KEY
PRESIGN_PUT_TTL=10m, PRESIGN_GET_TTL=5m
MAX_UPLOAD_BYTES=2097152, MAX_PHOTOS_PER_SUBMISSION=5
ALLOWED_ORIGINS
TURNSTILE_SECRET
```

### 11.5 Backup
- `pg_dump` harian, dikompres, di-upload ke bucket terpisah (`forms-backup`). Retensi 14 hari.
- Uji restore minimal sekali sebelum go-live.

## 12. Observability

- Log terstruktur JSON (`log/slog`) ke stdout → Promtail → Loki.
- Field log minimum: `request_id`, `method`, `path`, `status`, `duration_ms`, `client_ip`.
- Dashboard Grafana: request rate, error rate, latency per endpoint, jumlah foto `pending`, hasil cleanup job, suhu CPU Raspi.
- Alert: error rate > 2% selama 5 menit; suhu CPU > 75°C.

## 13. Load Testing

Tool: k6, dijalankan dari mesin lain (bukan Raspi).

Skenario utama:
1. Ramp up ke 1000 VU dalam 2 menit, tahan 5 menit.
2. Tiap VU: `presign` → PUT file dummy 400 KB ke storage → `submissions` dengan 3 anotasi → sleep 5–15 detik.
3. Skenario burst: 1000 VU submit dalam 10 detik.

Kriteria lulus: target di §10 terpenuhi, tanpa thermal throttling, koneksi DB tidak habis.

## 14. Open Questions

1. **Autentikasi:** form publik (anonim + Turnstile) atau user harus login? Siapa yang boleh melihat hasil submission?
2. **Field form:** apa saja field-nya, wajib/opsional, dan apakah perlu beberapa jenis form?
3. **Label anotasi:** daftar label tetap atau user boleh membuat label bebas?
4. **Retensi data:** berapa lama foto & submission disimpan?
5. **Export:** perlu export hasil anotasi (CSV / COCO / YOLO format)?
6. **Edit setelah submit:** apakah submission boleh diedit setelah dikirim?

## 15. Milestones

1. **M1 — Backend inti:** presign, submissions, migrasi DB, cleanup job.
2. **M2 — Frontend:** form, kamera, resize/kompresi, upload dengan progress.
3. **M3 — Anotasi:** integrasi Annotorious, validasi koordinat, render ulang.
4. **M4 — Hardening:** rate limit, Turnstile, admin access, backup.
5. **M5 — Load test & go-live:** jalankan skenario §13, tuning, deploy.
