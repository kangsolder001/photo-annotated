import { useId, useState, type ReactNode } from "react";
import { Banner } from "../../../components/ui";
import { limits } from "../../../config";
import { normalizeText, textProblem, textProblemMessage } from "../../../lib/text";
import { adminErrorMessage } from "../errors";
import { Dialog } from "../../../components/Dialog";

/** Isian alasan wajib (tolak review, blokir annotator): 1–500 karakter, aturan teks sama dengan backend. */
export function ReasonDialog(props: {
  title: string;
  label: string;
  confirmLabel: ReactNode;
  danger?: boolean;
  children?: ReactNode;
  onConfirm(reason: string): Promise<void>;
  onClose(): void;
}) {
  const id = useId();
  const [reason, setReason] = useState("");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const problem = textProblem(reason, limits.maxReason);

  const run = async () => {
    setTouched(true);
    if (problem || busy) return;
    setBusy(true);
    setError(undefined);
    try {
      await props.onConfirm(normalizeText(reason));
    } catch (e) {
      setError(adminErrorMessage(e));
      setBusy(false);
    }
  };

  return (
    <Dialog title={props.title} onClose={props.onClose} busy={busy}
      footer={<>
        <button type="button" className="btn" onClick={props.onClose} disabled={busy}>Batal</button>
        <button type="button" className={`btn ${props.danger ? "btn-danger-solid" : "btn-primary"}`} onClick={() => void run()} disabled={busy}>
          {busy ? <><span className="spinner" aria-hidden="true" /> Menyimpan…</> : props.confirmLabel}
        </button>
      </>}>
      {props.children}
      <div className="field">
        <label htmlFor={id}>{props.label}</label>
        <textarea id={id} value={reason} onChange={(e) => setReason(e.target.value)} rows={3} maxLength={limits.maxReason * 2} data-autofocus
          aria-invalid={touched && !!problem} aria-describedby={`${id}-hint`} />
        <span id={`${id}-hint`} className={touched && problem ? "field-error" : "muted"}>
          {touched && problem ? textProblemMessage(problem, "Alasan", limits.maxReason) : `Wajib, maksimal ${limits.maxReason} karakter. Ditampilkan sebagai teks.`}
        </span>
      </div>
      {error && <Banner kind="error">{error}</Banner>}
    </Dialog>
  );
}
