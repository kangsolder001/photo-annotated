import type { AnnotationDTO } from "../api/types";
import { limits } from "../config";

// Model kotak anotasi. Koordinat ternormalisasi 0..1 terhadap foto hasil proses (spec §6.3), sama
// dengan yang disimpan API. Aturan validasi di sini harus sama dengan backend (domain/annotation.go).

export interface Box {
  /** Id lokal untuk React; tidak dikirim ke API. */
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
  text: string;
}

export type Corner = "nw" | "ne" | "sw" | "se";
export type BoxProblem = "too-small" | "empty-text" | "text-too-long" | "bad-char";

const clamp01 = (v: number) => Math.min(1, Math.max(0, v));

/** Kotak dari dua titik seret (urutan bebas), dipotong ke dalam foto. */
export function rectFromPoints(x0: number, y0: number, x1: number, y1: number): Pick<Box, "x" | "y" | "w" | "h"> {
  const ax = clamp01(Math.min(x0, x1));
  const ay = clamp01(Math.min(y0, y1));
  return { x: ax, y: ay, w: clamp01(Math.max(x0, x1)) - ax, h: clamp01(Math.max(y0, y1)) - ay };
}

export function isBigEnough(b: Pick<Box, "w" | "h">): boolean {
  return b.w >= limits.minBoxSize && b.h >= limits.minBoxSize;
}

/** Menggeser kotak tanpa keluar dari foto (ukuran tetap). */
export function moveBox(b: Box, dx: number, dy: number): Box {
  return { ...b, x: clamp01(Math.min(b.x + dx, 1 - b.w)), y: clamp01(Math.min(b.y + dy, 1 - b.h)) };
}

/** Mengubah ukuran dengan menarik satu sudut ke (px, py); sudut seberang tetap. */
export function resizeBox(b: Box, corner: Corner, px: number, py: number): Box {
  const left = corner === "nw" || corner === "sw";
  const top = corner === "nw" || corner === "ne";
  const fx = left ? b.x + b.w : b.x; // sudut tetap
  const fy = top ? b.y + b.h : b.y;
  const min = limits.minBoxSize;
  // jaga ukuran minimum agar kotak tidak terbalik atau lenyap saat diseret melewati sudut tetap
  const nx = left ? Math.min(clamp01(px), fx - min) : Math.max(clamp01(px), fx + min);
  const ny = top ? Math.min(clamp01(py), fy - min) : Math.max(clamp01(py), fy + min);
  return { ...b, ...rectFromPoints(fx, fy, clamp01(nx), clamp01(ny)) };
}

/** Teks seperti yang akan disimpan: baris baru dinormalkan, tab menjadi spasi, spasi tepi dibuang. */
export function normalizeText(t: string): string {
  return t.replace(/\r\n?/g, "\n").replace(/\t/g, " ").trim();
}

// Sama dengan backend (domain.NormalizeAnnotationText): karakter kontrol (Cc) selain baris baru dan
// karakter kontrol arah teks U+202A–202E, U+2066–2069 ditolak.
const BAD_CHARS = /[\u0000-\u0009\u000b-\u001f\u007f-\u009f‪-‮⁦-⁩]/;

export function boxProblems(b: Box): BoxProblem[] {
  const out: BoxProblem[] = [];
  if (!isBigEnough(b)) out.push("too-small");
  const t = normalizeText(b.text);
  if (t === "") out.push("empty-text");
  if ([...t].length > limits.maxTextLength) out.push("text-too-long");
  if (BAD_CHARS.test(t)) out.push("bad-char");
  return out;
}

export const PROBLEM_TEXT: Record<BoxProblem, string> = {
  "too-small": "Kotak terlalu kecil.",
  "empty-text": "Catatan wajib diisi.",
  "text-too-long": `Catatan maksimal ${limits.maxTextLength} karakter.`,
  "bad-char": "Catatan mengandung karakter yang tidak diizinkan.",
};

// pembulatan 6 desimal: cukup presisi (±0,002 px pada 1920 px) dan payload tetap kecil
const r6 = (v: number) => Math.round(v * 1e6) / 1e6;

export function toDTO(b: Box): AnnotationDTO {
  // pastikan x+w ≤ 1 setelah pembulatan (backend menolak kotak yang keluar foto)
  const x = r6(b.x), y = r6(b.y);
  return { text: normalizeText(b.text), x, y, w: Math.min(r6(b.w), r6(1 - x)), h: Math.min(r6(b.h), r6(1 - y)) };
}

export function fromDTO(a: AnnotationDTO): Box {
  return { id: crypto.randomUUID(), x: a.x, y: a.y, w: a.w, h: a.h, text: a.text };
}
