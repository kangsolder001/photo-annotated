import { faTrashCan } from "@fortawesome/free-solid-svg-icons";
import { useId, useState, type ReactNode } from "react";
import { Icon } from "../../../components/Icon";
import { Banner } from "../../../components/ui";
import { adminErrorMessage } from "../errors";
import { Dialog } from "./Dialog";

export const CONFIRM_WORD = "HAPUS";

/**
 * Konfirmasi hapus permanen (spec-labels-admin §7.2): menyebut apa yang akan hilang, menegaskan
 * permanen, dan tombol bahaya baru aktif setelah admin mengetik HAPUS.
 */
export function ConfirmDelete(props: {
  title: string;
  /** Apa yang hilang, mis. "3 foto, 7 kotak anotasi milik Budi Santoso". */
  what: ReactNode;
  confirmLabel: string;
  children?: ReactNode;
  onConfirm(): Promise<void>;
  onClose(): void;
}) {
  const id = useId();
  const [word, setWord] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const ok = word.trim() === CONFIRM_WORD;

  const run = async () => {
    if (!ok || busy) return;
    setBusy(true);
    setError(undefined);
    try {
      await props.onConfirm();
    } catch (e) {
      setError(adminErrorMessage(e));
      setBusy(false);
    }
  };

  return (
    <Dialog title={props.title} onClose={props.onClose} busy={busy}
      footer={<>
        <button type="button" className="btn" onClick={props.onClose} disabled={busy}>Batal</button>
        <button type="button" className="btn btn-danger-solid" onClick={() => void run()} disabled={!ok || busy}>
          {busy ? <><span className="spinner" aria-hidden="true" /> Menghapus…</> : <><Icon icon={faTrashCan} /> {props.confirmLabel}</>}
        </button>
      </>}>
      <p>Akan dihapus: <strong>{props.what}</strong>.</p>
      <Banner kind="error">Tindakan ini <strong>permanen dan tidak bisa dibatalkan</strong>. Data dan file foto dihapus dari database dan SSD.</Banner>
      {props.children}
      <div className="field">
        <label htmlFor={id}>Ketik <strong>{CONFIRM_WORD}</strong> untuk melanjutkan</label>
        <input id={id} value={word} onChange={(e) => setWord(e.target.value)} autoComplete="off" autoCapitalize="characters" spellCheck={false}
          onKeyDown={(e) => e.key === "Enter" && void run()} data-autofocus />
      </div>
      {error && <Banner kind="error">{error}</Banner>}
    </Dialog>
  );
}

/** Pesan hasil hapus (cleanup_pending = data sudah tersembunyi, sebagian file menyusul). */
export function deleteResultText(status: "deleted" | "cleanup_pending", what: string): string {
  return status === "cleanup_pending" ? `${what} dihapus; sebagian file dibersihkan otomatis.` : `${what} dihapus permanen.`;
}
