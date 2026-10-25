// Urutan daftar anotasi terakhir (filter + id), disimpan per tab, agar halaman detail bisa membuka
// submission berikutnya/sebelumnya (J/K) dan "Terima/Tolak lalu berikutnya" di antrean review.

const KEY = "pa.admin.list";

export interface ListContext {
  search: string;
  ids: string[];
  cursor: string | null;
}

export const listContext = {
  save(search: string, ids: string[], cursor: string | null) {
    try {
      sessionStorage.setItem(KEY, JSON.stringify({ search, ids, cursor } satisfies ListContext));
    } catch {
      // abaikan
    }
  },
  get(): ListContext | null {
    try {
      const raw = sessionStorage.getItem(KEY);
      return raw ? (JSON.parse(raw) as ListContext) : null;
    } catch {
      return null;
    }
  },
  /** Buang id (mis. setelah dihapus) agar navigasi tidak menunjuk ke data yang hilang. */
  drop(ids: string[]) {
    const c = listContext.get();
    if (c) listContext.save(c.search, c.ids.filter((x) => !ids.includes(x)), c.cursor);
  },
  neighbours(id: string): { prev?: string; next?: string } {
    const ids = listContext.get()?.ids ?? [];
    const i = ids.indexOf(id);
    if (i < 0) return {};
    return { prev: ids[i - 1], next: ids[i + 1] };
  },
};
