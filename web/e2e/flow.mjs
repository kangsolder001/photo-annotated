// Uji end-to-end alur lengkap di Chrome (viewport HP 390×844, layar sentuh) memakai playwright-core.
//
// Prasyarat (lihat web/README.md): API lokal mode otp (auth.otp.resend_cooldown pendek, mis. "2s") dengan stub wa-notif di 127.0.0.1:13030
// (cmd/wanotif-stub -token …), Vite dev di http://localhost:5173 dengan VITE_AUTH_MODE=otp dan
// VITE_DEV_API_TARGET ke API, serta public_base_url API = http://localhost:5173.
//   node e2e/flow.mjs <folder-kerja>     (folder berisi img/big.jpg; screenshot ke <folder>/shots)
// Variabel CHROME_PATH mengganti lokasi Chrome (default /usr/bin/google-chrome).
import { chromium } from "playwright-core";
import { argv } from "node:process";

const dir = argv[2];
const BASE = "http://localhost:5173";
// nomor acak per run: nomor yang sudah aktif tidak bisa didaftarkan ulang (tidak ada kode dikirim)
const DIGITS = String(Math.floor(1e7 + Math.random() * 9e7));
const PHONE = `0812${DIGITS}`; // seperti diketik
const CANON = `62812${DIGITS}`; // bentuk kanonik di server
const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH ?? "/usr/bin/google-chrome", headless: true });
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, hasTouch: true, isMobile: true, locale: "id-ID" });
const page = await ctx.newPage();
const logs = [];
page.on("console", (m) => { if (m.type() === "error" || m.type() === "warning") logs.push(`${m.type()}: ${m.text()}`); });
page.on("pageerror", (e) => logs.push(`pageerror: ${e.message}`));
const shot = (n) => page.screenshot({ path: `${dir}/shots/${n}.png`, fullPage: true });
const step = (s) => console.log("✓", s);

async function stubCode(phone) {
  for (let i = 0; i < 20; i++) {
    const msgs = await (await fetch("http://127.0.0.1:13030/_stub/messages")).json();
    const m = [...msgs].reverse().find((x) => x.To === phone);
    if (m) return m.Text.match(/\*(\d{6})\*/)[1];
    await new Promise((r) => setTimeout(r, 300));
  }
  throw new Error("kode tidak terkirim");
}

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
await page.keyboard.type("Penyok di sudut kiri atas");
await drawBox(0.62, 0.62, 0.95, 0.95);
await shot("06-dua-kotak-satu-kosong");
const submitBtn = page.getByRole("button", { name: "Kirim" });
await submitBtn.click();
await page.getByText("Catatan wajib diisi.").waitFor();
step("kotak tanpa catatan menahan kirim");
await page.getByLabel("Catatan kotak 2").fill("Label <resi> & sobek");
// foto 2: satu kotak
await page.getByRole("button", { name: /^Foto 2/ }).click();
await drawBox(0.3, 0.3, 0.6, 0.7);
await page.keyboard.type("Lecet");
await shot("07-siap-kirim");
await submitBtn.click();
await page.waitForURL(/\/s\/[0-9a-f-]+$/);
await page.getByText("Laporan terkirim.").waitFor();
await page.waitForFunction(() => { const imgs = [...document.querySelectorAll(".box-editor img")]; return imgs.length === 2 && imgs.every((e) => e.complete && e.naturalWidth > 0); }, null, { timeout: 15000 });
const dims = await page.locator(".box-editor img").evaluateAll((els) => els.map((e) => `${e.naturalWidth}x${e.naturalHeight}`));
step(`terkirim → halaman laporan, 2 foto termuat dari API (${dims.join(", ")})`);
await page.getByText("Label <resi> & sobek").waitFor(); // teks tampil apa adanya, bukan HTML
await shot("08-laporan");

// 3. edit + reload di tengah edit → draf pulih → simpan
await page.getByRole("button", { name: "Edit" }).click();
await page.getByLabel("Catatan kotak 1").fill("Penyok di sudut kiri atas (diperbarui)");
await page.waitForTimeout(300);
await page.reload();
await page.getByRole("button", { name: "Edit" }).click();
await page.getByText("Perubahan yang belum disimpan dipulihkan.").waitFor();
if ((await page.getByLabel("Catatan kotak 1").inputValue()) !== "Penyok di sudut kiri atas (diperbarui)") throw new Error("draf tidak pulih");
step("reload di tengah edit → draf pulih");
await shot("09-edit-draf");
await page.getByRole("button", { name: "Simpan" }).click();
await page.getByText("Perubahan tersimpan.").waitFor();
await page.getByText("Penyok di sudut kiri atas (diperbarui)").waitFor();
step("edit tersimpan (versi 2)");

// 4. riwayat & logout
await page.getByRole("link", { name: "Riwayat" }).click();
await page.getByText(/2 foto · 3 kotak/).waitFor();
await shot("10-riwayat");
step("riwayat menampilkan laporan");
await page.getByRole("button", { name: "Keluar" }).click();
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
await page.waitForURL(/\/masuk/);
await page.getByLabel(/Nomor WhatsApp/).fill(PHONE);
await page.getByLabel("Password", { exact: true }).fill("passwordbaru1");
await page.getByRole("button", { name: "Masuk", exact: true }).click();
await page.waitForURL(inApp);
step("masuk dengan password baru");

console.log(logs.length ? "LOG BROWSER:\n" + logs.join("\n") : "tanpa error di console browser");
await browser.close();
