import { useEffect, useState } from "react";
import type { OTPStatusView } from "../../api/types";

const POLL_MS = 2000;

/**
 * Status pengiriman kode OTP, dipantau tiap 2 detik sampai final (sent/failed/expired). Dipakai
 * halaman kode user dan login admin (endpoint status masing-masing lewat `fetchStatus`).
 * `requestedAt` ikut menjadi kunci: kirim ulang dengan challenge yang sama memulai pantauan baru.
 */
export function useOtpStatus(challengeId: string | undefined, requestedAt: number | undefined, fetchStatus: (id: string) => Promise<OTPStatusView>) {
  const [status, setStatus] = useState<OTPStatusView>();
  useEffect(() => {
    setStatus(undefined);
    if (!challengeId) return;
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const s = await fetchStatus(challengeId);
        if (!alive) return;
        setStatus(s);
        if (s.status === "queued") timer = setTimeout(poll, POLL_MS);
      } catch {
        if (alive) timer = setTimeout(poll, POLL_MS * 2);
      }
    };
    void poll();
    return () => {
      alive = false;
      clearTimeout(timer);
    };
    // fetchStatus dianggap tetap (fungsi modul)
  }, [challengeId, requestedAt]);
  return status;
}

/** Waktu sekarang yang diperbarui tiap detik (hitung mundur kirim ulang). */
export function useNow(intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return now;
}

/** Sisa detik sebelum boleh kirim ulang. */
export function resendCooldown(requestedAt: number, resendAfterSec: number, now: number): number {
  return Math.max(0, Math.ceil((requestedAt + resendAfterSec * 1000 - now) / 1000));
}

export const OTP_CODE_LEN = 6;
