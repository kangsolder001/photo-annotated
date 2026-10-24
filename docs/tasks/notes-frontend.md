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

#### Task 41: FE user — pemilih label, warna kotak, migrasi draf, `LABEL_ARCHIVED` — **SELESAI**
**Hasil:** isian catatan per kotak diganti dropdown `<select>` native: opsi pertama "Pilih label…"
(kosong, tidak bisa dikirim), lalu label aktif dari `GET /labels` (urutan server). Titik warna label
terpilih di samping dropdown, `description` tampil di bawahnya sebagai petunjuk. Kotak di foto
memakai warna label (isi transparan + halo dari T52), nomor kotak dengan teks hitam/putih yang
terbaca di atas warna itu (`textOn`); kotak tanpa label abu-abu (`#8a8f98`) putus-putus. Kirim/Simpan
nonaktif sampai daftar label termuat dan tidak kosong; kotak tanpa label menahan kirim ("Pilih
label."). Daftar kosong → "Belum ada label. Hubungi admin."; gagal muat → pesan + "Coba lagi".
Tampilan baca: chip nama + warna label; baris lama (`label: null`) menampilkan teksnya dengan tanda
"catatan lama" (teks dirender sebagai teks).
**Kontrak (spec §5.1):** kirim `{ label_id, x, y, w, h }` (tanpa `text`); baca `{ label: { id, name,
color, archived } | null, text, x, y, w, h }`; `GET /labels` → `{ labels: [{ id, name, description?,
color }] }`. Tipe di `api/types.ts` (`Label`, `LabelRef`, `AnnotationDTO`, `AnnotationView`).
**Edit & label arsip (§4.2):** pilihan label per foto = label aktif + label arsip yang sudah dipakai
kotak di versi tersimpan foto itu (opsi "Nama (diarsipkan)", `lib/labels.optionsForPhoto`), sama
dengan pengecualian backend; foto baru hanya label aktif. Setelah daftar label termuat, label kotak
yang tidak ada di pilihan foto itu dikosongkan dan kotaknya ditandai ("Label sebelumnya sudah tidak
tersedia. Pilih label lain.", `clearUnavailable`).
**Draf:** menyimpan `label_id` (boleh `null`) + `legacy_text`. Draf lama `{ text, … }` dimigrasi saat
dibaca (`migrateAnnotation`): kotak dipertahankan, label kosong, teks ikut sebagai "catatan lama"
(petunjuk saat memilih label); kotak rusak dibuang. Submission lama (teks bebas) yang masih bisa
diedit: teks lama tampil sebagai petunjuk, user wajib memilih label untuk tiap kotak sebelum simpan.
**`422 INVALID_LABEL` / `LABEL_ARCHIVED`:** pesan "Ada label yang sudah … Pilih ulang label pada kotak
yang ditandai.", `GET /labels` dimuat ulang, kotak yang labelnya hilang dikosongkan + ditandai.
**Keputusan:** validasi teks bebas dipindah ke `lib/text.ts` (`textProblem`, aturan sama dengan backend:
trim, panjang per karakter, tanpa kontrol/bidi) untuk nama/deskripsi label dan alasan review/blokir;
test validasi teks lama ikut pindah ke `text.test.ts` dengan batas nama label 50. `limits.maxTextLength`
diganti `maxLabelName` (50), `maxLabelDescription` (200), `maxReason` (500). Alat test `src/test/fakeApi.ts`
(fetch palsu berbasis rute) untuk test komponen.
**Test:** `PhotoAnnotator.test.tsx` (kotak baru abu-abu putus-putus + fokus ke dropdown, pilih label →
warna kotak + deskripsi, tanpa isian teks, kotak ditandai, label arsip "(diarsipkan)", tampilan baca
+ catatan lama), `annotation.test.ts` (toDTO tanpa teks, fromView baris lama, draf, clearUnavailable),
`draft.test.ts` (migrasi draf lama, sameAsSaved berlabel), `readiness.test.ts`, `NewSubmissionPage.test.tsx`
(memuat label, kosong, gagal).
**Verification:** [x] typecheck; [x] `npm test` (278); [x] build. [x] Visual (Chrome headless, `vite
preview` mode none, API di-mock lewat route interception): unggah 2 foto, 3 kotak (2 berlabel, 1
kosong) di 360/768/1440 px × terang/gelap — tanpa scroll horizontal, kotak terlihat jelas di kedua
tema, dropdown dan deskripsi terbaca.

#### Task 42: FE admin — login OTP, layout lazy responsif, halaman label — **SELESAI**
**Hasil:**
- **Lazy chunk:** `App.tsx` memuat `features/admin/AdminApp` lewat `React.lazy` di rute `/admin/*`
  (di luar layout user). `npm run build` kini menjalankan `scripts/check-bundle.mjs` setelah `vite
  build`: build gagal bila chunk entry yang dimuat `index.html` memuat penanda kode admin
  (`/api/v1/admin`, `pa.admin.session`, teks/kelas khusus admin) atau chunk `AdminApp-*.js` tidak ada.
  Hasil: bundle user `index-*.js` 376,7 kB (gzip ±124 kB), chunk admin `AdminApp-*.js` 25,7 kB +
  CSS 7,5 kB (CSS admin ikut lazy).
- **Token admin** di `sessionStorage["pa.admin.session"]` (`features/admin/session.ts`), terpisah dari
  token user (`localStorage`). `api/client.ts` menerima `token`/`onUnauthorized` sehingga request admin
  tidak pernah membawa token user dan 401 hanya membuang token admin → `/admin/masuk?next=<rute>`
  (Keluar manual → `/admin/masuk` tanpa `next`).
- **Login `/admin/masuk`:** nomor HP + Turnstile → `POST /admin/otp/request` → layar kode (status
  dipantau `GET /admin/otp/{id}` tiap 2 detik, hitung mundur kirim ulang, `POST /admin/otp/resend`,
  banner kanal dari `GET /admin/channel-status`) → `POST /admin/otp/verify` → token. Bagian OTP user
  dipakai ulang setelah diekstrak dari `OtpPage`: `useOtpStatus` (polling; kirim ulang dengan
  `challenge_id` sama memulai pantauan baru), `useNow`, `resendCooldown`, `OtpCodeInput`, `describeStatus`,
  `ChannelBanner` (kini menerima fungsi `check`). Challenge berjalan di `sessionStorage["pa.admin.otp"]`.
  Pesan netral "Kode hanya dikirim bila nomor ini adalah nomor superadmin…" (anti-enumerasi).
- **Layout (`AdminLayout`, `admin.css`):** ≥ 1024 px sidebar tetap (Beranda, Anotasi, Label,
  Annotator, Statistik + pilihan tema inline + nama admin + Keluar); 768–1023 px sidebar default ciut
  jadi ikon (tombol lebarkan/ciutkan, label nav jadi `aria-label`/tooltip); < 768 px bar atas (tombol
  menu `bars` + judul halaman) dan drawer (tutup dengan Escape/latar/tombol). Menu Annotator hanya
  bila `GET /admin/me` → `auth_mode: "otp"`. Skip link, `usePageTitle` (judul bar atas + tab).
- **UI bersama admin (`features/admin/ui`):** `Dialog` (modal tengah; sheet layar penuh dari bawah di
  HP; Escape, jebakan fokus, fokus dikembalikan), `ToastProvider` (notifikasi hasil, ikon + teks),
  `RowMenu` (menu `ellipsis-vertical`), `useMediaQuery`/`useIsPhone`.
- **`/admin/label`:** desktop/tablet tabel (warna + kode, nama, deskripsi — disembunyikan < 1024 px,
  jumlah pemakaian, status, aksi); HP daftar kartu dengan menu titik tiga. Tambah (nama, deskripsi,
  warna otomatis dari palet server atau pilih sendiri `<input type=color>` + kode hex), ubah inline,
  naik/turun urutan (`PUT /admin/labels/order` berisi semua id label aktif; tampilan langsung
  diperbarui), arsip/buka arsip (`PUT` replace lengkap), hapus hanya bila pemakaian 0 lewat dialog
  konfirmasi (selain itu hanya "Arsipkan"). Peringatan "Label ini dipakai di N kotak…" saat mengganti
  nama label terpakai. Validasi klien = backend (nama 1–50, deskripsi ≤ 200, warna `#rrggbb` huruf
  kecil). Error `LABEL_NAME_TAKEN`, `LABEL_LIMIT`, `LABEL_IN_USE` (muat ulang), `LABEL_ORDER_STALE`
  (muat ulang + minta ulangi) diberi pesan sendiri (`features/admin/errors.ts`).
- Klien API admin lengkap untuk semua task berikutnya sudah ditulis di sini (`features/admin/api.ts`,
  `types.ts`, persis bentuk JSON spec §5).
**Keputusan / asumsi kontrak:**
- `POST /admin/labels` tanpa `color` bila "otomatis" dicentang (server memilih dari palet).
- `PUT /admin/labels/order` dianggap membalas 2xx tanpa isi yang dipakai FE.
- `GET /admin/labels` diurutkan ulang di FE (aktif per `sort_order` lalu nama, arsip di bawah) agar
  tidak bergantung urutan server.
- Login admin memakai `body.turnstile_token` (seperti `/auth/register`), bukan header.
**Test:** `admin.test.tsx` (sesi di sessionStorage terpisah, lazy route tanpa token → `/admin/masuk`,
token admin dipakai & token user tidak, 401 → keluar ke `/admin/masuk?next=`, menu Annotator per
`auth_mode`, Keluar; alur login: request → status terkirim → kode salah "Sisa 4 percobaan" → benar →
token + redirect `next`; kanal mati + `WA_UNAVAILABLE` → kirim ulang langsung aktif, Ganti nomor),
`LabelsPage.test.tsx` (urutan, kolom, tombol hapus vs arsip, tambah otomatis/warna sendiri, validasi,
`LABEL_NAME_TAKEN`, peringatan ganti nama + body PUT, reorder + `LABEL_ORDER_STALE`, arsip/buka arsip,
hapus + `LABEL_IN_USE`, tampilan kartu HP + menu).
**Verification:** [x] typecheck; [x] `npm test` (335); [x] build + cek bundle. [x] Visual 360/768/1440 ×
terang/gelap (`/admin/label`, `/admin/masuk`): tanpa scroll horizontal; ditemukan & diperbaiki: `main`
global (maks 760 px) ikut membatasi dashboard, pilihan tema terpotong di sidebar 240 px.

#### Task 43: FE admin — daftar & detail anotasi, export — **SELESAI**
**Hasil:**
- **`/admin/anotasi`:** kartu per submission: foto sampul + overlay kotak berwarna label (komponen
  render yang sama dengan tampilan baca user: `BoxEditor` mode `display` — tanpa interaksi/tab stop,
  `loading="lazy"`, rasio asli dipertahankan sehingga overlay pas juga untuk foto potret), nama +
  nomor HP utuh annotator (Anonim dengan ikon `user-secret`; lencana Diblokir `user-slash`), waktu,
  jumlah foto/kotak, chip label + jumlah, lencana status review, "N catatan lama", "terhapus dalam N
  hari", alasan penolakan. Grid 3–4 kolom desktop, 2 kolom tablet, 1 kolom HP. "Muat lagi" (cursor).
- **Filter di URL** (`filters.ts`): tanggal dari/sampai (tanggal lokal inklusif → `from` = awal hari,
  `to` = awal hari berikutnya, RFC 3339), label (termasuk arsip), status review, cari annotator (nama
  / nomor, dikirim saat tekan cari), "Masih ada catatan lama" (`unlabeled=true`), `user_id` (dari
  tautan "Semua dari annotator ini"), `sort`. Pintasan **Antrean review** = `review_status=pending&
  sort=oldest`. Di HP filter di panel "Filter (n)" yang bisa dibuka-tutup; filter aktif selalu tampil
  sebagai chip yang bisa dihapus + "Hapus semua filter". Ukuran halaman 24 (habis dibagi 2/3/4 kolom).
- **`/admin/anotasi/:id`:** semua foto dengan overlay bernomor + daftar kotak (label, tanda
  "diarsipkan", ukuran px; baris lama: "catatan lama" + teks apa adanya), kartu annotator (nama,
  email `mailto:`, nomor `tel:`, "Semua dari annotator ini", Profil bila mode otp), waktu dibuat/diubah,
  versi, status review, kapan terhapus retensi. Desktop: foto kiri, panel kanan (sticky); < 1024 px
  bertumpuk. URL foto kedaluwarsa → detail dimuat ulang (maks 1×/10 detik). 404 → "tidak ditemukan atau
  sudah dihapus". Urutan daftar terakhir disimpan per tab (`listContext`) untuk navigasi J/K (T49).
- **Export JSONL:** dialog (rentang tanggal dari filter aktif, status review default **Diterima**,
  opsi "hanya foto berlabel X" bila filter label aktif) → `fetch` dengan `Authorization` →
  `photo-annotated-<from>-<to>.jsonl` ("awal"/"sekarang" bila kosong) → notifikasi jumlah baris.
**Keputusan:** trailer `X-Export-Complete` tidak bisa dibaca `fetch`; FE memakai header itu bila ada
(proxy bisa mengubah trailer jadi header) dan selain itu memeriksa badan: harus diakhiri baris baru
dan baris terakhir JSON utuh; bila tidak → "Export tidak lengkap". Export dibaca penuh ke memori
(`res.text()`) lalu disimpan sebagai Blob: cukup untuk ukuran dataset sekarang (satu baris per foto,
tanpa URL), dicatat sebagai batas.
**Asumsi kontrak:** `cover.annotations[].label_id` boleh `null` (baris lama); `next_cursor` kosong/
`null` = habis; detail admin memuat `photos[].url` dan `annotations[].label` seperti §5.1.
**Test:** `SubmissionsPage.test.tsx` (kartu, Anonim, overlay, tautan detail, filter → query API +
URL, chip, cursor, antrean review, `INVALID_QUERY`, export: header Authorization + default
`accepted` + nama berkas, export terpotong, `checkJSONL`; detail: foto/kotak/ukuran/arsip/catatan lama
sebagai teks/annotator/tautan, 404), `filters.test.ts`.
**Verification:** [x] typecheck; [x] `npm test` (363); [x] build (chunk admin 44 kB). [x] Visual
360/768/1440 × kedua tema; ditemukan & diperbaiki: overlay sampul bergeser pada foto potret
(`object-fit: contain` membuat SVG lebih besar dari gambar) → wadah mengikuti ukuran gambar.

#### Task 45: FE hapus permanen + pesan "sudah dihapus" di sisi user — **SELESAI**
**Hasil:**
- `ui/ConfirmDelete.tsx`: dialog yang menyebut apa yang akan hilang ("2 foto, 3 kotak anotasi milik
  Budi Santoso"), banner "permanen dan tidak bisa dibatalkan … database dan SSD", tombol bahaya
  (latar `--danger`) baru aktif setelah mengetik persis `HAPUS` (Enter juga mengirim). Di HP dialog
  menjadi sheet layar penuh dari bawah.
- Detail: tombol **Hapus submission** (`DELETE /admin/submissions/{id}` → notifikasi → kembali ke
  daftar dengan filter terakhir) dan **Hapus foto** per foto (`DELETE /admin/photos/{id}`; foto terakhir
  diberi tahu bahwa submission ikut terhapus; `submission_deleted: true` → kembali ke daftar, selain itu
  detail dimuat ulang karena versi naik).
- Daftar: kotak centang per kartu (desktop/tablet selalu tampil; HP lewat tombol **Pilih**), bar aksi
  (menempel di atas; di HP menempel di bawah) dengan "Pilih semua yang tampil", jumlah dipilih (maks
  50), **Hapus terpilih** (`POST /admin/submissions/delete`) → item `deleted`/`cleanup_pending`/
  `not_found` dibuang dari daftar, ringkasan hasil di notifikasi.
- `cleanup_pending` → "… dihapus; sebagian file dibersihkan otomatis." (notifikasi waspada).
- **User:** `GET /submissions/{id}` → 404 untuk laporan yang tercatat di riwayat perangkat →
  "Laporan ini sudah dihapus…", entri riwayat (`history.remove`) dan drafnya dibuang. Simpan edit
  yang mendapat 404 → "Laporan ini sudah dihapus." + Muat ulang (lalu jalur yang sama).
**Keputusan:** teks user memakai "Laporan" (istilah yang dipakai di seluruh layar user) alih-alih
"Submission" dari spec §7.1. Mode "Pilih" di HP lewat tombol saja; tekan lama belum dibuat.
**Test:** `delete.test.tsx` (teks dialog, tombol nonaktif sampai `HAPUS` persis, DELETE + redirect;
hapus foto `cleanup_pending` + muat ulang; hapus massal: body ids, ringkasan, item hilang),
`SubmissionPage.test.tsx` (404 di riwayat → sudah dihapus + riwayat/draf dibuang; 404 lain → pesan akses).
**Verification:** [x] typecheck; [x] `npm test` (370); [x] build. [x] Visual: mode Pilih + bar bawah dan
sheet konfirmasi di 360 px, bar atas + modal di 1440 px.

#### Task 49: FE review — panel + antrean + aksi massal; lencana review user; akun diblokir — **SELESAI**
**Hasil (admin):**
- `ReviewPanel` di detail: lencana status + waktu review + alasan, **Terima** / **Tolak** (dialog
  alasan wajib 1–500 karakter, aturan teks backend) / **Batalkan review** (→ `pending`), pintas
  **Terima lalu berikutnya** / **Tolak lalu berikutnya** dan Sebelumnya/Berikutnya mengikuti urutan
  daftar terakhir (antrean review = `pending` + terlama dulu). `PUT /admin/submissions/{id}/review`
  selalu membawa `version` yang sedang dilihat; `409 VERSION_CONFLICT` → "baru saja diubah annotator…"
  + **Muat ulang** (admin tidak pernah menerima isi yang belum dilihat).
- Pintasan keyboard: `A` terima, `R` tolak (membuka isian alasan), `J`/`K` berikutnya/sebelumnya;
  tidak aktif saat fokus di isian teks/select atau saat dialog terbuka; tertulis di tooltip tombol dan
  petunjuk di panel (desktop). Di HP tombol Terima/Tolak menempel di bawah layar.
- Membatalkan penerimaan submission yang dibuat > 90 hari lalu → dialog peringatan "terhapus
  permanen pada putaran cleanup berikutnya (paling lama 1 jam)" sebelum dikirim.
- Daftar: **Terima terpilih** / **Tolak terpilih** (satu alasan untuk semua) di bar aksi massal →
  `POST /admin/submissions/review` dengan `{ id, version }` per item; hasil per item: `ok` → status
  kartu diperbarui (atau keluar dari daftar bila tidak cocok lagi dengan filter status, mis. antrean),
  `version_conflict` dan `not_found` dilaporkan di notifikasi.
**Hasil (user):** `GET /submissions/{id}` → `review` ditampilkan sebagai lencana (Menunggu review /
Diterima / Ditolak, ikon + teks) + alasan penolakan; ditolak dan masih bisa diedit → tombol
**Perbaiki** (menggantikan Edit) + penjelasan bahwa perbaikan kembali menunggu review. Riwayat
memperbarui status review 20 laporan terbaru (4 permintaan paralel) dan menyimpannya di entri riwayat,
menampilkan lencana + alasan; laporan yang 404 (dihapus admin) dibuang dari riwayat. Login
`403 ACCOUNT_BLOCKED` → "Akun Anda diblokir. Hubungi admin." Gaya `.badge` dipindah ke CSS global.
**Keputusan:** batas 90 hari untuk peringatan diambil dari default `retention.days` (FE tidak tahu
nilai config server; `retention_deletes_at` bernilai `null` saat diterima). Riwayat hanya
memperbarui 20 laporan terbaru agar membuka riwayat tidak membuat 100 request.
**Asumsi kontrak:** `GET /submissions/{id}` untuk user memuat `review: { status, reason }`; review
massal membalas 200 dengan `results[].result`.
**Test:** `review.test.tsx` (terima + versi, tolak + validasi alasan + body, `VERSION_CONFLICT` + muat
ulang, pintasan A/J/K dan tidak aktif saat mengetik, terima lalu berikutnya, peringatan > 90 hari,
review massal + hasil per item), `HistoryPage.test.tsx`, `LoginPage.test.tsx` (`ACCOUNT_BLOCKED`).
**Verification:** [x] typecheck; [x] `npm test` (387); [x] build. [x] Visual 360 px (tombol review
menempel di bawah, bar aksi massal 4 tombol) dan 1440 px.

#### Task 50: FE admin — halaman annotator — **SELESAI**
**Hasil:**
- **`/admin/annotator`** (menu hanya pada `auth_mode: "otp"`; pada mode `none` halaman memberi tahu
  tidak ada akun annotator dan **tidak** memanggil `/admin/users`, yang 404 di mode itu): tabel nama
  (tautan profil), nomor HP utuh, email, submission / kotak / diterima / ditolak, aktivitas terakhir
  (submission terakhir, atau login terakhir), status (Aktif / Diblokir `user-slash`). Tablet
  menyembunyikan email, kotak, aktivitas; HP daftar kartu. Cari nama/nomor (`q`) dan filter
  `status` di URL; "Muat lagi" (cursor).
- **`/admin/annotator/:id`:** profil (nama, email, nomor, terdaftar, login/submission terakhir,
  status blokir + waktu + alasan), kotak statistik, "Lihat submission-nya" (`/admin/anotasi?user_id=`).
  **Blokir** (alasan wajib, dialog menjelaskan efeknya: sesi dicabut, login/daftar ulang/lupa password
  ditolak) → `POST …/block`; **Buka blokir** (konfirmasi) → `POST …/unblock`; **Hapus data annotator**
  → `ConfirmDelete` (ketik `HAPUS`) dengan kotak centang "Ikut hapus submission yang sudah diterima"
  (default tidak; teks konfirmasi menyebut jumlah yang dihapus vs dipertahankan) →
  `POST …/delete-data { include_accepted }` → notifikasi jumlah submission/foto/kotak, file
  dibersihkan otomatis.
**Keputusan:** daftar annotator menunggu `GET /admin/me` (mode) sebelum meminta data.
**Test:** `annotators.test.tsx` (kolom + status, cari & filter → query, mode none tanpa request, blokir
dengan alasan wajib → body, buka blokir, hapus data default/ikut diterima + `HAPUS` + body).
**Verification:** [x] typecheck; [x] `npm test` (396); [x] build. [x] Visual 360/768/1440 × kedua tema.

#### Task 51: FE admin — beranda status sistem + statistik label — **SELESAI**
**Hasil:**
- **`/admin` (Beranda):** `GET /admin/system` dimuat saat dibuka, tiap 30 detik selama tab terlihat,
  saat tab kembali terlihat, dan lewat "Muat ulang". Ringkasan pekerjaan: "N menunggu review" dan "N
  akan terhapus retensi dalam 7 hari" (tautan ke antrean review; yang kedua berlatar waspada bila > 0),
  submission / diterima / ditolak, annotator aktif + diblokir (disembunyikan pada mode none). Kartu
  status Database (`database`), SSD (`hard-drive`, bar pemakaian `role=meter`), Upload aktif
  (`cloud-arrow-up`), WhatsApp/OTP (`message`), Cleanup (`broom`), Suhu CPU (`temperature-half`);
  tiap kartu punya tingkat OK / Waspada / Bahaya sebagai ikon + teks + garis tepi berwarna token tema.
  Baris bawah: waktu cek, versi API, waktu start, mode auth.
- **Ambang (`systemStatus.ts`, sama dengan alert Grafana):** SSD terpakai > 80% → waspada; sisa <
  `min_free_bytes` atau `available: false` → bahaya; kanal WhatsApp tidak tersedia → bahaya; antrean >
  80% `max_queue` atau terkirim 24 jam > 80% `daily_cap` → waspada; suhu > 75 °C → waspada; DB `ok:
  false` → bahaya; upload > 80% slot → waspada; cleanup terakhir gagal → waspada. Bagian yang berisi
  `{ error }` → kartu "Tidak bisa dihitung" (bahaya; suhu: waspada) tanpa menjatuhkan kartu lain;
  `cpu_temp_c: null` → "Tidak terbaca dari container" (OK).
- **`/admin/statistik`:** filter tanggal + status review (default **Diterima (dataset)**, bisa "Semua"),
  disimpan di URL. Kotak total (kotak, foto, submission, catatan lama). Tabel batang horizontal per
  label (warna label, urut kotak terbanyak, lebar relatif terhadap label terbanyak, ujung membulat 4 px)
  dengan angka kotak / porsi / foto / submission berdampingan di desktop; di HP angka tampil di bawah
  tiap batang. Tooltip per baris (`title`). Label dengan 0 contoh (termasuk arsip) dipisah di bagian
  "Belum ada contoh" di bawah.
- **Ikon per berkas:** ditemukan saat build bahwa `import { faX } from "@fortawesome/free-solid-svg-icons"`
  membuat Rollup menaruh **semua** ikon yang dipakai (termasuk ikon dashboard) di chunk entry, karena
  modul ikon dipakai bersama. Semua impor ikon diubah ke per berkas
  (`@fortawesome/free-solid-svg-icons/faBroom`, didukung `exports "./*"` paket), dijaga test di
  `conventions.test.ts`, dan `check-bundle.mjs` kini juga memeriksa nama ikon khusus dashboard. Bundle
  user turun ke 374,4 kB (gzip ±123 kB); chunk admin 104 kB (gzip ±30 kB; ikon per berkas berbentuk CJS
  sehingga sedikit lebih besar).
**Test:** `home.test.tsx` (ambang tiap kartu, bagian error tidak menjatuhkan yang lain, kartu ikon +
teks status, meter SSD, tautan antrean, muat ulang 30 detik dengan timer palsu; statistik: default
`accepted`, urutan, format angka/persen id-ID, label tanpa contoh di bawah, filter → query).
**Verification:** [x] typecheck; [x] `npm test` (495); [x] build + cek bundle. [x] Visual 360/768/1440 ×
kedua tema (Beranda, Statistik): tanpa scroll horizontal; diperbaiki: sel nama label `display:flex`
merusak garis baris tabel.

#### E2E browser (`web/e2e/flow.mjs`) — **diperbarui, BELUM DIJALANKAN**
Diperbarui ke kontrak label dan diperluas dengan alur admin (commit terpisah setelah T51, karena
mencakup T41–T51). Belum bisa dijalankan: backend T37–T48 belum ada di branch ini.
Alur: admin masuk OTP (`ADMIN_PHONE` / `ADMIN_CANON`, default `0812-0000-0001` / `6281200000001`;
harus sama dengan `admin.superadmin.phone` di config API uji) → buat 3 label (nama unik per run) →
user daftar → kode salah/benar → 2 foto → kotak + **pilih label** (deskripsi tampil; tidak ada isian
teks) → kotak tanpa label menahan kirim ("Pilih label.") → kirim → nama label + "Menunggu review" →
edit label, reload, draf pulih → simpan → admin: antrean review menampilkan annotator + nomor utuh,
detail (foto, label, email) → tolak dengan alasan → user: "Ditolak" + alasan + "Perbaiki" → riwayat
dengan lencana → logout/login/lupa password (seperti sebelumnya) → admin hapus submission (dialog
menyebut "2 foto, 3 kotak anotasi milik Budi Santoso", ketik `HAPUS`) → user membuka laporan →
"Laporan ini sudah dihapus" → admin hapus label tak terpakai, arsipkan label lain → Keluar.
Yang perlu dicek saat dijalankan: teks/peran tombol yang diasumsikan di atas, urutan kotak setelah
reload draf, dan bahwa `GET /submissions/{id}` user memuat `review`.
