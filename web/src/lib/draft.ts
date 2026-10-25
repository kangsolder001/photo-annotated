import type { AnnotationView } from "../api/types";
import type { DraftAnnotation } from "./annotation";
import { readJSON, remove, writeJSON } from "./storage";

// Draf edit yang belum disimpan, per laporan. Hanya foto yang sudah menjadi bagian laporan (punya
// photo_id dan bisa ditampilkan ulang) yang disimpan; foto baru yang belum tersimpan tidak bisa
// dipulihkan setelah halaman dimuat ulang. Draf terikat ke `version`: bila laporan sudah berubah di
// tempat lain, draf lama dibuang agar tidak menimpa perubahan orang lain.
// Draf menyimpan `label_id`. Draf dari sebelum label (`{ text, … }`) tetap dipulihkan: kotaknya
// dipertahankan, labelnya kosong, dan teksnya ikut sebagai "catatan lama" (spec-labels-admin §7.1).

export interface Draft {
  version: number;
  savedAt: string;
  photos: { photoId: string; annotations: DraftAnnotation[] }[];
}

const key = (id: string, owner: string) => `pa.draft.${owner}.${id}`;

const num = (v: unknown) => (typeof v === "number" && Number.isFinite(v) ? v : null);

/** Satu kotak draf dari format apa pun (baru atau lama); null bila rusak. */
export function migrateAnnotation(raw: unknown): DraftAnnotation | null {
  if (!raw || typeof raw !== "object") return null;
  const a = raw as Record<string, unknown>;
  const x = num(a.x), y = num(a.y), w = num(a.w), h = num(a.h);
  if (x == null || y == null || w == null || h == null) return null;
  const labelId = typeof a.label_id === "number" && Number.isInteger(a.label_id) ? a.label_id : null;
  const legacy = typeof a.legacy_text === "string" ? a.legacy_text : typeof a.text === "string" && a.text.trim() ? a.text : null;
  return { label_id: labelId, x, y, w, h, ...(legacy ? { legacy_text: legacy } : {}) };
}

export const drafts = {
  save(id: string, owner: string, d: Draft) {
    writeJSON(key(id, owner), d);
  },
  /** Draf untuk versi ini, atau null (tidak ada / sudah basi — yang basi sekalian dihapus). */
  load(id: string, owner: string, version: number): Draft | null {
    const d = readJSON<Draft | null>(key(id, owner), null);
    if (!d) return null;
    if (d.version !== version || !Array.isArray(d.photos)) {
      remove(key(id, owner));
      return null;
    }
    return {
      ...d,
      photos: d.photos.map((p) => ({
        photoId: p.photoId,
        annotations: (Array.isArray(p.annotations) ? p.annotations : []).map(migrateAnnotation).filter((a): a is DraftAnnotation => a !== null),
      })),
    };
  },
  clear(id: string, owner: string) {
    remove(key(id, owner));
  },
};

/** Kotak tersimpan dalam bentuk draf (untuk dibandingkan). */
export function viewToDraft(a: AnnotationView): DraftAnnotation {
  return { label_id: a.label?.id ?? null, x: a.x, y: a.y, w: a.w, h: a.h, ...(!a.label && a.text ? { legacy_text: a.text } : {}) };
}

const shape = (a: DraftAnnotation) => [a.label_id, a.x, a.y, a.w, a.h];

/** Sama persis dengan isi laporan tersimpan? (tidak perlu menawarkan pemulihan) */
export function sameAsSaved(d: Draft, saved: { photo_id: string; annotations: AnnotationView[] }[]): boolean {
  return JSON.stringify(d.photos.map((p) => [p.photoId, p.annotations.map(shape)])) ===
    JSON.stringify(saved.map((p) => [p.photo_id, p.annotations.map(viewToDraft).map(shape)]));
}
