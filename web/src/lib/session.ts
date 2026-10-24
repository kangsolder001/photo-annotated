import type { User } from "../api/types";
import { readJSON, remove, writeJSON } from "./storage";

// Sesi login (auth.mode = otp). Token disimpan di localStorage; CSP ketat dan render teks sebagai
// teks (bukan HTML) adalah pertahanan terhadap pencurian lewat XSS (spec §9.5).

const KEY = "pa.session";

interface Stored {
  token: string;
  expiresAt: string;
  user: User;
}

type Listener = () => void;
const listeners = new Set<Listener>();
const notify = () => listeners.forEach((l) => l());

function load(): Stored | null {
  const s = readJSON<Stored | null>(KEY, null);
  if (!s || !s.token || new Date(s.expiresAt).getTime() <= Date.now()) return null;
  return s;
}

export const session = {
  token: (): string | null => load()?.token ?? null,
  user: (): User | null => load()?.user ?? null,
  save(token: string, expiresAt: string, user: User) {
    writeJSON(KEY, { token, expiresAt, user } satisfies Stored);
    notify();
  },
  /** Logout atau token ditolak (401). */
  expire() {
    remove(KEY);
    notify();
  },
  subscribe(l: Listener): () => void {
    listeners.add(l);
    return () => listeners.delete(l);
  },
};
