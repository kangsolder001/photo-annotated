import type { AnswerValue, Answers, SurveyForm, SurveyQuestion, SurveySection } from "../api/types";
import { normalizeText, textProblem } from "./text";

// Logika murni pendataan (spec-pendataan §4). Definisi pertanyaan datang dari server (GET /survey/form);
// aturan di sini harus sama dengan backend (internal/domain/survey/validate.go) agar kiriman tidak ditolak.

/** Id pertanyaan persetujuan dan bagian identitas (§4.2). */
export const CONSENT_ID = "a_consent";
export const IDENTITY_SECTION = "A";
/** Panjang maksimal `textarea` dan teks opsi "Lainnya" (§4.1). */
export const TEXTAREA_MAX = 1000;
export const OTHER_TEXT_MAX = 100;

/**
 * Kunci jawaban untuk teks opsi "Lainnya" pada `multi`, mis. `r3_container_types_other`. Spec hanya
 * menyebut "opsi other wajib disertai teks"; tempat teksnya dipilih di FE sebagai kunci terpisah
 * (lihat Catatan T66 di plan-pendataan.md).
 */
export const otherKey = (questionId: string) => `${questionId}_other`;

export const isConsented = (answers: Answers) => answers[CONSENT_ID] === "yes";
export const isDeclined = (answers: Answers) => answers[CONSENT_ID] === "no";

/** Bagian yang berlaku: menolak → hanya bagian A; selain itu semua bagian. */
export function visibleSections(form: SurveyForm, answers: Answers): SurveySection[] {
  return isDeclined(answers) ? form.sections.filter((s) => s.id === IDENTITY_SECTION) : form.sections;
}

/** Tanggal hari ini di zona Asia/Jakarta (`YYYY-MM-DD`), acuan "tidak boleh di masa depan". */
export function todayJakarta(now: Date = new Date()): string {
  // en-CA memformat tanggal sebagai YYYY-MM-DD
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Jakarta", year: "numeric", month: "2-digit", day: "2-digit" }).format(now);
}

/** Jawaban kosong: tidak ada, string kosong (setelah trim), atau array kosong. */
export function isEmpty(v: AnswerValue | undefined): boolean {
  if (v === undefined || v === null) return true;
  if (typeof v === "string") return v.trim() === "";
  if (Array.isArray(v)) return v.length === 0;
  return false;
}

function validDate(s: string): boolean {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s);
  if (!m) return false;
  const [y, mo, d] = [Number(m[1]), Number(m[2]), Number(m[3])];
  const dt = new Date(Date.UTC(y, mo - 1, d));
  return dt.getUTCFullYear() === y && dt.getUTCMonth() === mo - 1 && dt.getUTCDate() === d;
}

/**
 * Masalah satu jawaban (aturan §4.1) atau null bila sah. Jawaban kosong hanya salah bila wajib.
 * `answers` dipakai untuk teks "Lainnya" pada `multi`.
 */
export function validateAnswer(q: SurveyQuestion, value: AnswerValue | undefined, answers: Answers = {}, today = todayJakarta()): string | null {
  if (isEmpty(value)) return q.required ? "Wajib diisi." : null;
  const opts = q.options ?? [];
  switch (q.type) {
    case "date":
      if (typeof value !== "string" || !validDate(value)) return "Tanggal tidak valid.";
      if (value > today) return "Tanggal tidak boleh di masa depan.";
      return null;
    case "text":
    case "textarea": {
      if (typeof value !== "string") return "Jawaban tidak valid.";
      const max = q.type === "textarea" ? TEXTAREA_MAX : (q.max ?? TEXTAREA_MAX);
      // textarea boleh baris baru; text tidak (baris baru termasuk karakter kontrol)
      if (q.type === "text" && /[\r\n]/.test(value)) return "Tidak boleh berisi baris baru.";
      const p = textProblem(value, max, { optional: true });
      if (p === "too-long") return `Maksimal ${max} karakter.`;
      if (p === "bad-char") return "Mengandung karakter yang tidak diizinkan.";
      return null;
    }
    case "int": {
      if (typeof value !== "number" || !Number.isInteger(value)) return "Isi dengan bilangan bulat.";
      if (q.min != null && value < q.min) return `Minimal ${q.min}.`;
      if (q.max != null && value > q.max) return `Maksimal ${q.max}.`;
      return null;
    }
    case "choice":
      if (typeof value !== "string" || !opts.some((o) => o.id === value)) return "Pilihan tidak valid.";
      return null;
    case "multi": {
      if (!Array.isArray(value) || value.some((v) => !opts.some((o) => o.id === v))) return "Pilihan tidak valid.";
      if (new Set(value).size !== value.length) return "Pilihan ganda tidak boleh berulang.";
      if (opts.some((o) => o.other && value.includes(o.id))) {
        const t = answers[otherKey(q.id)];
        const p = typeof t === "string" ? textProblem(t, OTHER_TEXT_MAX) : "empty";
        if (p === "empty") return "Tuliskan jenis lainnya.";
        if (p === "too-long") return `Keterangan lainnya maksimal ${OTHER_TEXT_MAX} karakter.`;
        if (p === "bad-char") return "Keterangan lainnya mengandung karakter yang tidak diizinkan.";
      }
      return null;
    }
  }
}

/** Masalah per pertanyaan dalam satu bagian (id → pesan); kosong = bagian sah. */
export function sectionErrors(section: SurveySection, answers: Answers, today = todayJakarta()): Record<string, string> {
  const out: Record<string, string> = {};
  for (const q of section.questions) {
    const p = validateAnswer(q, answers[q.id], answers, today);
    if (p) out[q.id] = p;
  }
  return out;
}

/** Id pertanyaan wajib yang belum diisi pada bagian yang berlaku (urut definisi). */
export function missingRequired(form: SurveyForm, answers: Answers): string[] {
  return visibleSections(form, answers).flatMap((s) => s.questions.filter((q) => q.required && isEmpty(answers[q.id])).map((q) => q.id));
}

/** Bagian yang berlaku dan masih punya masalah (wajib kosong atau jawaban tidak sah). */
export function incompleteSections(form: SurveyForm, answers: Answers, today = todayJakarta()): SurveySection[] {
  return visibleSections(form, answers).filter((s) => Object.keys(sectionErrors(s, answers, today)).length > 0);
}

/**
 * Jawaban siap kirim: hanya bagian yang berlaku (menolak → bagian A saja, §4.3), tanpa jawaban kosong,
 * teks di-trim, teks "Lainnya" hanya bila opsi lainnya dipilih.
 */
export function cleanAnswers(form: SurveyForm, answers: Answers): Answers {
  const out: Answers = {};
  for (const s of visibleSections(form, answers)) {
    for (const q of s.questions) {
      const v = answers[q.id];
      if (isEmpty(v)) continue;
      out[q.id] = typeof v === "string" ? normalizeText(v) : v!;
      if (q.type === "multi" && Array.isArray(v) && (q.options ?? []).some((o) => o.other && v.includes(o.id))) {
        const t = answers[otherKey(q.id)];
        if (typeof t === "string" && !isEmpty(t)) out[otherKey(q.id)] = normalizeText(t);
      }
    }
  }
  return out;
}

/** Teks opsi untuk ditampilkan (riwayat, ringkasan, admin); id tak dikenal ditampilkan apa adanya. */
export function answerText(q: SurveyQuestion, v: AnswerValue | undefined, answers: Answers = {}): string {
  if (isEmpty(v)) return "";
  const label = (id: string) => q.options?.find((o) => o.id === id)?.label ?? id;
  if (q.type === "choice" && typeof v === "string") return label(v);
  if (q.type === "multi" && Array.isArray(v)) {
    return v
      .map((id) => {
        const o = q.options?.find((x) => x.id === id);
        const t = answers[otherKey(q.id)];
        return o?.other && typeof t === "string" && t.trim() ? `${o.label}: ${t.trim()}` : label(id);
      })
      .join("; ");
  }
  return String(v);
}
