import type { AdminOTPTicket } from "./types";

// Challenge login admin yang sedang berjalan (sessionStorage, terpisah dari OTP user), agar layar
// kode tetap berfungsi setelah reload saat admin berpindah ke WhatsApp.

export interface AdminPending {
  challengeId: string;
  requestedAt: number;
  resendAfter: number;
  estimatedWait: number;
  next: string;
}

const KEY = "pa.admin.otp";

export const adminPending = {
  get(): AdminPending | null {
    try {
      const raw = sessionStorage.getItem(KEY);
      return raw ? (JSON.parse(raw) as AdminPending) : null;
    } catch {
      return null;
    }
  },
  set(t: AdminOTPTicket, next: string): AdminPending {
    const p: AdminPending = {
      challengeId: t.challenge_id, requestedAt: Date.now(), resendAfter: t.resend_after_seconds,
      estimatedWait: t.estimated_wait_seconds, next,
    };
    try {
      sessionStorage.setItem(KEY, JSON.stringify(p));
    } catch {
      // state tetap di memori komponen
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

/** Tujuan setelah login admin: hanya path di bawah /admin. */
export function safeAdminNext(v: string | null): string {
  return v && v.startsWith("/admin") && !v.startsWith("/admin/masuk") && !v.includes("//") ? v : "/admin";
}
