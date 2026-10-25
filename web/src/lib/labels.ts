import type { AnnotationView, Label } from "../api/types";

// Pilihan label untuk kotak (spec-labels-admin §4, §7.1).

export interface LabelOption {
  id: number;
  name: string;
  color: string;
  description?: string | null;
  /** Label arsip: tidak bisa dipilih untuk kotak baru, kecuali sudah ada di foto itu (§4.2). */
  archived?: boolean;
}

/** Warna kotak tanpa label (digambar putus-putus). Sama di kedua tema. */
export const UNLABELED_COLOR = "#8a8f98";

/**
 * Pilihan label untuk satu foto: label aktif, ditambah label arsip yang sudah dipakai kotak di versi
 * tersimpan foto itu (backend menerima label arsip saat edit hanya bila foto yang sama sudah punya
 * kotak berlabel itu sebelumnya).
 */
export function optionsForPhoto(active: Label[], saved: AnnotationView[] = []): LabelOption[] {
  const ids = new Set(active.map((l) => l.id));
  const extra = new Map<number, LabelOption>();
  for (const a of saved) {
    if (a.label && !ids.has(a.label.id)) extra.set(a.label.id, { ...a.label, archived: true });
  }
  return [...active, ...extra.values()];
}

/** Label yang dipakai di tampilan baca (dari isi submission, termasuk yang diarsipkan). */
export function optionsFromView(anns: AnnotationView[]): LabelOption[] {
  const m = new Map<number, LabelOption>();
  for (const a of anns) if (a.label) m.set(a.label.id, a.label);
  return [...m.values()];
}
