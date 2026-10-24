// Uji end-to-end alur lengkap di Chrome (viewport HP 390×844, layar sentuh) memakai playwright-core.
//
// Prasyarat (lihat web/README.md): API lokal mode otp (auth.otp.resend_cooldown pendek, mis. "2s") dengan stub wa-notif di 127.0.0.1:13030
// (cmd/wanotif-stub -token …), Vite dev di http://localhost:5173 dengan VITE_AUTH_MODE=otp dan
// VITE_DEV_API_TARGET ke API, serta public_base_url API = http://localhost:5173.
// Admin: admin.enabled = true dan admin.superadmin.phone = ADMIN_PHONE (default 0812-0000-0001, sesuaikan
// config API; ADMIN_CANON = bentuk kanonik, default 6281200000001).
//   node e2e/flow.mjs <folder-kerja>     (folder berisi img/big.jpg; screenshot ke <folder>/shots)
// Variabel CHROME_PATH mengganti lokasi Chrome (default /usr/bin/google-chrome).
//
// Alur: admin masuk (OTP) → buat 2 label → user daftar/masuk → foto + kotak + pilih label → edit →
// admin melihat anotasi + detail → tolak dengan alasan → user melihat lencana "Ditolak" → admin hapus
// submission (ketik HAPUS) → user melihat "sudah dihapus" → admin hapus label belum terpakai.
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
const RUN = DIGITS.slice(-4); // nama label unik per run (nama label unik tanpa beda huruf besar-kecil)
const LABEL_A = `Retak ${RUN}`;
const LABEL_B = `Penyok ${RUN}`;
const LABEL_UNUSED = `Tak terpakai ${RUN}`;
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

// 0. admin: masuk dengan OTP WhatsApp, buat label
await admin.goto(BASE + "/admin/label");
await admin.waitForURL(/\/admin\/masuk\?next=%2Fadmin%2Flabel/);
await admin.getByLabel(/Nomor WhatsApp superadmin/).fill(ADMIN_PHONE);
await admin.getByRole("button", { name: /Kirim kode WhatsApp/ }).click();
await admin.getByText("Kode sudah dikirim ke WhatsApp Anda.").waitFor();
await shot("00-admin-kode", admin);
await admin.getByLabel(/Kode 6 digit/).fill(await stubCode(ADMIN_CANON));
await admin.waitForURL(BASE + "/admin/label");
step("admin masuk dengan OTP → /admin/label");
for (const [name, desc] of [[LABEL_A, "Retakan pada dinding paket"], [LABEL_B, ""], [LABEL_UNUSED, ""]]) {
  await admin.getByRole("button", { name: /Tambah label/ }).click();
  await admin.getByLabel("Nama", { exact: true }).fill(name);
  if (desc) await admin.getByLabel("Deskripsi").fill(desc);
  await admin.getByRole("button", { name: "Tambah", exact: true }).click();
  await admin.getByText(`Label "${name}" ditambahkan.`).waitFor();
}
await shot("00-admin-label", admin);
step("admin membuat 3 label");

// 1. tanpa sesi → /masuk; daftar
await page.goto(BASE + "/");
await page.waitForURL(/\/masuk/);
await shot("01-masuk");
await page.getByRole("tab", { name: "Daftar baru" }).click();
await page.getByLabel("Nama").fill("Budi Santoso");
await page.getByLabel("Email").fill("budi@example.com");
await page.getByLabel(/Nomor WhatsApp/).fill(PHONE);
await page.getByLabel("Password", { exact: true }).fill("rahasia123");
await page.getByLabel("Ulangi password").fill("rahasia12");
await page.getByRole("button", { name: /Daftar & kirim kode/ }).click();
await page.getByText("Konfirmasi password tidak sama.").waitFor();
step("konfirmasi password berbeda ditolak di form");
await page.getByLabel("Ulangi password").fill("rahasia123");
await shot("02-daftar");
await page.getByRole("button", { name: /Daftar & kirim kode/ }).click();
await page.waitForURL(/\/masuk\/kode/);
await page.getByText("Kode sudah dikirim ke WhatsApp Anda.").waitFor();
await shot("03-kode");
step("register → kode terkirim");

// kode salah dulu, lalu benar (otomatis terkirim saat 6 digit)
await page.getByLabel(/Kode 6 digit/).fill("000000");
await page.getByText(/Kode salah\. Sisa 4 percobaan\./).waitFor();
step("kode salah → sisa 4 percobaan");
await page.getByLabel(/Kode 6 digit/).fill(await stubCode(CANON));
await page.waitForURL(BASE + "/");
step("login berhasil → halaman laporan baru");
await shot("04-baru-kosong");

// 2. dua foto dari galeri
const gallery = page.locator('input[type=file][multiple]');
await gallery.setInputFiles([`${dir}/img/big.jpg`, `${dir}/img/big.jpg`]);
await page.locator(".photo-status", { hasText: "Siap" }).nth(1).waitFor({ timeout: 30000 });
step("2 foto diproses & terunggah");
await shot("05-foto-terunggah");

// gambar kotak di foto 1 (seret)
async function drawBox(fx0, fy0, fx1, fy1) {
  const svg = page.getByRole("application");
  const b = await svg.boundingBox();
  await page.mouse.move(b.x + b.width * fx0, b.y + b.height * fy0);
  await page.mouse.down();
  await page.mouse.move(b.x + b.width * fx1, b.y + b.height * fy1, { steps: 8 });
  await page.mouse.up();
}
await drawBox(0.1, 0.1, 0.5, 0.45);
await page.getByLabel("Label kotak 1").selectOption({ label: LABEL_A });
await page.getByText("Retakan pada dinding paket").waitFor(); // deskripsi label sebagai petunjuk
await drawBox(0.62, 0.62, 0.95, 0.95);
await shot("06-dua-kotak-satu-kosong");
if (await page.getByRole("textbox", { name: /Catatan/ }).count()) throw new Error("masih ada isian teks bebas");
const submitBtn = page.getByRole("button", { name: "Kirim" });
await submitBtn.click();
await page.getByText("Pilih label.").waitFor();
step("kotak tanpa label menahan kirim");
await page.getByLabel("Label kotak 2").selectOption({ label: LABEL_B });
// foto 2: satu kotak
await page.getByRole("button", { name: /^Foto 2/ }).click();
await drawBox(0.3, 0.3, 0.6, 0.7);
await page.getByLabel("Label kotak 1").selectOption({ label: LABEL_A });
await shot("07-siap-kirim");
await submitBtn.click();
await page.waitForURL(/\/s\/[0-9a-f-]+$/);
await page.getByText("Laporan terkirim.").waitFor();
await page.waitForFunction(() => { const imgs = [...document.querySelectorAll(".box-editor img")]; return imgs.length === 2 && imgs.every((e) => e.complete && e.naturalWidth > 0); }, null, { timeout: 15000 });
const dims = await page.locator(".box-editor img").evaluateAll((els) => els.map((e) => `${e.naturalWidth}x${e.naturalHeight}`));
step(`terkirim → halaman laporan, 2 foto termuat dari API (${dims.join(", ")})`);
await page.getByText(LABEL_B).first().waitFor(); // nama label tampil di tampilan baca
await page.getByText("Menunggu review").waitFor();
const submissionPath = new URL(page.url()).pathname;
const submissionId = submissionPath.split("/").pop();
await shot("08-laporan");

// 3. edit + reload di tengah edit → draf pulih → simpan
await page.getByRole("button", { name: "Edit" }).click();
await page.getByLabel("Label kotak 1").selectOption({ label: LABEL_B });
await page.waitForTimeout(300);
await page.reload();
await page.getByRole("button", { name: "Edit" }).click();
await page.getByText("Perubahan yang belum disimpan dipulihkan.").waitFor();
if ((await page.getByLabel("Label kotak 1").locator("option:checked").textContent()) !== LABEL_B) throw new Error("draf tidak pulih");
step("reload di tengah edit → draf (label) pulih");
await shot("09-edit-draf");
await page.getByRole("button", { name: "Simpan" }).click();
await page.getByText("Perubahan tersimpan.").waitFor();
step("edit tersimpan (versi 2)");

// 3b. admin: daftar anotasi → detail → tolak dengan alasan
await admin.goto(BASE + "/admin/anotasi?review_status=pending&sort=oldest");
await admin.getByText("Budi Santoso").first().waitFor();
await admin.getByText(CANON).first().waitFor(); // nomor utuh annotator
await shot("09a-admin-anotasi", admin);
await admin.goto(BASE + "/admin/anotasi/" + submissionId);
await admin.getByRole("heading", { name: "Foto 1 dari 2" }).waitFor();
await admin.getByText(LABEL_B).first().waitFor();
await admin.getByText("budi@example.com").waitFor();
step("admin melihat submission: foto, label, annotator");
await admin.getByRole("button", { name: /^Tolak$/ }).click();
await admin.getByLabel(/Alasan penolakan/).fill("Kotak tidak menutupi seluruh kerusakan");
await admin.getByRole("dialog").getByRole("button", { name: /Tolak/ }).click();
await admin.getByText("Submission ditolak.").waitFor();
await shot("09b-admin-ditolak", admin);
step("admin menolak dengan alasan");
await page.reload();
await page.getByText("Ditolak").first().waitFor();
await page.getByText("Alasan: Kotak tidak menutupi seluruh kerusakan").waitFor();
await page.getByRole("button", { name: "Perbaiki" }).waitFor();
step("user melihat lencana Ditolak + alasan + Perbaiki");

// 4. riwayat & logout
await page.getByRole("link", { name: "Riwayat" }).click();
await page.getByText(/2 foto · 3 kotak/).waitFor();
await page.getByText("Ditolak").first().waitFor();
await shot("10-riwayat");
step("riwayat menampilkan laporan + lencana review");
await page.getByRole("button", { name: "Keluar" }).click();
await page.getByRole("dialog").getByRole("button", { name: "Keluar" }).click();
await page.waitForURL(/\/masuk/);
await page.goto(BASE + "/riwayat");
await page.waitForURL(/\/masuk\?next=%2Friwayat/);
step("logout → rute dilindungi mengarah ke /masuk?next=");

// 5. masuk dengan nomor + password (tanpa OTP); password salah dulu
await page.getByLabel(/Nomor WhatsApp/).fill(PHONE);
await page.getByLabel("Password", { exact: true }).fill("salahsalah");
await page.getByRole("button", { name: "Masuk", exact: true }).click();
await page.getByText("Nomor HP atau password salah.").waitFor();
await shot("11-login-salah");
await page.getByLabel("Password", { exact: true }).fill("rahasia123");
await page.getByRole("button", { name: "Masuk", exact: true }).click();
await page.waitForURL(BASE + "/riwayat");
step("password salah ditolak; password benar → kembali ke /riwayat tanpa OTP");

// 6. lupa password → kode → password baru → masuk
await page.getByRole("button", { name: "Keluar" }).click();
await page.getByRole("dialog").getByRole("button", { name: "Keluar" }).click();
await page.waitForURL(/\/masuk/);
await page.getByRole("link", { name: "Lupa password?" }).click();
await page.getByLabel(/Nomor WhatsApp terdaftar/).fill(PHONE);
await shot("12-lupa");
await page.getByRole("button", { name: "Kirim kode WhatsApp" }).click();
await page.waitForURL(/\/masuk\/kode/);
await page.getByText("Kode sudah dikirim ke WhatsApp Anda.").waitFor();
await page.getByLabel(/Kode 6 digit/).fill(await stubCode(CANON));
await page.getByLabel("Password baru", { exact: true }).fill("passwordbaru1");
await page.getByLabel("Ulangi password baru").fill("passwordbaru1");
await shot("13-password-baru");
await page.getByRole("button", { name: "Simpan password & masuk" }).click();
const inApp = (u) => !u.pathname.startsWith("/masuk");
await page.waitForURL(inApp); // kembali ke halaman terakhir (?next=)
step("lupa password: kode + password baru → langsung masuk");
await page.getByRole("button", { name: "Keluar" }).click();
await page.getByRole("dialog").getByRole("button", { name: "Keluar" }).click();
await page.waitForURL(/\/masuk/);
await page.getByLabel(/Nomor WhatsApp/).fill(PHONE);
await page.getByLabel("Password", { exact: true }).fill("passwordbaru1");
await page.getByRole("button", { name: "Masuk", exact: true }).click();
await page.waitForURL(inApp);
step("masuk dengan password baru");

// 7. admin menghapus submission (ketik HAPUS) → user melihat "sudah dihapus"; hapus label tak terpakai
await admin.goto(BASE + "/admin/anotasi/" + submissionId);
await admin.getByRole("button", { name: /Hapus submission/ }).click();
const del = admin.getByRole("dialog");
await del.getByText(/2 foto, 3 kotak anotasi milik Budi Santoso/).waitFor();
await del.getByLabel(/Ketik HAPUS/).fill("HAPUS");
await shot("14-admin-hapus", admin);
await del.getByRole("button", { name: /Hapus submission/ }).click();
await admin.getByText(/Submission dihapus/).waitFor();
step("admin menghapus submission");
await page.goto(BASE + submissionPath);
await page.getByText(/Laporan ini sudah dihapus/).waitFor();
step("user: laporan yang dihapus admin → 'sudah dihapus'");
await admin.goto(BASE + "/admin/label");
const row = admin.getByRole("row").filter({ hasText: LABEL_UNUSED });
await row.getByRole("button", { name: /Hapus/ }).click();
await admin.getByRole("dialog").getByRole("button", { name: /Hapus label/ }).click();
await admin.getByText(`Label "${LABEL_UNUSED}" dihapus.`).waitFor();
// label yang pernah dipakai: setelah submission dihapus pemakaiannya 0, jadi diarsipkan saja di sini
await admin.getByRole("row").filter({ hasText: LABEL_A }).getByRole("button", { name: /Arsipkan/ }).click();
await admin.getByText(`Label "${LABEL_A}" diarsipkan.`, { exact: false }).waitFor();
step("admin hapus label tak terpakai, arsipkan label lain");
await admin.getByRole("button", { name: /Keluar/ }).click();
await admin.waitForURL(/\/admin\/masuk$/);
step("admin keluar");

console.log(logs.length ? "LOG BROWSER:\n" + logs.join("\n") : "tanpa error di console browser");
await browser.close();
