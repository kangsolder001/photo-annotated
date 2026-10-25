import { faXmark } from "@fortawesome/free-solid-svg-icons/faXmark";
import { useEffect, useId, useRef, type ReactNode } from "react";
import { IconButton } from "./Icon";

/**
 * Dialog modal: di tengah layar pada desktop/tablet, sheet dari bawah di HP (CSS); `compact` tetap
 * kotak kecil di tengah (konfirmasi singkat).
 * Escape / tombol tutup / klik latar menutup (kecuali `busy`); fokus dipindah ke dalam dan
 * dikembalikan saat ditutup.
 */
export function Dialog(props: { title: string; onClose(): void; children: ReactNode; footer?: ReactNode; busy?: boolean; wide?: boolean; compact?: boolean }) {
  const titleId = useId();
  const box = useRef<HTMLDivElement>(null);
  const { onClose, busy } = props;

  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    const first = box.current?.querySelector<HTMLElement>("[data-autofocus], input, textarea, select, button:not([data-close])");
    (first ?? box.current)?.focus();
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = prevOverflow;
      prev?.focus?.();
    };
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !busy) {
        e.stopPropagation();
        onClose();
      }
      if (e.key === "Tab" && box.current) {
        // jebakan fokus sederhana
        const f = [...box.current.querySelectorAll<HTMLElement>("button, input, textarea, select, a[href], [tabindex]:not([tabindex='-1'])")].filter((x) => !x.hasAttribute("disabled"));
        if (!f.length) return;
        const first = f[0]!, last = f[f.length - 1]!;
        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first.focus();
        }
      }
    };
    document.addEventListener("keydown", onKey, true);
    return () => document.removeEventListener("keydown", onKey, true);
  }, [onClose, busy]);

  return (
    <div className={`dialog-backdrop${props.compact ? " compact" : ""}`} onPointerDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} className={`dialog${props.wide ? " dialog-wide" : ""}`} role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1}>
        <div className="dialog-head">
          <h2 id={titleId}>{props.title}</h2>
          <IconButton icon={faXmark} label="Tutup" onClick={onClose} disabled={busy} data-close />
        </div>
        <div className="dialog-body">{props.children}</div>
        {props.footer && <div className="dialog-foot">{props.footer}</div>}
      </div>
    </div>
  );
}
