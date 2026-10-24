import { useSyncExternalStore } from "react";

/** true bila media query cocok; tanpa matchMedia (test/jsdom) = false (tampilan desktop). */
export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    (cb) => {
      if (typeof window.matchMedia !== "function") return () => {};
      const m = window.matchMedia(query);
      m.addEventListener?.("change", cb);
      return () => m.removeEventListener?.("change", cb);
    },
    () => typeof window.matchMedia === "function" && window.matchMedia(query).matches,
    () => false,
  );
}

/** HP (< 768 px): tabel jadi kartu, dialog jadi sheet. */
export const useIsPhone = () => useMediaQuery("(max-width: 767px)");
