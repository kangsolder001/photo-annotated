// Konfigurasi build-time (VITE_*). Semua nilai ini publik: masuk ke bundle.

export type AuthMode = "none" | "otp";

function authMode(v: string | undefined): AuthMode {
  return v === "none" ? "none" : "otp";
}

export const config = {
  /** URL dasar API tanpa garis miring akhir; kosong = origin yang sama (proxy saat dev). */
  apiBase: (import.meta.env.VITE_API_BASE_URL ?? "").replace(/\/+$/, ""),
  /** Harus sama dengan auth.mode di config.json API. */
  authMode: authMode(import.meta.env.VITE_AUTH_MODE),
  /** Site key Turnstile; kosong = Turnstile tidak dipakai. */
  turnstileSiteKey: import.meta.env.VITE_TURNSTILE_SITE_KEY ?? "",
} as const;

/** Batas yang sama dengan API (config.json limits). */
export const limits = {
  maxPhotos: 5,
  maxBoxesPerPhoto: 50,
  maxTextLength: 1000,
  maxUploadBytes: 2 * 1024 * 1024,
  maxSide: 1920,
  minBoxSize: 0.005,
} as const;
