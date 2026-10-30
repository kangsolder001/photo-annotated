# Photo Annotated — Frontend

React 19 + Vite 8 + TypeScript (strict). SPA statis untuk Cloudflare Pages; memanggil API Go lewat
Cloudflare Tunnel. Node 24 (npm 11; npm 10.9 gagal memasang vitest), lihat `.nvmrc`.

## Struktur

```
src/
  api/          klien HTTP (error API → ApiError), satu fungsi per endpoint, tipe kontrak API
  lib/          logika murni yang diuji: foto (resize/kompresi), upload ber-retry, anotasi
                (aturan = backend), draf, riwayat, sesi, pesan error, Turnstile
  features/     satu folder per fitur: halaman + hook + komponen khususnya
    auth/       masuk/daftar, halaman kode OTP, penjagaan rute (mode otp)
    survey/     pendataan (wizard per bagian dari GET /survey/form, langkah foto R3, ringkasan, kirim/edit)
    submit/     pilih foto + upload (dipakai langkah foto pendataan)
    annotate/   editor kotak (SVG + Pointer Events) dan daftar catatan
    submission/ lihat & edit laporan, draf
    history/    beranda: tombol "Pendataan baru" + riwayat di perangkat ini (+ status review)
    admin/      dashboard /admin (chunk lazy terpisah): login OTP, label, anotasi, review, hapus,
                annotator, statistik, status sistem; token admin di sessionStorage
  components/   UI bersama (layout, banner, progres, Turnstile)
  styles/       CSS global (token tema terang hangat/gelap, mobile-first; kontras dicek test)
```

Aturan: komponen tidak memanggil `fetch` langsung (lewat `api/endpoints.ts`); logika yang bisa
diuji tanpa DOM diletakkan di `lib/`. Ikon: Font Awesome Free lewat `components/Icon.tsx`, di-import
per berkas (`@fortawesome/free-solid-svg-icons/faCamera`) agar ikon dashboard tidak masuk bundle user;
tanpa emoji di teks UI (dicek `src/conventions.test.ts`).

## Konfigurasi (`.env.local`, lihat `.env.example`)

| Variabel | Arti |
|---|---|
| `VITE_API_BASE_URL` | URL API, mis. `https://api.contoh.com` (kosong = origin sama / proxy dev) |
| `VITE_AUTH_MODE` | `otp` atau `none`, harus sama dengan `auth.mode` API |
| `VITE_TURNSTILE_SITE_KEY` | site key Turnstile (kosong = tanpa Turnstile) |
| `VITE_DEV_API_TARGET` | dev saja: tujuan proxy `/api` (mis. SSH tunnel ke Pi `http://127.0.0.1:8080`) |

Semua `VITE_*` masuk ke bundle (publik). Jangan isi rahasia.

## Perintah

```
npx -y npm@11 install
npm run dev        # http://localhost:5173
npm test           # unit + komponen (vitest, jsdom)
npm run typecheck
npm run build      # dist/ + _headers (CSP) + _redirects (SPA); lalu scripts/check-bundle.mjs
                   # memastikan bundle user tidak memuat kode dashboard admin
npm run e2e -- <folder>   # uji end-to-end di Chrome, lihat komentar di e2e/flow.mjs
                          # (ADMIN_PHONE/ADMIN_CANON = nomor superadmin di config API)
```

## Deploy ke Cloudflare Pages

- Root directory: `web`, build command: `npm run build`, output: `dist`.
- Environment variables (Production): `NODE_VERSION=24`, `VITE_API_BASE_URL`, `VITE_AUTH_MODE`,
  `VITE_TURNSTILE_SITE_KEY`.
- `dist/_headers` berisi CSP yang hanya mengizinkan origin API dan Turnstile; origin diambil dari
  `VITE_API_BASE_URL` saat build, jadi build ulang bila domain API berubah.
- Di API, tambahkan origin Pages ke `app.allowed_origins` dan isi `app.public_base_url` dengan URL API.
