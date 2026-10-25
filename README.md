# Photo Annotated

Aplikasi web untuk mengumpulkan foto beranotasi: user memotret atau memilih hingga 5 foto, menandai bagian
penting dengan kotak (bounding box), lalu memilih **label** untuk setiap kotak dari daftar yang dikelola
admin. Admin punya dashboard untuk mengelola label, mereview hasil anotasi, mengelola annotator, melihat
statistik, dan mengekspor dataset (JSONL) untuk melatih model.

Berjalan di **Raspberry Pi 4**: API Go + PostgreSQL bersama + foto di SSD lokal, dibuka ke publik lewat
Cloudflare Tunnel. Login user memakai nomor HP + password (OTP WhatsApp untuk verifikasi nomor dan reset
password); login admin memakai OTP WhatsApp ke nomor superadmin di `config.json`.

## Fitur

- **User:** daftar/masuk, ambil foto (diperkecil ≤ 1920 px dan GPS dibuang di browser), upload dengan
  progres dan retry, gambar kotak + pilih label, edit selama 24 jam, draf tersimpan otomatis, riwayat dengan
  status review, tema Sistem/Terang/Gelap.
- **Admin (`/admin`):** status sistem (DB, SSD, WhatsApp, cleanup, suhu CPU), daftar & detail anotasi dengan
  overlay kotak, review terima/tolak (satuan dan massal), hapus permanen (DB + file di SSD), kelola label
  (tambah, ubah, urutkan, arsip, hapus), kelola annotator (blokir, hapus data, hapus akun), statistik per
  label, export JSONL.
- **Retensi:** submission otomatis terhapus setelah 90 hari, kecuali yang sudah diterima admin.

## Arsitektur

```
Browser (React SPA)
   │  HTTPS  cam.<domain>
Cloudflare Tunnel ──► nginx `web` (:8081)  ─ file statis + proxy /api ─►  API Go `api` (:8080)
                                                                           ├─ PostgreSQL 17 (shared)
                                                                           ├─ SSD /mnt/ssd (foto)
                                                                           └─ wa-notif (WhatsApp OTP)
```

- Foto di-upload langsung ke API lewat URL bertanda tangan berumur pendek dan di-stream ke disk (memori
  konstan per upload); isi file divalidasi dari header (JPEG/WebP, dimensi), tidak di-decode ulang.
- Anotasi disimpan terstruktur (koordinat ternormalisasi + `label_id` stabil sebagai indeks kelas).

## Struktur repo

| Path | Isi |
|---|---|
| `cmd/api` | Server HTTP |
| `cmd/migrate` | Migrasi skema (`up` / `down` / `status`), dijalankan sebelum API |
| `cmd/maintenance` | Pekerjaan pemeliharaan sekali jalan |
| `cmd/wanotif-stub` | Stub WhatsApp untuk dev, E2E, dan load test (tidak pernah mengirim pesan sungguhan) |
| `cmd/healthcheck`, `cmd/uploadbench`, `cmd/wanotif-check` | Healthcheck container, benchmark upload, cek sesi WhatsApp |
| `internal/domain` | Aturan bisnis murni (foto, anotasi, label, nomor HP, OTP) |
| `internal/usecase` | Alur aplikasi + port (interface) |
| `internal/adapter/*` | Implementasi port: `postgres`, `localfs` (penyimpanan foto), `wanotif`, `signer`, `turnstile`, `export`, … |
| `internal/delivery/http` | Router Gin, handler, middleware, test E2E HTTP |
| `migrations/` | Migrasi SQL (goose, disematkan ke binary) |
| `web/` | Frontend React 19 + Vite + TypeScript, lihat [web/README.md](web/README.md) |
| `deploy/observability` | Alloy + Loki + Prometheus + Grafana untuk Pi |
| `docs/` | Spec ([spec.md](docs/spec.md), [spec-labels-admin.md](docs/spec-labels-admin.md)) dan rencana kerja ([docs/tasks](docs/tasks)) |

## Menjalankan di lokal

Prasyarat: Go 1.26, Node 24 / npm 11, PostgreSQL 17, Docker (untuk build image).

```bash
# 1. config: salin contoh lalu isi (secret ada di bagian "secrets"); file ini di-gitignore
cp config.json.example config.json
#    untuk dev: app.env "development", storage.local.root ke folder lokal yang berisi file penanda
#    .photo-annotated-store, wa_notif.base_url ke stub, turnstile.enabled false

# 2. stub WhatsApp + migrasi + API
go run ./cmd/wanotif-stub -token dev-token &        # kode OTP terlihat di GET :13030/_stub/messages
go run ./cmd/migrate up
go run ./cmd/api

# 3. frontend (proxy /api ke API lokal)
cd web && npm ci
VITE_AUTH_MODE=otp VITE_DEV_API_TARGET=http://127.0.0.1:8080 npm run dev
```

## Test

```bash
# backend: test integrasi butuh PostgreSQL (TEST_DATABASE_URL, lihat .env.example);
# tanpa variabel itu test DB dilewati
set -a; . ./.env.local; set +a
go vet ./... && go test ./... -race

# frontend
cd web && npm run typecheck && npm test && npm run build

# E2E browser (Chrome headless) terhadap API + Vite lokal, lihat komentar di web/e2e/flow.mjs
cd web && npm run e2e -- <folder-kerja>
```

## Deploy (Raspberry Pi)

```bash
V=$(git rev-parse --short HEAD)
docker buildx build --platform linux/arm64 --build-arg VERSION=$V -t photo-annotated:$V --load .
docker buildx build --platform linux/arm64 -t photo-annotated-web:$V --load web/
# kirim ke Pi: docker save … | gzip | ssh <pi> 'gunzip | docker load'
# di Pi (folder berisi docker-compose.yml + config.json mode 640, chown <user>:65532):
docker compose up -d        # migrate jalan dulu, lalu api dan web
```

`docker-compose.yml` menjelaskan network yang dibutuhkan (`shared-db`, `wa-notif_default`) dan bind mount
SSD. API menolak start bila config tidak valid atau SSD belum ter-mount (file penanda tidak ada).

## Dokumentasi

- [docs/spec.md](docs/spec.md): spec induk (arsitektur, API, data model, keamanan, target performa)
- [docs/spec-labels-admin.md](docs/spec-labels-admin.md): label terkontrol & dashboard superadmin
- [docs/tasks/todo.md](docs/tasks/todo.md) dan [docs/tasks/plan.md](docs/tasks/plan.md): status task dan catatan tiap task
