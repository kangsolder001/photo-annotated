import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/endpoints";

// URL lihat foto bertanda tangan berumur pendek (5 menit, spec §5.5). Bila gambar gagal dimuat
// karena URL kedaluwarsa, panggil refresh(photoId) untuk meminta URL baru.
export function usePhotoURLs(photoIds: string[], editToken?: string) {
  const [urls, setUrls] = useState<Record<string, string>>({});
  const key = photoIds.join(",");

  const load = useCallback(async (id: string) => {
    try {
      const r = await api.photoURL(id, editToken);
      setUrls((u) => ({ ...u, [id]: r.url }));
    } catch {
      // gambar akan tampil sebagai gagal dimuat; laporan tetap bisa dibaca/diedit
    }
  }, [editToken]);

  useEffect(() => {
    for (const id of key ? key.split(",") : []) void load(id);
  }, [key, load]);

  return { urls, refresh: load };
}
