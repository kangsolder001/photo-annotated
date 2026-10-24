import type { AdminInfo } from "./types";

// Token admin di sessionStorage: hilang saat tab ditutup, terpisah dari token user (localStorage).

const KEY = "pa.admin.session";

interface Stored {
  token: string;
  expiresAt: string;
  admin: AdminInfo;
}

type Listener = () => void;
let loggedOut = false;
const listeners = new Set<Listener>();
const notify = () => listeners.forEach((l) => l());

function load(): Stored | null {
  try {
    const raw = sessionStorage.getItem(KEY);
    const s = raw ? (JSON.parse(raw) as Stored) : null;
    if (!s?.token || !(new Date(s.expiresAt).getTime() > Date.now())) return null;
    return s;
  } catch {
    return null;
  }
}

export const adminSession = {
  token: (): string | null => load()?.token ?? null,
  admin: (): AdminInfo | null => load()?.admin ?? null,
  save(token: string, expiresAt: string, admin: AdminInfo) {
    loggedOut = false;
    try {
      sessionStorage.setItem(KEY, JSON.stringify({ token, expiresAt, admin } satisfies Stored));
    } catch {
      // storage diblokir: login tidak bisa dipertahankan
    }
    notify();
  },
  /** Token ditolak (401) atau kedaluwarsa. */
  expire() {
    try {
      sessionStorage.removeItem(KEY);
    } catch {
      // abaikan
    }
    notify();
  },
  /** Keluar atas kemauan sendiri (tanpa ?next= saat kembali ke halaman masuk). */
  logout() {
    loggedOut = true;
    adminSession.expire();
  },
  loggedOut: () => loggedOut,
  subscribe(l: Listener): () => void {
    listeners.add(l);
    return () => listeners.delete(l);
  },
};
