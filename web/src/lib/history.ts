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
  /** Status review terakhir yang diketahui (diperbarui saat riwayat/laporan dibuka). */
  review?: { status: "pending" | "accepted" | "rejected"; reason: string | null };
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
  remove(id: string) {
    writeJSON(KEY, readJSON<HistoryEntry[]>(KEY, []).filter((x) => x.id !== id));
  },
  update(id: string, p: Partial<Pick<HistoryEntry, "photos" | "boxes" | "review">>) {
    writeJSON(KEY, readJSON<HistoryEntry[]>(KEY, []).map((x) => (x.id === id ? { ...x, ...p } : x)));
  },
};
