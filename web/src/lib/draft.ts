import type { AnnotationDTO } from "../api/types";
import { readJSON, remove, writeJSON } from "./storage";

// Draf edit yang belum disimpan, per laporan. Hanya foto yang sudah menjadi bagian laporan (punya
// photo_id dan bisa ditampilkan ulang) yang disimpan; foto baru yang belum tersimpan tidak bisa
// dipulihkan setelah halaman dimuat ulang. Draf terikat ke `version`: bila laporan sudah berubah di
// tempat lain, draf lama dibuang agar tidak menimpa perubahan orang lain.

export interface Draft {
  version: number;
  savedAt: string;
  photos: { photoId: string; annotations: AnnotationDTO[] }[];
}

const key = (id: string, owner: string) => `pa.draft.${owner}.${id}`;

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
    return d;
  },
  clear(id: string, owner: string) {
    remove(key(id, owner));
  },
};

/** Sama persis dengan isi laporan tersimpan? (tidak perlu menawarkan pemulihan) */
export function sameAsSaved(d: Draft, saved: { photo_id: string; annotations: AnnotationDTO[] }[]): boolean {
  return JSON.stringify(d.photos.map((p) => [p.photoId, p.annotations])) ===
    JSON.stringify(saved.map((p) => [p.photo_id, p.annotations]));
}
