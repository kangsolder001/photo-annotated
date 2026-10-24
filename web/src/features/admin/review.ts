import type { ReviewStatus } from "./types";

/** Hari retensi bawaan (config retention.days). Dipakai hanya untuk peringatan di dialog. */
export const RETENTION_DAYS = 90;

/**
 * Membatalkan penerimaan submission yang sudah lewat masa retensi membuatnya terhapus pada putaran
 * cleanup berikutnya (spec-labels-admin §5.7): dashboard memperingatkan sebelum melakukannya.
 */
export function unacceptWouldDelete(current: ReviewStatus, next: ReviewStatus, createdAt: string, now = Date.now()): boolean {
  return current === "accepted" && next !== "accepted" && now - new Date(createdAt).getTime() > RETENTION_DAYS * 86_400_000;
}

/** Pintasan keyboard tidak aktif saat fokus di isian teks / dialog. */
export function typingTarget(t: EventTarget | null): boolean {
  if (!(t instanceof HTMLElement)) return false;
  return t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName) || !!t.closest("[role=dialog]");
}
