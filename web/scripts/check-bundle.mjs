// Dijalankan setelah `vite build`: bundle user (chunk entry yang dimuat index.html) tidak boleh
// memuat kode dashboard admin (spec-labels-admin §7.2, §9 #12). Dashboard harus chunk terpisah.
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const dist = new URL("../dist/", import.meta.url).pathname;
const html = readFileSync(join(dist, "index.html"), "utf8");
const entries = [...html.matchAll(/(?:src|href)="\/assets\/([^"]+\.(?:js|css))"/g)].map((m) => m[1]);
// penanda yang hanya ada di kode admin
// (termasuk ikon yang hanya dipakai dashboard: ikon di-import per berkas agar ikut chunk admin)
const MARKERS = ["/api/v1/admin", "pa.admin.session", "Masuk admin", "admin-sidebar", "temperature-half", "hard-drive", "ellipsis-vertical"];

let failed = false;
for (const f of entries) {
  const src = readFileSync(join(dist, "assets", f), "utf8");
  for (const m of MARKERS) {
    if (src.includes(m)) {
      console.error(`bundle user ${f} memuat kode admin (${m})`);
      failed = true;
    }
  }
}
const all = readdirSync(join(dist, "assets"));
const admin = all.filter((f) => /^AdminApp-.*\.js$/.test(f));
if (admin.length !== 1) {
  console.error(`chunk admin tidak ditemukan (AdminApp-*.js): ${admin.join(", ")}`);
  failed = true;
}
const size = (f) => (readFileSync(join(dist, "assets", f)).length / 1024).toFixed(1);
console.log(`bundle user: ${entries.map((f) => `${f} ${size(f)} kB`).join(", ")}`);
console.log(`chunk admin: ${all.filter((f) => f.startsWith("AdminApp-")).map((f) => `${f} ${size(f)} kB`).join(", ")}`);
if (failed) process.exit(1);
