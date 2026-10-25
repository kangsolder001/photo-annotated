import type { IconDefinition } from "@fortawesome/fontawesome-svg-core";
import { faEllipsisVertical } from "@fortawesome/free-solid-svg-icons/faEllipsisVertical";
import { useEffect, useId, useRef, useState } from "react";
import { Icon } from "../../../components/Icon";

export interface RowAction {
  label: string;
  icon: IconDefinition;
  onSelect(): void;
  danger?: boolean;
  disabled?: boolean;
}

/** Menu titik tiga untuk aksi baris (tampilan kartu di HP). */
export function RowMenu({ label, actions }: { label: string; actions: RowAction[] }) {
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const id = useId();
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
    <div className="row-menu" ref={box}>
      <button type="button" className="icon-btn" aria-label={label} title={label} aria-haspopup="menu" aria-expanded={open}
        aria-controls={open ? id : undefined} onClick={() => setOpen((v) => !v)}>
        <Icon icon={faEllipsisVertical} />
      </button>
      {open && (
        <div className="row-menu-list" role="menu" id={id} aria-label={label}>
          {actions.map((a) => (
            <button key={a.label} type="button" role="menuitem" className={a.danger ? "danger" : undefined} disabled={a.disabled}
              onClick={() => {
                setOpen(false);
                a.onSelect();
              }}>
              <Icon icon={a.icon} /> {a.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
