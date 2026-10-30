# Todo: Pendataan Baseline Rumah Tangga Cipadung Kulon

Detail tiap task (file yang dibaca, pola yang ditiru, langkah, verifikasi) ada di
[plan-pendataan.md](plan-pendataan.md); **baca §0 "Panduan untuk agent" dulu**. Spesifikasi:
[spec-pendataan.md](../spec-pendataan.md). Todo aplikasi lama (T1–T56): [todo.md](todo.md).

Legenda: `[x]` selesai · `[ ]` belum · 🔒 butuh input/keputusan Anda · ⚠️ risiko tinggi · S/M = ukuran

## Phase 1: Backend
- [ ] **T57** (S) Retensi bisa dimatikan: `retention.days = 0`
- [ ] **T58** (M) Tabel `groups`, `GET /groups`, CRUD `/admin/groups`
- [ ] **T59** (M) ⚠️ Registrasi dengan NIM (unik) + kelompok; `/auth/me`; pindah kelompok oleh admin — butuh T58
- [ ] **T60** (M) `internal/domain/survey` (definisi v1 + validasi) + `GET /survey/form`
- [ ] **T61** (M) ⚠️ Submit/edit dengan `answers` (migrasi 0013, 0 / 1–3 foto, seed label) — butuh T60
- [ ] **T62** (S) Baca + admin daftar/detail pendataan — butuh T61
- [ ] **T63** (S) `GET /admin/export.csv` — butuh T61
- [ ] **T64** (S) `GET /admin/stats/survey` — butuh T61

### Checkpoint A: Backend lengkap
- [ ] `go vet`, `go build ./cmd/...`, `go test ./... -race` hijau
- [ ] Alur lengkap lewat `curl` di schema sementara

## Phase 2: Frontend
- [x] **T65** (S) Registrasi NIM + dropdown kelompok — butuh T59
- [x] **T66** (S) `src/lib/survey.ts` (logika murni + test) — butuh T60
- [x] **T67** (M) Wizard bagian A–R2 + draf — butuh T66
- [ ] **T68** (M) Langkah foto, ringkasan, kirim, riwayat; alur lama dihapus — butuh T61, T67
- [ ] **T69** (S) E2E browser `web/e2e/flow.mjs` — butuh T65, T68
- [ ] **T70** (S) Admin: kelompok + profil annotator — butuh T59
- [ ] **T71** (M) Admin: daftar/detail pendataan + tombol Export CSV — butuh T62, T63
- [ ] **T72** (S) Admin: statistik pendataan — butuh T64

### Checkpoint B: Aplikasi utuh (lokal)
- [ ] FE + BE hijau; E2E lulus
- [ ] Review dengan Anda

## Phase 3: Deploy
- [ ] **T73** (M) 🔒 Instance baru di Pi: DB `baseline_cipadung`, `/mnt/ssd/baseline_cipadung/photos`, port 8090/8091, `kknsampah.<domain>`

### Checkpoint C: Go-live
- [ ] Uji lapangan dari HP (seluler)
- [ ] Admin isi daftar kelompok sebenarnya
- [ ] Review dengan Anda
