// Aturan teks bebas yang sama dengan backend (domain.NormalizeAnnotationText): dipakai untuk nama
// label (1–50), deskripsi label (≤ 200), alasan review/blokir (1–500).

/** Teks seperti yang akan disimpan: baris baru dinormalkan, tab menjadi spasi, spasi tepi dibuang. */
export function normalizeText(t: string): string {
  return t.replace(/\r\n?/g, "\n").replace(/\t/g, " ").trim();
}

// karakter kontrol (Cc) selain baris baru dan karakter kontrol arah teks U+202A–202E, U+2066–2069
const BAD_CHARS = /[\u0000-\u0009\u000b-\u001f\u007f-\u009f‪-‮⁦-⁩]/;

export type TextProblem = "empty" | "too-long" | "bad-char";

/** Masalah teks setelah dinormalkan; panjang dihitung per karakter (bukan unit UTF-16). */
export function textProblem(t: string, max: number, opts: { optional?: boolean } = {}): TextProblem | null {
  const n = normalizeText(t);
  if (n === "") return opts.optional ? null : "empty";
  if ([...n].length > max) return "too-long";
  if (BAD_CHARS.test(n)) return "bad-char";
  return null;
}

export function textProblemMessage(p: TextProblem, what: string, max: number): string {
  switch (p) {
    case "empty":
      return `${what} wajib diisi.`;
    case "too-long":
      return `${what} maksimal ${max} karakter.`;
    case "bad-char":
      return `${what} mengandung karakter yang tidak diizinkan.`;
  }
}
