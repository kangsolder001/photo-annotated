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
    submit/     laporan baru: pilih foto, upload, kirim
    annotate/   editor kotak (SVG + Pointer Events) dan daftar catatan
    submission/ lihat & edit laporan, draf
    history/    riwayat laporan di perangkat ini
  components/   UI bersama (layout, banner, progres, Turnstile)
  styles/       CSS global (variabel warna, mode gelap, mobile-first)
```

Aturan: komponen tidak memanggil `fetch` langsung (lewat `api/endpoints.ts`); logika yang bisa
diuji tanpa DOM diletakkan di `lib/`.

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
npm run build      # dist/ + _headers (CSP) + _redirects (SPA)
npm run e2e -- <folder>   # uji end-to-end di Chrome, lihat komentar di e2e/flow.mjs
```

## Deploy ke Cloudflare Pages

- Root directory: `web`, build command: `npm run build`, output: `dist`.
- Environment variables (Production): `NODE_VERSION=24`, `VITE_API_BASE_URL`, `VITE_AUTH_MODE`,
  `VITE_TURNSTILE_SITE_KEY`.
- `dist/_headers` berisi CSP yang hanya mengizinkan origin API dan Turnstile; origin diambil dari
  `VITE_API_BASE_URL` saat build, jadi build ulang bila domain API berubah.
- Di API, tambahkan origin Pages ke `app.allowed_origins` dan isi `app.public_base_url` dengan URL API.
