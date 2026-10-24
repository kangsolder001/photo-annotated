/// <reference types="node" />
// @vitest-environment node
// Aturan lintas berkas yang dicek otomatis (spec-labels-admin §7.5, §9 #25):
//  - tidak ada emoji di teks aplikasi (src + index.html), di luar komentar;
//  - ikon/font tidak dimuat dari domain luar (tanpa CDN/web font); satu-satunya URL luar = Turnstile.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const SRC = fileURLToPath(new URL(".", import.meta.url));
const WEB = join(SRC, "..");

function files(dir: string): string[] {
  return readdirSync(dir).flatMap((n) => {
    const p = join(dir, n);
    return statSync(p).isDirectory() ? files(p) : /\.(tsx?|css|html)$/.test(n) ? [p] : [];
  });
}

// berkas ini sendiri berisi contoh emoji untuk menguji pemindai
const all = [...files(SRC), join(WEB, "index.html")].filter((f) => !f.endsWith("conventions.test.ts"));
// URL di nama test tidak dikirim ke browser
const shipped = all.filter((f) => !/\.test\.tsx?$/.test(f));

/** Buang komentar (blok, baris, HTML). `//` setelah `:` (URL) tidak dianggap komentar. */
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/(^|[^:\\"'`])\/\/.*$/gm, "$1");
}

// Extended_Pictographic mencakup emoji (wajah, kamera, pensil, tanda peringatan…); ditambah bendera dan variation selector-16.
const EMOJI = /[\p{Extended_Pictographic}\p{Regional_Indicator}\u{FE0F}\u{20E3}]/u;

describe("pemindai emoji", () => {
  it("mendeteksi emoji dan mengabaikan komentar", () => {
    expect(EMOJI.test(stripComments('const a = "\u{1F4F7} Foto";'))).toBe(true);
    expect(EMOJI.test(stripComments('const a = "\u270F\uFE0F";'))).toBe(true);
    expect(EMOJI.test(stripComments('// \u{1F4F7} komentar\nconst a = "Foto"; /* \u{1F5BC} */'))).toBe(false);
    expect(EMOJI.test("Kotak · 3 foto — sisa ≥ 1 … ×")).toBe(false);
  });

  it.each(all.map((f) => [relative(WEB, f), f]))("%s tanpa emoji", (_name, f) => {
    const lines = stripComments(readFileSync(f, "utf8")).split("\n");
    const bad = lines.map((l, i) => [i + 1, l] as const).filter(([, l]) => EMOJI.test(l));
    expect(bad.map(([n, l]) => `${n}: ${l.trim()}`)).toEqual([]);
  });
});

describe("tanpa sumber ikon/font dari luar", () => {
  const ALLOWED = [/^https:\/\/challenges\.cloudflare\.com\//, /^http:\/\/www\.w3\.org\//];
  it.each(shipped.map((f) => [relative(WEB, f), f]))("%s hanya memakai URL yang diizinkan", (_name, f) => {
    const src = stripComments(readFileSync(f, "utf8"));
    const urls = [...src.matchAll(/https?:\/\/[^\s"'`)<>]+/g)].map((m) => m[0]);
    expect(urls.filter((u) => !ALLOWED.some((a) => a.test(u)))).toEqual([]);
    expect(src).not.toMatch(/@font-face|@import\s+url|fonts\.googleapis|use\.fontawesome|kit\.fontawesome|cdnjs|jsdelivr|unpkg/);
  });
});
