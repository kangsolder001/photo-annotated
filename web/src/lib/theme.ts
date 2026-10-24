// Tema aplikasi (spec-labels-admin §7.4): Sistem (default, ikut perangkat), Terang (hangat), Gelap.
// Pilihan disimpan di localStorage `pa_theme`; tanpa storage (mode privat) tetap jalan dengan Sistem.
// Skrip inline di index.html menjalankan logika yang sama sebelum CSS dimuat (tanpa kedip); setelah
// itu modul ini yang memasang data-theme dan <meta name="theme-color"> saat pilihan/perangkat berubah.

export type ThemePref = "system" | "light" | "dark";
export type Theme = "light" | "dark";

export const THEME_KEY = "pa_theme";
/** Warna bar browser di HP = warna header (--surface) tiap tema. Sama dengan index.html. */
export const THEME_COLOR: Record<Theme, string> = { light: "#fcf6ec", dark: "#1b1f22" };

const DARK_QUERY = "(prefers-color-scheme: dark)";

export function readPref(): ThemePref {
  try {
    const v = localStorage.getItem(THEME_KEY);
    return v === "light" || v === "dark" ? v : "system";
  } catch {
    return "system";
  }
}

function systemDark(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia(DARK_QUERY).matches;
}

export function resolveTheme(pref: ThemePref, prefersDark: boolean): Theme {
  return pref === "system" ? (prefersDark ? "dark" : "light") : pref;
}

export function applyTheme(t: Theme): void {
  document.documentElement.setAttribute("data-theme", t);
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", THEME_COLOR[t]);
}

type Listener = () => void;
const listeners = new Set<Listener>();
let current: ThemePref | null = null;
let mql: MediaQueryList | null = null;
const onSystemChange = () => {
  if ((current ?? readPref()) === "system") applyTheme(resolveTheme("system", systemDark()));
};

export const theme = {
  /** Dipanggil sekali saat start: pasang tema (bila skrip inline gagal) dan ikuti perubahan perangkat. */
  init() {
    applyTheme(resolveTheme(theme.pref(), systemDark()));
    if (!mql && typeof window.matchMedia === "function") {
      mql = window.matchMedia(DARK_QUERY);
      mql.addEventListener?.("change", onSystemChange);
    }
  },
  pref(): ThemePref {
    return (current ??= readPref());
  },
  set(p: ThemePref) {
    current = p;
    try {
      localStorage.setItem(THEME_KEY, p);
    } catch {
      // storage diblokir: pilihan berlaku sampai halaman ditutup
    }
    applyTheme(resolveTheme(p, systemDark()));
    listeners.forEach((l) => l());
  },
  subscribe(l: Listener): () => void {
    listeners.add(l);
    return () => listeners.delete(l);
  },
};

export const THEME_LABEL: Record<ThemePref, string> = { system: "Sistem", light: "Terang", dark: "Gelap" };
