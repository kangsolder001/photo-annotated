import type { AnnotationDTO, AnnotationView } from "../api/types";
import { limits } from "../config";
import { uuid } from "./id";

// Model kotak anotasi. Koordinat ternormalisasi 0..1 terhadap foto hasil proses (spec §6.3), sama
// dengan yang disimpan API. Aturan validasi di sini harus sama dengan backend (domain/annotation.go).
// Tiap kotak wajib memakai satu label dari daftar label aktif (spec-labels-admin §4.2).

export interface Box {
  /** Id lokal untuk React; tidak dikirim ke API. */
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
  /** Label terpilih; null = belum dipilih (opsi "Pilih label…"). */
  labelId: number | null;
  /** Catatan teks bebas dari data lama (sebelum label); hanya petunjuk, tidak dikirim. */
  legacyText?: string | null;
  /** Labelnya dikosongkan karena sudah tidak tersedia (diarsipkan admin); user diminta memilih ulang. */
  flagged?: boolean;
}

/** Kotak di draf lokal (localStorage). Draf lama berbentuk `{ text, … }` tanpa `label_id`. */
export interface DraftAnnotation {
  label_id: number | null;
  x: number;
  y: number;
  w: number;
  h: number;
  legacy_text?: string | null;
}

export type Corner = "nw" | "ne" | "sw" | "se";
export type BoxProblem = "too-small" | "no-label" | "label-unavailable";

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

export function boxProblems(b: Box): BoxProblem[] {
  const out: BoxProblem[] = [];
  if (!isBigEnough(b)) out.push("too-small");
  if (b.labelId == null) out.push(b.flagged ? "label-unavailable" : "no-label");
  return out;
}

export const PROBLEM_TEXT: Record<BoxProblem, string> = {
  "too-small": "Kotak terlalu kecil.",
  "no-label": "Pilih label.",
  "label-unavailable": "Label sebelumnya sudah tidak tersedia. Pilih label lain.",
};

// pembulatan 6 desimal: cukup presisi (±0,002 px pada 1920 px) dan payload tetap kecil
const r6 = (v: number) => Math.round(v * 1e6) / 1e6;

function rounded(b: Pick<Box, "x" | "y" | "w" | "h">) {
  // pastikan x+w ≤ 1 setelah pembulatan (backend menolak kotak yang keluar foto)
  const x = r6(b.x), y = r6(b.y);
  return { x, y, w: Math.min(r6(b.w), r6(1 - x)), h: Math.min(r6(b.h), r6(1 - y)) };
}

/** Bentuk kirim (POST/PUT). Hanya dipanggil bila semua kotak sudah berlabel (readiness). */
export function toDTO(b: Box): AnnotationDTO {
  if (b.labelId == null) throw new Error("kotak tanpa label");
  return { label_id: b.labelId, ...rounded(b) };
}

/** Bentuk draf: label boleh kosong, catatan lama ikut agar tetap tampil sebagai petunjuk. */
export function toDraft(b: Box): DraftAnnotation {
  return { label_id: b.labelId, ...rounded(b), ...(b.legacyText ? { legacy_text: b.legacyText } : {}) };
}

export function fromView(a: AnnotationView): Box {
  return { id: uuid(), x: a.x, y: a.y, w: a.w, h: a.h, labelId: a.label?.id ?? null, legacyText: a.label ? null : a.text };
}

export function fromDraft(a: DraftAnnotation): Box {
  return { id: uuid(), x: a.x, y: a.y, w: a.w, h: a.h, labelId: a.label_id, legacyText: a.legacy_text ?? null };
}

/**
 * Kosongkan label yang tidak boleh dipakai lagi (diarsipkan/dihapus admin) dan tandai kotaknya,
 * agar user memilih ulang. Mengembalikan array yang sama bila tidak ada perubahan.
 */
export function clearUnavailable(boxes: Box[], allowed: (labelId: number) => boolean): Box[] {
  let changed = false;
  const out = boxes.map((b) => {
    if (b.labelId == null || allowed(b.labelId)) return b;
    changed = true;
    return { ...b, labelId: null, flagged: true };
  });
  return changed ? out : boxes;
}
