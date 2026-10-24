import { config } from "../config";

// Cloudflare Turnstile (mode render eksplisit). Dipakai bila VITE_TURNSTILE_SITE_KEY diisi.

interface TurnstileAPI {
  render(el: HTMLElement, opts: Record<string, unknown>): string;
  reset(id: string): void;
  remove(id: string): void;
}

declare global {
  interface Window {
    turnstile?: TurnstileAPI;
  }
}

const SCRIPT = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";
let loading: Promise<TurnstileAPI> | null = null;

function load(): Promise<TurnstileAPI> {
  if (window.turnstile) return Promise.resolve(window.turnstile);
  loading ??= new Promise((resolve, reject) => {
    const s = document.createElement("script");
    s.src = SCRIPT;
    s.async = true;
    s.onload = () => (window.turnstile ? resolve(window.turnstile) : reject(new Error("turnstile tidak termuat")));
    s.onerror = () => {
      loading = null;
      reject(new Error("Verifikasi keamanan tidak bisa dimuat. Periksa koneksi lalu muat ulang halaman."));
    };
    document.head.appendChild(s);
  });
  return loading;
}

export const turnstileEnabled = () => config.turnstileSiteKey !== "";

/**
 * Memasang widget di el dan mengembalikan pengendalinya. `token()` menunggu token berikutnya;
 * `reset()` meminta token baru (token Turnstile sekali pakai di sisi Cloudflare).
 */
export async function mountTurnstile(el: HTMLElement): Promise<{ token(): Promise<string>; reset(): void; destroy(): void }> {
  const ts = await load();
  let current: string | null = null;
  let waiters: ((t: string) => void)[] = [];
  const id = ts.render(el, {
    sitekey: config.turnstileSiteKey,
    appearance: "interaction-only",
    callback: (t: string) => {
      current = t;
      waiters.forEach((w) => w(t));
      waiters = [];
    },
    "expired-callback": () => {
      current = null;
      ts.reset(id);
    },
  });
  return {
    token: () => (current ? Promise.resolve(current) : new Promise((resolve) => waiters.push(resolve))),
    reset: () => {
      current = null;
      ts.reset(id);
    },
    destroy: () => ts.remove(id),
  };
}
