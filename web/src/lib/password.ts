// Aturan password sama dengan backend (domain.ValidatePassword): ≥ 8 karakter, ≤ 72 byte (batas
// bcrypt), tidak hanya spasi, tanpa karakter kontrol. Tanpa aturan komposisi (NIST SP 800-63B).

export const MIN_PASSWORD = 8;
const MAX_BYTES = 72;

export function passwordProblem(p: string): string | null {
  if ([...p].length < MIN_PASSWORD) return `Password minimal ${MIN_PASSWORD} karakter.`;
  if (new TextEncoder().encode(p).length > MAX_BYTES) return "Password terlalu panjang (maksimal 72 byte).";
  if (p.trim() === "") return "Password tidak boleh hanya spasi.";
  if (/[\u0000-\u001f\u007f-\u009f]/.test(p)) return "Password mengandung karakter yang tidak diizinkan.";
  return null;
}
