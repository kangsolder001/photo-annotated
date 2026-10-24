import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/endpoints";
import type { Label } from "../../api/types";
import { errorMessage } from "../../lib/errors";

/**
 * Daftar label aktif (GET /labels). Dimuat saat layar anotasi dibuka, dan dimuat ulang lewat
 * `reload()` setelah server menolak label (INVALID_LABEL / LABEL_ARCHIVED).
 */
export function useLabels() {
  const [labels, setLabels] = useState<Label[]>();
  const [error, setError] = useState<string>();

  const reload = useCallback(async (): Promise<Label[] | undefined> => {
    setError(undefined);
    try {
      const l = await api.labels();
      setLabels(l);
      return l;
    } catch (e) {
      setError(errorMessage(e));
      return undefined;
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { labels, error, reload };
}
