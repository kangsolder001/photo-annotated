import type { OTPTicket } from "../../api/types";

// Challenge OTP yang sedang berjalan, disimpan di sessionStorage agar halaman kode tetap berfungsi
// setelah reload (mis. pengguna berpindah ke WhatsApp lalu kembali) tanpa bertahan selamanya.

export type OTPPurpose = "register" | "reset";

export interface PendingOTP {
  challengeId: string;
  /** register: verifikasi nomor saat daftar; reset: lupa password (butuh password baru). */
  purpose: OTPPurpose;
  requestedAt: number;
  resendAfter: number;
  estimatedWait: number;
  next: string;
}

const KEY = "pa.otp";

export const pending = {
  get(): PendingOTP | null {
    try {
      const raw = sessionStorage.getItem(KEY);
      return raw ? (JSON.parse(raw) as PendingOTP) : null;
    } catch {
      return null;
    }
  },
  set(t: OTPTicket, purpose: OTPPurpose, next: string) {
    const p: PendingOTP = {
      challengeId: t.challenge_id, purpose, requestedAt: Date.now(),
      resendAfter: t.resend_after_seconds, estimatedWait: t.estimated_wait_seconds, next,
    };
    try {
      sessionStorage.setItem(KEY, JSON.stringify(p));
    } catch {
      // abaikan: halaman kode tetap menerima state navigasi
    }
    return p;
  },
  clear() {
    try {
      sessionStorage.removeItem(KEY);
    } catch {
      // abaikan
    }
  },
};

/** Tujuan setelah login: hanya path internal, agar ?next= tidak bisa dipakai untuk redirect keluar. */
export function safeNext(v: string | null): string {
  return v && v.startsWith("/") && !v.startsWith("//") ? v : "/";
}
