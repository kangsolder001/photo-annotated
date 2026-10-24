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
