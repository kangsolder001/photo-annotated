/** Sisa waktu dalam kalimat singkat, mis. "3 jam 12 menit". */
export function formatRemaining(ms: number): string {
  if (ms <= 0) return "habis";
  const min = Math.floor(ms / 60_000);
  if (min < 1) return "kurang dari 1 menit";
  const h = Math.floor(min / 60);
  const m = min % 60;
  if (h === 0) return `${m} menit`;
  return m === 0 ? `${h} jam` : `${h} jam ${m} menit`;
}

export const formatDateTime = (iso: string) =>
  new Intl.DateTimeFormat("id-ID", { dateStyle: "medium", timeStyle: "short" }).format(new Date(iso));
