import type { IconDefinition } from "@fortawesome/fontawesome-svg-core";
import { faCircleHalfStroke } from "@fortawesome/free-solid-svg-icons/faCircleHalfStroke";
import { faMoon } from "@fortawesome/free-solid-svg-icons/faMoon";
import { faSun } from "@fortawesome/free-solid-svg-icons/faSun";
import { useEffect, useId, useRef, useState, useSyncExternalStore } from "react";
import { theme, THEME_LABEL, type ThemePref } from "../lib/theme";
import { Icon } from "./Icon";

const OPTIONS: ThemePref[] = ["system", "light", "dark"];
const ICONS: Record<ThemePref, IconDefinition> = { system: faCircleHalfStroke, light: faSun, dark: faMoon };

export function useThemePref(): ThemePref {
  return useSyncExternalStore(theme.subscribe, theme.pref, theme.pref);
}

/**
 * Pilihan tema Sistem / Terang / Gelap.
 *  - `menu`: satu tombol ikon (header aplikasi user yang sempit) yang membuka daftar pilihan.
 *  - `inline`: tiga tombol berdampingan (sidebar/menu dashboard).
 */
export function ThemeSwitcher({ variant = "menu" }: { variant?: "menu" | "inline" }) {
  const pref = useThemePref();
  if (variant === "inline") {
    return (
      <div className="theme-inline" role="radiogroup" aria-label="Tema">
        {OPTIONS.map((o) => (
          <button key={o} type="button" role="radio" aria-checked={pref === o} onClick={() => theme.set(o)} title={`Tema ${THEME_LABEL[o]}`}>
            <Icon icon={ICONS[o]} /> <span>{THEME_LABEL[o]}</span>
          </button>
        ))}
      </div>
    );
  }
  return <ThemeMenu pref={pref} />;
}

function ThemeMenu({ pref }: { pref: ThemePref }) {
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const menuId = useId();
  const label = `Tema: ${THEME_LABEL[pref]}`;

  useEffect(() => {
    if (!open) return;
    const close = (e: Event) => {
      if (e instanceof KeyboardEvent ? e.key === "Escape" : !box.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", close);
    document.addEventListener("keydown", close);
    return () => {
      document.removeEventListener("pointerdown", close);
      document.removeEventListener("keydown", close);
    };
  }, [open]);

  return (
    <div className="theme-menu" ref={box}>
      <button
        type="button"
        className="icon-btn"
        aria-label={label}
        title={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        onClick={() => setOpen((v) => !v)}
      >
        <Icon icon={ICONS[pref]} />
      </button>
      {open && (
        <div className="theme-menu-list" role="menu" id={menuId} aria-label="Tema">
          {OPTIONS.map((o) => (
            <button
              key={o}
              type="button"
              role="menuitemradio"
              aria-checked={pref === o}
              onClick={() => {
                theme.set(o);
                setOpen(false);
              }}
            >
              <Icon icon={ICONS[o]} /> {THEME_LABEL[o]}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
