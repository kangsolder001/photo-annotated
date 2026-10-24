# Catatan Frontend: Label Terkontrol & Dashboard Superadmin

Sumber: [spec-labels-admin.md](../spec-labels-admin.md) (Draft v0.4). Dikerjakan di branch
`feat/labels-frontend`, paralel dengan backend (T37–T40, T44, T46–T48) yang belum tersedia saat
FE ditulis: klien API dibangun dari bentuk JSON spec §5 dan diuji dengan `fetch` palsu.
Satu commit per task. Catatan ini menggantikan entri `plan.md`/`todo.md` untuk task FE agar tidak
bentrok dengan agent backend; digabung ke `plan.md` setelah merge.

#### Task 53: Ikon Font Awesome + test pemindai emoji — **SELESAI**
**Hasil:** Font Awesome Free 6.7.2 (`@fortawesome/fontawesome-svg-core`, `free-solid-svg-icons`,
`free-regular-svg-icons`) + `@fortawesome/react-fontawesome` 3.5.0, versi dikunci persis. Ikon
di-import satu per satu dan dirender sebagai SVG inline. Komponen `components/Icon.tsx`: `Icon`
(dekoratif, selalu `aria-hidden`, 1em, `currentColor`) dan `IconButton` (hanya-ikon, `label` wajib →
`aria-label` + tooltip `title`, ikon 20 px, area sentuh 44 × 44 px). Lima emoji yang ada diganti:
logo header (`camera`), Ambil foto / Pilih dari galeri (`camera` / `images`), mode gambar / gulir
(`vector-square` / `hand`).
**Keputusan:**
- CSS Font Awesome di-import di `main.tsx` dan `config.autoAddCss = false`: CSS ikut bundel, tidak
  disisipkan sebagai `<style>` saat runtime.
- `react-fontawesome` 3.x (peer `fontawesome-svg-core ~6 || ~7`, React 18/19) dipilih alih-alih 0.2.x
  (masih bergantung `prop-types`).
**Test:** `src/conventions.test.ts` (lingkungan node) memindai semua `.ts/.tsx/.css` di `src` dan
`index.html`: tidak ada karakter `Extended_Pictographic`, regional indicator, atau VS16 di luar
komentar (komentar blok/baris/HTML dibuang dulu); berkas yang dikirim ke browser hanya boleh
memuat URL Turnstile (`challenges.cloudflare.com`) dan namespace `www.w3.org`, tanpa `@font-face`,
`@import url`, atau CDN ikon/font. Test lama yang memakai emoji literal diganti ke escape `\u{1F600}`.
**Verification:** [x] `npm run typecheck`; [x] `npm test` (173); [x] `npm run build` — URL di bundel
hanya namespace SVG/XML, tautan dokumentasi pesan error React/Router, dan skrip Turnstile.

#### Task 52: Tema terang hangat / gelap, anti-kedip, test kontras — **SELESAI**
**Hasil:** token di `styles/global.css`: `:root` = terang hangat (nilai spec §7.4), `:root[data-theme="dark"]`
= tema gelap lama (nilai dipertahankan). Token baru `--border-strong` (tepi isian, `select`, tombol)
dan `--focus` (cincin fokus; sebelumnya memakai `--box-selected` yang di latar krem < 3:1).
`lib/theme.ts`: pilihan `system` / `light` / `dark` di `localStorage["pa_theme"]` (storage diblokir →
tetap jalan dengan Sistem), `theme.init()` saat start mengikuti perubahan setelan perangkat
(`matchMedia`), `applyTheme` memasang `data-theme` + `<meta name="theme-color">` (= `--surface`).
`components/ThemeSwitcher.tsx`: varian `menu` (satu tombol ikon `circle-half-stroke`/`sun`/`moon` di
header user, membuka `menuitemradio`, tutup dengan Escape/klik di luar) dan `inline` (radio group,
untuk sidebar dashboard). Kotak anotasi: garis warna + halo gelap tipis (`rgb(0 0 0 / 65%)`) yang
sama di kedua tema; kotak terpilih dibedakan dengan garis 1,7× lebih tebal dan pegangan berisi warna
kotak, bukan hanya warna. Foto tidak diberi filter.
**Keputusan:**
- **Skrip anti-kedip inline + hash CSP.** CSP produksi `script-src 'self'` memblokir skrip inline,
  jadi hash `sha256-…` skrip itu ditambahkan ke `nginx/security-headers.conf` dan dihitung otomatis
  di `vite.config.ts` untuk `_headers` (Cloudflare Pages). Test memastikan hash di nginx sama dengan
  isi skrip di `index.html`; mengubah skrip tanpa memperbarui hash → test merah. Dicek: `vite build`
  menyalin skrip apa adanya (hash `dist/index.html` sama).
- Tanpa fallback CSS `prefers-color-scheme` untuk browser tanpa JS (SPA tidak tampil tanpa JS),
  sehingga nilai token gelap hanya ditulis sekali.
- `--ok` terang diturunkan dari `#3f7a2c` (spec) ke `#377026`: `#3f7a2c` di atas `--primary-soft`
  (latar banner sukses) hanya 4.29:1 dan di atas `--bg` 4.45:1; kini ≥ 4.77:1.
- `lib/color.ts`: `contrast()` WCAG dan `textOn(warna)` (teks hitam/putih yang terbaca di atas warna
  label, dipakai nomor kotak di T41).
**Test:** `styles/contrast.test.ts` membaca token dari `global.css` dan mengecek 23 pasangan per tema
(teks/`--muted`/`--primary`/`--danger`/`--warn`/`--ok` di atas `--bg`/`--surface`/latar lembut ≥ 4.5:1;
`--border-strong` dan `--focus` ≥ 3:1), nilai gelap = tema lama, `theme-color` = `--surface`.
`lib/theme.test.ts` menjalankan skrip inline dari `index.html` di jsdom (6 kombinasi storage ×
setelan perangkat, storage melempar error) + hash CSP; `ThemeSwitcher.test.tsx`.
**Verification:** [x] typecheck; [x] `npm test` (248); [x] build (`_headers` memuat hash yang sama).
Cek visual di browser: lihat T41 (layar anotasi kedua tema).
