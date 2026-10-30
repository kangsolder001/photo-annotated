import type { ReviewStatus, SubmissionQuery } from "./types";

// Filter daftar anotasi disimpan di URL (?from=2026-10-01&label_id=3…), sehingga tautan seperti
// "semua dari annotator ini" dan "antrean review" bisa dibagikan/di-bookmark.

export interface Filters {
  /** Tanggal lokal YYYY-MM-DD (inklusif). */
  from?: string;
  /** Tanggal lokal YYYY-MM-DD (inklusif; dikirim sebagai awal hari berikutnya, eksklusif). */
  to?: string;
  label_id?: number;
  user_id?: string;
  q?: string;
  unlabeled?: boolean;
  review_status?: ReviewStatus;
  /** Pendataan: bersedia/menolak, RW, kelompok (spec-pendataan §5.6). */
  consent?: "yes" | "no";
  rw?: string;
  group_id?: number;
  sort?: "newest" | "oldest";
}

const DATE = /^\d{4}-\d{2}-\d{2}$/;
const REVIEW = new Set(["pending", "accepted", "rejected"]);

export function parseFilters(sp: URLSearchParams): Filters {
  const f: Filters = {};
  const from = sp.get("from"), to = sp.get("to");
  if (from && DATE.test(from)) f.from = from;
  if (to && DATE.test(to)) f.to = to;
  const label = Number(sp.get("label_id"));
  if (sp.get("label_id") && Number.isInteger(label) && label > 0) f.label_id = label;
  const user = sp.get("user_id");
  if (user) f.user_id = user;
  const q = sp.get("q")?.trim();
  if (q) f.q = q;
  if (sp.get("unlabeled") === "true") f.unlabeled = true;
  const rs = sp.get("review_status");
  if (rs && REVIEW.has(rs)) f.review_status = rs as ReviewStatus;
  const consent = sp.get("consent");
  if (consent === "yes" || consent === "no") f.consent = consent;
  const rw = sp.get("rw")?.trim();
  if (rw) f.rw = rw;
  const group = Number(sp.get("group_id"));
  if (sp.get("group_id") && Number.isInteger(group) && group > 0) f.group_id = group;
  if (sp.get("sort") === "oldest") f.sort = "oldest";
  return f;
}

export function filtersToParams(f: Filters): URLSearchParams {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(f)) {
    if (v === undefined || v === "" || v === false) continue;
    if (k === "sort" && v === "newest") continue;
    p.set(k, String(v));
  }
  return p;
}

/** Awal hari lokal sebagai RFC 3339 (UTC). */
export function dayStart(date: string): string {
  const [y, m, d] = date.split("-").map(Number) as [number, number, number];
  return new Date(y, m - 1, d).toISOString();
}

/** Awal hari berikutnya (batas `to` eksklusif untuk tanggal inklusif). */
export function dayAfter(date: string): string {
  const [y, m, d] = date.split("-").map(Number) as [number, number, number];
  return new Date(y, m - 1, d + 1).toISOString();
}

export function toQuery(f: Filters): SubmissionQuery {
  return {
    from: f.from ? dayStart(f.from) : undefined,
    to: f.to ? dayAfter(f.to) : undefined,
    label_id: f.label_id,
    user_id: f.user_id,
    q: f.q,
    unlabeled: f.unlabeled || undefined,
    review_status: f.review_status,
    consent: f.consent,
    rw: f.rw,
    group_id: f.group_id,
    sort: f.sort,
  };
}

/** Nama berkas export: photo-annotated-<from>-<to>.jsonl ("awal"/"sekarang" bila kosong). */
export function exportFilename(f: Pick<Filters, "from" | "to">): string {
  return `photo-annotated-${f.from ?? "awal"}-${f.to ?? "sekarang"}.jsonl`;
}

/** Nama berkas export CSV pendataan: pendataan-<from>-<to>.csv. */
export function csvFilename(f: Pick<Filters, "from" | "to">): string {
  return `pendataan-${f.from ?? "awal"}-${f.to ?? "sekarang"}.csv`;
}

/** Hari tersisa sampai `iso` (dibulatkan ke atas; 0 bila sudah lewat). */
export function daysUntil(iso: string, now = Date.now()): number {
  return Math.max(0, Math.ceil((new Date(iso).getTime() - now) / 86_400_000));
}
