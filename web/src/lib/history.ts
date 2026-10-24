import { readJSON, writeJSON } from "./storage";

// Riwayat laporan di perangkat ini. Pada auth.mode = none, edit_token hanya ada di sini (tanpa
// itu laporan tidak bisa dibuka/diedit lagi); pada mode otp dicatat per pengguna.

const KEY = "pa.history";
const MAX = 100;

export interface HistoryEntry {
  id: string;
  createdAt: string;
  photos: number;
  boxes: number;
  /** Pemilik: id pengguna (mode otp) atau "anon". */
  owner: string;
  editToken?: string;
}

export const history = {
  list(owner: string): HistoryEntry[] {
    return readJSON<HistoryEntry[]>(KEY, []).filter((e) => e.owner === owner);
  },
  get(id: string, owner: string): HistoryEntry | undefined {
    return history.list(owner).find((e) => e.id === id);
  },
  add(e: HistoryEntry) {
    const all = readJSON<HistoryEntry[]>(KEY, []).filter((x) => x.id !== e.id);
    writeJSON(KEY, [e, ...all].slice(0, MAX));
  },
  update(id: string, p: Partial<Pick<HistoryEntry, "photos" | "boxes">>) {
    writeJSON(KEY, readJSON<HistoryEntry[]>(KEY, []).map((x) => (x.id === id ? { ...x, ...p } : x)));
  },
};
