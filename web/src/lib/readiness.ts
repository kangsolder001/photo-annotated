import { boxProblems, type Box } from "./annotation";

// Kapan laporan boleh dikirim: semua foto sudah terunggah dan semua kotak valid.

export interface PhotoState {
  status: "processing" | "uploading" | "uploaded" | "error";
  boxes: Box[];
}

export interface Readiness {
  ok: boolean;
  /** Alasan singkat untuk ditampilkan di samping tombol Kirim. */
  reason?: string;
  invalidBoxes: number;
}

export function readiness(photos: PhotoState[]): Readiness {
  const invalidBoxes = photos.reduce((n, p) => n + p.boxes.filter((b) => boxProblems(b).length > 0).length, 0);
  if (photos.length === 0) return { ok: false, reason: "Tambahkan minimal 1 foto.", invalidBoxes };
  if (photos.some((p) => p.status === "error")) return { ok: false, reason: "Ada foto yang gagal diunggah.", invalidBoxes };
  if (photos.some((p) => p.status !== "uploaded")) return { ok: false, reason: "Menunggu upload selesai…", invalidBoxes };
  if (invalidBoxes > 0) return { ok: false, reason: `${invalidBoxes} kotak belum lengkap.`, invalidBoxes };
  return { ok: true, invalidBoxes };
}
