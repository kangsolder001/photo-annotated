import type { OTPStatusView } from "../../api/types";

// Teks status pengiriman kode untuk pengguna (spec §4.0 langkah 4).

export interface StatusText {
  kind: "info" | "ok" | "warn" | "error";
  text: string;
  /** Pengguna perlu meminta kode baru (kirim ulang langsung diizinkan). */
  needsResend: boolean;
}

export function describeStatus(s: OTPStatusView | undefined, estimatedWait: number): StatusText {
  switch (s?.status) {
    case "sent":
      return { kind: "ok", text: "Kode sudah dikirim ke WhatsApp Anda.", needsResend: false };
    case "failed":
      return s.failure_reason === "SEND_FAILED"
        ? { kind: "error", text: "Kode tidak bisa dikirim ke nomor ini. Pastikan nomor tersebut memakai WhatsApp, lalu coba lagi.", needsResend: true }
        : { kind: "error", text: "Layanan WhatsApp sedang bermasalah, kode belum terkirim. Coba kirim ulang beberapa saat lagi.", needsResend: true };
    case "expired":
    case "consumed":
      return { kind: "warn", text: "Kode ini sudah tidak berlaku. Minta kode baru.", needsResend: true };
    default:
      return {
        kind: "info",
        text: estimatedWait > 3 ? `Menunggu giliran pengiriman (sekitar ${estimatedWait} detik)…` : "Mengirim kode…",
        needsResend: false,
      };
  }
}
