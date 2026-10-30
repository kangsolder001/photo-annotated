# CLAUDE.md

Panduan untuk agent yang bekerja di repo ini. Ringkasan proyek: [README.md](README.md). Bahasa proyek:
**Indonesia** (komentar kode, pesan error untuk manusia, teks UI, dokumen, catatan task). Identifier
dan kode error API tetap bahasa Inggris.

## Sumber kebenaran

- Spec: [docs/spec.md](docs/spec.md) (induk) dan [docs/spec-labels-admin.md](docs/spec-labels-admin.md)
  (label + dashboard admin; menimpa bagian spec induk yang disebut di §11-nya). Kontrak API (bentuk JSON,
  status HTTP, kode error) mengikuti spec persis; bila implementasi harus menyimpang, perbarui spec dan
  catat alasannya.
- Task: [docs/tasks/todo.md](docs/tasks/todo.md) (daftar) dan [docs/tasks/plan.md](docs/tasks/plan.md)
  (detail: Hasil / Keputusan / Temuan / Verification). Catatan T37–T53 ada di `docs/tasks/notes-*.md`.
- Pendataan baseline (branch `feat/instrumen-baseline-rumah-tangga-cipadung-kulon`, T57+):
  [docs/spec-pendataan.md](docs/spec-pendataan.md), [todo](docs/tasks/todo-pendataan.md),
  [plan](docs/tasks/plan-pendataan.md). Task baru di branch ini masuk ke file tersebut.

## Alur kerja

- Branch kerja `feat/implementation`. **Satu commit per task** (`feat(<area>): T<n> <ringkas>`), setelah
  test hijau; perbarui `todo.md`/`plan.md` (dan spec bila berubah) di commit yang sama. Task baru dari
  permintaan user diberi nomor lanjutan di `todo.md`.
- Push hanya bila user meminta.
- Sebelum commit backend: `go vet ./...`, `go build ./cmd/...`, `go test ./... -race` (dengan DB, lihat
  bawah). Frontend (di `web/`): `npm run typecheck`, `npm test`, `npm run build`.
- Jangan melemahkan test (skip, hapus assertion, menurunkan ambang) agar hijau; perbaiki penyebabnya.

## Backend (Go 1.26, Gin, pgx, goose)

- Clean architecture: `domain` (aturan murni, tanpa I/O) ← `usecase` (alur + port di `ports.go`) ←
  `adapter/*` (implementasi port) dan `delivery/http` (handler tipis). Handler memetakan error lewat
  `httpx.WriteError`; error domain membawa `Kind` + `Code` stabil.
- Migrasi baru = file berikutnya di `migrations/` (`00NN_nama.sql`, goose Up/Down). `Down` tidak boleh
  diam-diam membuang data.
- Konfigurasi hanya dari `config.json` (secret di bagian `secrets`; env dengan nama yang sama menimpa).
  Field tak dikenal ditolak dan validasi fail-fast: perubahan config wajib memperbarui
  `config.json.example` (diuji `TestExampleConfigIsValid`).
- Test DB memakai `internal/testutil/pgtest` (schema acak per test). DB test ada di Pi lewat SSH tunnel:
  `scripts/db-tunnel.sh` (port 15432), lalu `set -a; . ./.env.local; set +a`. **Jangan** jalankan API/E2E
  lokal dengan `DATABASE_URL` dari `.env.local` (itu DB produksi); untuk E2E buat schema sementara di DB
  test (`?search_path=<schema>`) dan hapus setelahnya.
- Test E2E HTTP ada di `internal/delivery/http/e2e_*_test.go`. Alur baru yang menyentuh data sensitif
  harus ikut `e2e_loghygiene_test.go` (log tidak boleh memuat kode OTP, token, nomor utuh, password,
  alasan review/blokir).
- Aksi admin dicatat `msg="admin action"` dengan `action` (mis. `user.delete_account`) lewat
  `logAdminAction`.

## Frontend (`web/`, React 19 + Vite + TS strict)

- Lihat [web/README.md](web/README.md). Komponen tidak memanggil `fetch` langsung (lewat
  `src/api/endpoints.ts`, admin lewat `src/features/admin/api.ts`); logika murni di `src/lib/` + test.
- Dashboard admin adalah **lazy chunk**: kode `features/admin` tidak boleh di-import dari bundle user
  (dicek `scripts/check-bundle.mjs` saat build). Komponen bersama user + admin ada di `src/components/`.
- Ikon: Font Awesome Free, di-import **per berkas** (`@fortawesome/free-solid-svg-icons/faCamera`) lewat
  `components/Icon.tsx`. **Tanpa emoji** di teks UI (dicek test). Warna lewat token CSS tema; kontras AA
  dicek `src/styles/contrast.test.ts`. Skrip inline di `index.html` terikat hash CSP di
  `web/nginx/security-headers.conf` (test gagal bila tidak sinkron).
- Layout harus nyaman di HP 360 px (tanpa scroll horizontal, target sentuh ≥ 44 px) dan desktop.
- E2E browser: `web/e2e/flow.mjs` (Chrome headless + `playwright-core`) terhadap API lokal mode `otp` +
  `cmd/wanotif-stub` + Vite dev; perbarui bila teks/alur UI yang dipakainya berubah.

## Deploy ke Raspberry Pi

- Akses SSH: kredensial di `docs/raspi_auth.md` (gitignored). **Jangan pernah menampilkan, mencetak, atau
  meng-commit isinya**; baca ke env tanpa echo dan pakai `sshpass -e`.
- Folder deploy di Pi: `/home/myrpi/docker/photo-annotated` (`docker-compose.yml` + `config.json` mode 640,
  `chown myrpi:65532`). Build image arm64 di mesin dev (lihat README), `docker save | gzip | ssh … docker
  load`, tag image lama `prev-<commit>` untuk rollback, lalu `docker compose up -d`.
- Sebelum migrasi yang mengubah skema data produksi: backup `config.json` + `pg_dump` ke
  `~/backups/photo-annotated/` di Pi.
- Data pribadi (nomor HP superadmin, secret) hanya di `config.json` di Pi, tidak pernah di repo.
- **Jangan** restart/ubah `wa-notif-service`, kernel/cmdline Pi, atau mengirim beban uji ke WhatsApp asli
  (pakai stub). Hindari `pkill -f` dengan pola yang bisa cocok dengan shell sendiri; matikan proses lewat
  PID dari port (`ss -ltnp`).
