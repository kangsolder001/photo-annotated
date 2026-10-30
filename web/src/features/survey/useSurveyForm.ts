import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/endpoints";
import type { SurveyForm } from "../../api/types";
import { errorMessage } from "../../lib/errors";

// Definisi instrumen jarang berubah (berubah = deploy): dimuat sekali per pemuatan halaman.
let cached: SurveyForm | undefined;

/** Untuk test: lupakan definisi yang tersimpan. */
export function resetSurveyFormCache() {
  cached = undefined;
}

/** Definisi instrumen dari GET /survey/form, dengan `reload()` bila gagal. */
export function useSurveyForm() {
  const [form, setForm] = useState<SurveyForm | undefined>(cached);
  const [error, setError] = useState<string>();

  const reload = useCallback(async () => {
    setError(undefined);
    try {
      const f = await api.getSurveyForm();
      cached = f;
      setForm(f);
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    if (!cached) void reload();
  }, [reload]);

  return { form, error, reload };
}
