import { faCircleCheck } from "@fortawesome/free-solid-svg-icons/faCircleCheck";
import { faCircleXmark } from "@fortawesome/free-solid-svg-icons/faCircleXmark";
import { faTriangleExclamation } from "@fortawesome/free-solid-svg-icons/faTriangleExclamation";
import { faXmark } from "@fortawesome/free-solid-svg-icons/faXmark";
import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from "react";
import { Icon, IconButton } from "../../../components/Icon";

export type ToastKind = "ok" | "warn" | "error";
interface Toast { id: number; kind: ToastKind; text: string }

const Ctx = createContext<(kind: ToastKind, text: string) => void>(() => {});

const ICON = { ok: faCircleCheck, warn: faTriangleExclamation, error: faCircleXmark };

/** Notifikasi hasil aksi (hapus, review, blokir…), hilang sendiri setelah 6 detik. */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<Toast[]>([]);
  const seq = useRef(0);
  const dismiss = useCallback((id: number) => setItems((t) => t.filter((x) => x.id !== id)), []);
  const push = useCallback((kind: ToastKind, text: string) => {
    const id = ++seq.current;
    setItems((t) => [...t.slice(-3), { id, kind, text }]);
    setTimeout(() => dismiss(id), 6000);
  }, [dismiss]);
  const value = useMemo(() => push, [push]);
  return (
    <Ctx.Provider value={value}>
      {children}
      <div className="toasts" role="status" aria-live="polite">
        {items.map((t) => (
          <div key={t.id} className={`toast toast-${t.kind}`}>
            <Icon icon={ICON[t.kind]} />
            <span>{t.text}</span>
            <IconButton icon={faXmark} label="Tutup notifikasi" onClick={() => dismiss(t.id)} />
          </div>
        ))}
      </div>
    </Ctx.Provider>
  );
}

export const useToast = () => useContext(Ctx);
