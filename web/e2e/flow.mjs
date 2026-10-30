// Uji end-to-end alur pendataan di Chrome (viewport HP 390×844, layar sentuh) memakai playwright-core.
//
// Prasyarat (lihat web/README.md): API lokal mode otp (auth.otp.resend_cooldown pendek, mis. "2s") dengan stub wa-notif di 127.0.0.1:13030
// (cmd/wanotif-stub -token …), Vite dev di http://localhost:5173 dengan VITE_AUTH_MODE=otp dan
// VITE_DEV_API_TARGET ke API, serta public_base_url API = http://localhost:5173. Database: schema sementara
// di DB test (DATABASE_URL=…&search_path=<schema>, `go run ./cmd/migrate up`), hapus setelah selesai.
// Admin: admin.enabled = true dan admin.superadmin.phone = ADMIN_PHONE (default 0812-0000-0001, sesuaikan
// config API; ADMIN_CANON = bentuk kanonik, default 6281200000001).
//   node e2e/flow.mjs <folder-kerja>     (folder berisi img/big.jpg; screenshot ke <folder>/shots)
// Variabel CHROME_PATH mengganti lokasi Chrome (default /usr/bin/google-chrome).
//
// Alur: admin masuk (OTP) → buat label + kelompok → mahasiswa daftar dengan NIM + kelompok → pendataan
// menolak (hanya bagian A) → pendataan bersedia (semua bagian, 1 foto + 1 kotak berlabel) → edit + reload
// (draf pulih) → admin melihat daftar + detail pendataan (teks opsi, NIM, kelompok) → tolak dengan alasan
// → mahasiswa melihat lencana "Ditolak" → keluar/masuk dengan password, lupa password → admin hapus
// pendataan (ketik HAPUS) → mahasiswa melihat "sudah dihapus" → admin arsipkan label + kelompok.
import { chromium } from "playwright-core";
import { argv } from "node:process";

const dir = argv[2];
const BASE = "http://localhost:5173";
// nomor acak per run: nomor yang sudah aktif tidak bisa didaftarkan ulang (tidak ada kode dikirim)
const DIGITS = String(Math.floor(1e7 + Math.random() * 9e7));
const PHONE = `0812${DIGITS}`; // seperti diketik
const CANON = `62812${DIGITS}`; // bentuk kanonik di server
const ADMIN_PHONE = process.env.ADMIN_PHONE ?? "0812-0000-0001";
const ADMIN_CANON = process.env.ADMIN_CANON ?? "6281200000001";
const RUN = DIGITS.slice(-4); // nama label/kelompok unik per run (unik tanpa beda huruf besar-kecil)
const LABEL = `Wadah organik ${RUN}`;
const GROUP = `Kelompok ${RUN} – CL-05`;
const NIM = `251fs${DIGITS}`; // diketik huruf kecil, disimpan huruf besar
const NIM_UPPER = NIM.toUpperCase();
const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH ?? "/usr/bin/google-chrome", headless: true });
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, hasTouch: true, isMobile: true, locale: "id-ID" });
const page = await ctx.newPage();
// dashboard di konteks terpisah (desktop): token admin di sessionStorage, tidak tercampur sesi user
const adminCtx = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: "id-ID" });
const admin = await adminCtx.newPage();
const logs = [];
for (const [p, who] of [[page, "user"], [admin, "admin"]]) {
  p.on("console", (m) => { if (m.type() === "error" || m.type() === "warning") logs.push(`${who} ${m.type()}: ${m.text()}`); });
  p.on("pageerror", (e) => logs.push(`${who} pageerror: ${e.message}`));
}
const shot = (n, p = page) => p.screenshot({ path: `${dir}/shots/${n}.png`, fullPage: true });
const step = (s) => console.log("OK", s);

async function stubCode(phone) {
  for (let i = 0; i < 20; i++) {
    const msgs = await (await fetch("http://127.0.0.1:13030/_stub/messages")).json();
    const m = [...msgs].reverse().find((x) => x.To === phone);
    if (m) return m.Text.match(/\*(\d{6})\*/)[1];
    await new Promise((r) => setTimeout(r, 300));
  }
  throw new Error("kode tidak terkirim");
}

// tanpa scroll horizontal di layar HP (spec: nyaman di 360–390 px)
async function noHorizontalScroll(where) {
  const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  if (over > 0) throw new Error(`scroll horizontal ${over}px di ${where}`);
}

// --- helper wizard pendataan ---
const heading = () => page.locator("#survey-step-title");
async function atSection(title) {
  await heading().filter({ hasText: title }).waitFor();
}
/** Pilih satu opsi pada pertanyaan choice/multi (fieldset berlegenda = role group). */
async function choose(question, option) {
  await page.getByRole("group", { name: question }).locator("label.choice", { hasText: option }).first().click();
}
/** Pilih opsi pertama di setiap pertanyaan pilihan pada bagian ini (butir B: "Ya", C: "Ada/Sesuai"). */
async function chooseFirstEverywhere() {
  const qs = page.locator("fieldset.question");
  const n = await qs.count();
  if (n === 0) throw new Error("tidak ada pertanyaan pilihan");
  for (let i = 0; i < n; i++) await qs.nth(i).locator("label.choice").first().click();
}
const next = () => page.getByRole("button", { name: "Lanjut" }).click();
async function fillIdentity({ rw, rt, code, initials }) {
  await atSection("A. Identitas Pendataan");
  // tanggal bawaan = hari ini (Asia/Jakarta); identitas mahasiswa dari sesi
  if (!(await page.getByLabel(/^Tanggal pendataan/).inputValue())) throw new Error("tanggal pendataan kosong");
  await page.getByText(NIM_UPPER).waitFor();
  await page.getByText(GROUP).waitFor();
  await page.getByLabel(/^RW/).fill(rw);
  await page.getByLabel(/^RT/).fill(rt);
  await page.getByLabel(/^Kode rumah tangga/).fill(code);
  await page.getByLabel(/^Inisial responden/).fill(initials);
  await choose(/^Status responden/, "Kepala keluarga");
}
async function toSummary() {
  for (let i = 0; i < 15; i++) {
    if (await page.getByRole("heading", { name: "Ringkasan" }).count()) return;
    await next();
  }
  throw new Error("ringkasan tidak tercapai");
}

// 0. admin: masuk dengan OTP WhatsApp, buat label + kelompok
await admin.goto(BASE + "/admin/label");
await admin.waitForURL(/\/admin\/masuk\?next=%2Fadmin%2Flabel/);
await admin.getByLabel(/Nomor WhatsApp superadmin/).fill(ADMIN_PHONE);
await admin.getByRole("button", { name: /Kirim kode WhatsApp/ }).click();
await admin.getByText("Kode sudah dikirim ke WhatsApp Anda.").waitFor();
await admin.getByLabel(/Kode 6 digit/).fill(await stubCode(ADMIN_CANON));
await admin.waitForURL(BASE + "/admin/label");
step("admin masuk dengan OTP → /admin/label");
await admin.getByRole("button", { name: /Tambah label/ }).click();
await admin.getByLabel("Nama", { exact: true }).fill(LABEL);
await admin.getByLabel("Deskripsi").fill("Wadah khusus sisa makanan");
await admin.getByRole("button", { name: "Tambah", exact: true }).click();
await admin.getByText(`Label "${LABEL}" ditambahkan.`).waitFor();
step("admin membuat label");
await admin.goto(BASE + "/admin/kelompok");
await admin.getByRole("button", { name: /Tambah kelompok/ }).click();
await admin.getByLabel("Nama kelompok").fill(GROUP);
await admin.getByRole("button", { name: "Tambah", exact: true }).click();
await admin.getByText(`Kelompok "${GROUP}" ditambahkan.`).waitFor();
await admin.getByRole("row").filter({ hasText: GROUP }).getByText("Aktif").waitFor();
await shot("00-admin-kelompok", admin);
step("admin membuat kelompok");

// 1. tanpa sesi → /masuk; daftar dengan NIM + kelompok
await page.goto(BASE + "/");
await page.waitForURL(/\/masuk/);
await shot("01-masuk");
await page.getByRole("tab", { name: "Daftar baru" }).click();
await page.getByLabel("Nama", { exact: true }).fill("Budi Santoso");
await page.getByLabel("Email").fill("budi@example.com");
await page.getByLabel("NIM").fill(NIM);
if ((await page.getByLabel("NIM").inputValue()) !== NIM_UPPER) throw new Error("NIM tidak otomatis huruf besar");
await page.getByLabel("Kelompok").selectOption({ label: GROUP });
await page.getByLabel(/Nomor WhatsApp/).fill(PHONE);
await page.getByLabel("Password", { exact: true }).fill("rahasia123");
await page.getByLabel("Ulangi password").fill("rahasia12");
await page.getByRole("button", { name: /Daftar & kirim kode/ }).click();
await page.getByText("Konfirmasi password tidak sama.").waitFor();
step("konfirmasi password berbeda ditolak di form");
await page.getByLabel("Ulangi password").fill("rahasia123");
await noHorizontalScroll("form daftar");
await shot("02-daftar");
await page.getByRole("button", { name: /Daftar & kirim kode/ }).click();
await page.waitForURL(/\/masuk\/kode/);
await page.getByText("Kode sudah dikirim ke WhatsApp Anda.").waitFor();
step("register (NIM + kelompok) → kode terkirim");
await page.getByLabel(/Kode 6 digit/).fill("000000");
await page.getByText(/Kode salah\. Sisa 4 percobaan\./).waitFor();
step("kode salah → sisa 4 percobaan");
await page.getByLabel(/Kode 6 digit/).fill(await stubCode(CANON));
await page.waitForURL(BASE + "/");
await page.getByText("Belum ada pendataan dari perangkat ini.").waitFor();
await shot("03-beranda-kosong");
step("login berhasil → beranda");

// 2. pendataan menolak: hanya bagian A, langsung bisa dikirim
await page.getByRole("link", { name: "Pendataan baru" }).click();
await page.waitForURL(BASE + "/pendataan/baru");
await fillIdentity({ rw: "05", rt: "03", code: "RT03-RW05-11", initials: "Tn. A" });
await page.getByText("Bagian 1 dari 9").waitFor();
await choose(/^Persetujuan pendataan/, "Tidak bersedia");
await page.getByText("Responden tidak bersedia: hanya bagian A yang disimpan.", { exact: false }).waitFor();
await page.getByText("Bagian 1 dari 1").waitFor();
if (await page.getByRole("button", { name: "Lanjut" }).count()) throw new Error("menolak masih menampilkan Lanjut");
await noHorizontalScroll("bagian A (menolak)");
await shot("04-menolak");
await page.getByRole("button", { name: "Kirim", exact: true }).click();
await page.waitForURL(/\/s\/[0-9a-f-]+$/);
await page.getByText("Pendataan terkirim.", { exact: false }).waitFor();
await page.getByText("Tidak bersedia").first().waitFor();
const declinedId = new URL(page.url()).pathname.split("/").pop();
await shot("05-menolak-terkirim");
step("pendataan menolak terkirim (hanya bagian A)");

// 3. pendataan bersedia: semua bagian, 1 foto + 1 kotak berlabel
await page.goto(BASE + "/");
await page.locator(".history-item", { hasText: "Menolak" }).waitFor();
await page.getByRole("link", { name: "Pendataan baru" }).click();
await page.waitForURL(BASE + "/pendataan/baru");
if (await page.getByText("Isian yang belum dikirim dipulihkan.").count()) throw new Error("draf tidak terhapus setelah kirim");
await fillIdentity({ rw: "07", rt: "02", code: "RT02-RW07-15", initials: "Ny. S" });
await choose(/^Persetujuan pendataan/, "Bersedia");
await page.getByText("Bagian 1 dari 9").waitFor();
await shot("06-bagian-a");
await next();
for (const title of ["1. Pengetahuan", "2. Kondisi", "3. Pengelolaan", "4. Hilirisasi", "5. Dukungan", "C. Checklist"]) {
  await atSection(title);
  await chooseFirstEverywhere();
  if (title === "1. Pengetahuan") {
    await noHorizontalScroll("bagian B1");
    await shot("07-bagian-b1");
  }
  await next();
}
step("bagian B1–B5 + C terisi");
await atSection("R2. Data rumah tangga");
await page.getByLabel(/^Jumlah penghuni rumah/).fill("4");
await choose(/^Usia kepala keluarga/, "30–44 tahun");
await next();
await atSection("R3. Foto dan label wadah");
await page.getByText("Protokol foto:", { exact: false }).waitFor();
// lanjut tanpa foto → ringkasan menandai R3 belum lengkap, Kirim ditahan
await choose(/^Label lapangan/, "Sebagian");
await choose(/^Isi wadah terlihat/, "Ya, terlihat jelas");
await next();
await page.getByRole("heading", { name: "Ringkasan" }).waitFor();
const r3Summary = page.locator(".survey-summary-item", { hasText: "R3." });
await r3Summary.getByText("Belum lengkap").waitFor();
await page.getByText("1 bagian belum lengkap.").waitFor();
await page.getByRole("button", { name: "Kirim", exact: true }).click();
if (!page.url().endsWith("/pendataan/baru")) throw new Error("terkirim tanpa foto");
step("tanpa foto: ringkasan menandai R3, kirim ditahan");
await r3Summary.click();
await atSection("R3. Foto dan label wadah");
await page.getByText("Tambahkan minimal 1 foto.", { exact: false }).waitFor();
await page.locator("input[type=file][multiple]").setInputFiles(`${dir}/img/big.jpg`);
await page.locator(".photo-status", { hasText: "Siap" }).first().waitFor({ timeout: 30000 });
step("1 foto diproses & terunggah");
const svg = page.getByRole("application");
await svg.scrollIntoViewIfNeeded();
const b = await svg.boundingBox();
await page.mouse.move(b.x + b.width * 0.15, b.y + b.height * 0.15);
await page.mouse.down();
await page.mouse.move(b.x + b.width * 0.6, b.y + b.height * 0.55, { steps: 8 });
await page.mouse.up();
await page.getByLabel("Label kotak 1").selectOption({ label: LABEL });
await page.getByText("Wadah khusus sisa makanan").waitFor(); // deskripsi label sebagai petunjuk
await page.getByRole("group", { name: /^Jenis wadah/ }).locator("label.choice", { hasText: "Lainnya" }).click();
await page.getByLabel("Sebutkan lainnya").fill("Galon bekas");
await page.getByLabel(/^Jumlah wadah/).fill("2");
await noHorizontalScroll("bagian R3");
await shot("08-r3-foto-kotak");
await next();
await page.getByRole("heading", { name: "Ringkasan" }).waitFor();
await page.getByText("1 foto · 1 kotak", { exact: false }).waitFor();
if (await page.getByText("Belum lengkap").count()) throw new Error("masih ada bagian belum lengkap");
await shot("09-ringkasan");
await page.getByRole("button", { name: "Kirim", exact: true }).click();
await page.waitForURL(/\/s\/[0-9a-f-]+$/);
await page.getByText("Pendataan terkirim.", { exact: false }).waitFor();
await page.waitForFunction(() => { const imgs = [...document.querySelectorAll(".box-editor img")]; return imgs.length === 1 && imgs.every((e) => e.complete && e.naturalWidth > 0); }, null, { timeout: 15000 });
await page.getByText(LABEL).first().waitFor(); // nama label di tampilan baca
await page.getByText("Galon bekas").waitFor(); // teks "Lainnya"
await page.getByText("30–44 tahun").waitFor(); // teks opsi, bukan id
await page.getByText("Menunggu review").first().waitFor();
const submissionPath = new URL(page.url()).pathname;
const submissionId = submissionPath.split("/").pop();
await shot("10-bersedia-terkirim");
step("pendataan bersedia terkirim → foto termuat, jawaban tampil dengan teks opsi");

// 4. edit + reload di tengah edit → draf pulih → simpan
await page.getByRole("button", { name: "Edit" }).click();
await atSection("A. Identitas Pendataan");
await page.getByLabel(/^Inisial responden/).fill("Ny. T");
await page.waitForTimeout(300);
await page.reload();
await page.getByRole("button", { name: "Edit" }).click();
await page.getByText("Perubahan yang belum disimpan dipulihkan.").waitFor();
if ((await page.getByLabel(/^Inisial responden/).inputValue()) !== "Ny. T") throw new Error("draf tidak pulih");
step("reload di tengah edit → draf (jawaban) pulih");
await toSummary();
await page.getByRole("button", { name: "Simpan", exact: true }).click();
await page.getByText("Perubahan tersimpan.").waitFor();
await page.getByText("Ny. T").waitFor();
step("edit tersimpan (versi 2)");

// 5. admin: daftar pendataan → detail (jawaban dengan teks opsi, NIM, kelompok) → tolak dengan alasan
await admin.goto(BASE + "/admin/anotasi?review_status=pending&sort=oldest");
await admin.getByText("Budi Santoso").first().waitFor();
await admin.getByText(NIM_UPPER).first().waitFor();
await admin.getByText("RT02-RW07-15", { exact: false }).first().waitFor();
await admin.getByText("RT03-RW05-11", { exact: false }).first().waitFor();
await shot("11-admin-pendataan", admin);
step("admin: daftar pendataan memuat kedua pendataan + NIM");
await admin.goto(BASE + "/admin/anotasi/" + declinedId);
await admin.getByText("Tidak bersedia").first().waitFor();
if (await admin.getByText("C. Checklist").count()) throw new Error("pendataan menolak menampilkan bagian selain A");
step("admin: detail pendataan menolak hanya bagian A");
await admin.goto(BASE + "/admin/anotasi/" + submissionId);
await admin.getByRole("heading", { name: "Foto 1 dari 1" }).waitFor();
await admin.getByText(LABEL).first().waitFor();
await admin.getByText("budi@example.com").waitFor();
await admin.getByText(NIM_UPPER).first().waitFor();
await admin.getByText(GROUP).first().waitFor();
await admin.getByRole("heading", { name: "C. Checklist Verifikasi Observasi Langsung" }).waitFor();
await admin.getByText("Sebagian").first().waitFor();
await admin.getByText("Galon bekas").waitFor();
await admin.getByText("30–44 tahun").waitFor();
await admin.getByText("Kepala keluarga").first().waitFor();
await shot("12-admin-detail", admin);
step("admin melihat detail: foto berkotak, label, NIM, kelompok, jawaban dengan teks opsi");
await admin.getByRole("button", { name: /^Tolak$/ }).click();
await admin.getByLabel(/Alasan penolakan/).fill("Foto wadah kurang jelas");
await admin.getByRole("dialog").getByRole("button", { name: /Tolak/ }).click();
await admin.getByText("Submission ditolak.").waitFor();
step("admin menolak dengan alasan");
await page.reload();
await page.getByText("Ditolak").first().waitFor();
await page.getByText("Alasan: Foto wadah kurang jelas").waitFor();
await page.getByRole("button", { name: "Perbaiki" }).waitFor();
step("user melihat lencana Ditolak + alasan + Perbaiki");

// 6. riwayat & logout
await page.goto(BASE + "/");
const histDone = page.locator(".history-item", { hasText: "Bersedia" });
await histDone.getByText("RT 02 / RW 07 · RT02-RW07-15").waitFor();
await histDone.getByText("1 foto · 1 kotak").waitFor();
await histDone.getByText("Ditolak").waitFor();
await page.locator(".history-item", { hasText: "Menolak" }).getByText("RT 03 / RW 05 · RT03-RW05-11").waitFor();
await noHorizontalScroll("beranda");
await shot("13-riwayat");
step("riwayat: Bersedia/Menolak, RT/RW, kode, lencana review");
await page.getByRole("button", { name: "Keluar" }).click();
await page.getByRole("dialog").getByRole("button", { name: "Keluar" }).click();
await page.waitForURL(/\/masuk/);
await page.goto(BASE + "/pendataan/baru");
await page.waitForURL(/\/masuk\?next=%2Fpendataan%2Fbaru/);
step("logout → rute dilindungi mengarah ke /masuk?next=");

// 7. masuk dengan nomor + password (tanpa OTP); password salah dulu
await page.getByLabel(/Nomor WhatsApp/).fill(PHONE);
await page.getByLabel("Password", { exact: true }).fill("salahsalah");
await page.getByRole("button", { name: "Masuk", exact: true }).click();
await page.getByText("Nomor HP atau password salah.").waitFor();
await page.getByLabel("Password", { exact: true }).fill("rahasia123");
await page.getByRole("button", { name: "Masuk", exact: true }).click();
await page.waitForURL(BASE + "/pendataan/baru");
await page.getByText(NIM_UPPER).waitFor(); // identitas mahasiswa dari sesi baru
step("password salah ditolak; password benar → kembali ke /pendataan/baru tanpa OTP");

// 8. lupa password → kode → password baru → masuk
await page.goto(BASE + "/");
await page.getByRole("button", { name: "Keluar" }).click();
await page.getByRole("dialog").getByRole("button", { name: "Keluar" }).click();
await page.waitForURL(/\/masuk/);
await page.getByRole("link", { name: "Lupa password?" }).click();
await page.getByLabel(/Nomor WhatsApp terdaftar/).fill(PHONE);
await page.getByRole("button", { name: "Kirim kode WhatsApp" }).click();
await page.waitForURL(/\/masuk\/kode/);
await page.getByText("Kode sudah dikirim ke WhatsApp Anda.").waitFor();
await page.getByLabel(/Kode 6 digit/).fill(await stubCode(CANON));
await page.getByLabel("Password baru", { exact: true }).fill("passwordbaru1");
await page.getByLabel("Ulangi password baru").fill("passwordbaru1");
await page.getByRole("button", { name: "Simpan password & masuk" }).click();
const inApp = (u) => !u.pathname.startsWith("/masuk");
await page.waitForURL(inApp);
step("lupa password: kode + password baru → langsung masuk");

// 9. admin menghapus pendataan (ketik HAPUS) → user melihat "sudah dihapus"; arsipkan label + kelompok
await admin.goto(BASE + "/admin/anotasi/" + submissionId);
await admin.getByRole("button", { name: /Hapus submission/ }).click();
const del = admin.getByRole("dialog");
await del.getByText(/1 foto, 1 kotak anotasi milik Budi Santoso/).waitFor();
await del.getByLabel(/Ketik HAPUS/).fill("HAPUS");
await del.getByRole("button", { name: /Hapus submission/ }).click();
await admin.getByText(/Submission dihapus/).waitFor();
step("admin menghapus pendataan");
await page.goto(BASE + submissionPath);
await page.getByText(/Pendataan ini sudah dihapus/).waitFor();
step("user: pendataan yang dihapus admin → 'sudah dihapus'");
await admin.goto(BASE + "/admin/label");
await admin.getByRole("row").filter({ hasText: LABEL }).getByRole("button", { name: /Arsipkan/ }).click();
await admin.getByText(`Label "${LABEL}" diarsipkan.`, { exact: false }).waitFor();
// kelompok sudah dipakai akun: tidak bisa dihapus, hanya diarsipkan
await admin.goto(BASE + "/admin/kelompok");
const groupRow = admin.getByRole("row").filter({ hasText: GROUP });
await groupRow.getByRole("cell", { name: "1", exact: true }).waitFor(); // jumlah mahasiswa
if (await groupRow.getByRole("button", { name: /Hapus/ }).count()) throw new Error("kelompok terpakai masih bisa dihapus");
await groupRow.getByRole("button", { name: /Arsipkan/ }).click();
await admin.getByText(`Kelompok "${GROUP}" diarsipkan.`, { exact: false }).waitFor();
step("admin arsipkan label + kelompok terpakai");
await admin.getByRole("button", { name: /Keluar/ }).click();
await admin.waitForURL(/\/admin\/masuk$/);
step("admin keluar");

console.log(logs.length ? "LOG BROWSER:\n" + logs.join("\n") : "tanpa error di console browser");
await browser.close();
