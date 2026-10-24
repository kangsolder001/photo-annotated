import { faArrowLeft, faArrowRight, faCheck, faRotateLeft, faXmark } from "@fortawesome/free-solid-svg-icons";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Icon } from "../../components/Icon";
import { ReviewBadge } from "../../components/ReviewBadge";
import { Banner } from "../../components/ui";
import { isCode } from "../../lib/errors";
import { formatDateTime } from "../../lib/time";
import { adminApi } from "./api";
import { adminErrorMessage } from "./errors";
import { listContext } from "./listContext";
import { typingTarget, unacceptWouldDelete } from "./review";
import type { AdminSubmission, ReviewStatus } from "./types";
import { Dialog } from "./ui/Dialog";
import { ReasonDialog } from "./ui/ReasonDialog";
import { useToast } from "./ui/Toasts";
import { useIsPhone } from "./ui/useMediaQuery";

const DONE: Record<ReviewStatus, string> = { accepted: "Submission diterima.", rejected: "Submission ditolak.", pending: "Review dibatalkan." };

/**
 * Panel review di detail (spec-labels-admin §5.7, §7.2): Terima / Tolak (alasan wajib) / Batalkan,
 * plus "lalu berikutnya" untuk antrean. Pintasan: A terima, R tolak, J/K berikutnya/sebelumnya.
 * Di HP tombol menempel di bawah layar.
 */
export function ReviewPanel({ sub, onUpdated, onReload }: { sub: AdminSubmission; onUpdated(s: AdminSubmission): void; onReload(): void }) {
  const navigate = useNavigate();
  const toast = useToast();
  const isPhone = useIsPhone();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<{ text: string; conflict?: boolean }>();
  const [rejecting, setRejecting] = useState<{ next: boolean } | null>(null);
  const [warn, setWarn] = useState<{ status: ReviewStatus; reason: string | null; next: boolean } | null>(null);
  const { prev, next } = listContext.neighbours(sub.id);
  const status = sub.review.status;

  const go = (id?: string) => id && navigate(`/admin/anotasi/${id}`);

  const send = async (s: ReviewStatus, reason: string | null, thenNext: boolean, confirmed = false) => {
    if (busy) return;
    if (!confirmed && unacceptWouldDelete(status, s, sub.created_at)) {
      setWarn({ status: s, reason, next: thenNext });
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      const r = await adminApi.review(sub.id, { status: s, reason, version: sub.version });
      toast("ok", DONE[s]);
      setWarn(null);
      setRejecting(null);
      if (thenNext && next) go(next);
      else onUpdated({ ...sub, review: r.review, retention_deletes_at: r.retention_deletes_at });
    } catch (e) {
      setWarn(null);
      setRejecting(null);
      if (isCode(e, "VERSION_CONFLICT")) setError({ text: adminErrorMessage(e), conflict: true });
      else setError({ text: adminErrorMessage(e) });
    } finally {
      setBusy(false);
    }
  };

  // pintasan keyboard (tidak aktif saat mengetik atau saat dialog terbuka)
  const keyRef = useRef<(e: KeyboardEvent) => void>(() => {});
  keyRef.current = (e: KeyboardEvent) => {
    if (e.ctrlKey || e.metaKey || e.altKey || typingTarget(e.target) || rejecting || warn || busy) return;
    const k = e.key.toLowerCase();
    if (k === "a" && status !== "accepted") {
      e.preventDefault();
      void send("accepted", null, false);
    } else if (k === "r") {
      e.preventDefault();
      setRejecting({ next: false });
    } else if (k === "j" && next) go(next);
    else if (k === "k" && prev) go(prev);
  };
  useEffect(() => {
    const h = (e: KeyboardEvent) => keyRef.current(e);
    document.addEventListener("keydown", h);
    return () => document.removeEventListener("keydown", h);
  }, []);

  return (
    <div className="card stack review-panel">
      <h2>Review</h2>
      <div className="row">
        <ReviewBadge status={status} />
        {sub.review.reviewed_at && <span className="muted">{formatDateTime(sub.review.reviewed_at)}</span>}
      </div>
      {sub.review.reason && <p className="review-reason">Alasan: {sub.review.reason}</p>}
      {error && (
        <Banner kind={error.conflict ? "warn" : "error"}>
          {error.text}{" "}
          {error.conflict && <button type="button" className="btn btn-small" onClick={() => { setError(undefined); onReload(); }}>Muat ulang</button>}
        </Banner>
      )}
      <div className={`review-actions${isPhone ? " review-sticky" : ""}`} role="group" aria-label="Aksi review">
        <button type="button" className="btn btn-primary" disabled={busy || status === "accepted"} onClick={() => void send("accepted", null, false)} title="Terima (A)">
          <Icon icon={faCheck} /> Terima
        </button>
        <button type="button" className="btn btn-danger" disabled={busy} onClick={() => setRejecting({ next: false })} title="Tolak (R)">
          <Icon icon={faXmark} /> Tolak
        </button>
        {status !== "pending" && (
          <button type="button" className="btn" disabled={busy} onClick={() => void send("pending", null, false)} title="Kembalikan ke Menunggu review">
            <Icon icon={faRotateLeft} /> Batalkan review
          </button>
        )}
      </div>
      {next && (
        <div className="row">
          <button type="button" className="btn btn-small" disabled={busy || status === "accepted"} onClick={() => void send("accepted", null, true)}>
            <Icon icon={faCheck} /> Terima lalu berikutnya
          </button>
          <button type="button" className="btn btn-small" disabled={busy} onClick={() => setRejecting({ next: true })}>
            <Icon icon={faXmark} /> Tolak lalu berikutnya
          </button>
        </div>
      )}
      <div className="row review-nav">
        <button type="button" className="btn btn-small" disabled={!prev} onClick={() => go(prev)} title="Sebelumnya (K)"><Icon icon={faArrowLeft} /> Sebelumnya</button>
        <button type="button" className="btn btn-small" disabled={!next} onClick={() => go(next)} title="Berikutnya (J)">Berikutnya <Icon icon={faArrowRight} /></button>
      </div>
      {!isPhone && <p className="muted kbd-hint">Pintasan: <kbd>A</kbd> terima, <kbd>R</kbd> tolak, <kbd>J</kbd>/<kbd>K</kbd> berikutnya/sebelumnya.</p>}

      {rejecting && (
        <ReasonDialog title="Tolak submission" label="Alasan penolakan (dibaca annotator)" danger confirmLabel={<><Icon icon={faXmark} /> Tolak</>}
          onClose={() => setRejecting(null)} onConfirm={(reason) => send("rejected", reason, rejecting.next)}>
          <p className="muted">Annotator melihat status dan alasan ini di riwayatnya, dan bisa memperbaiki selama jendela edit 24 jam masih terbuka.</p>
        </ReasonDialog>
      )}
      {warn && (
        <Dialog title="Batalkan penerimaan?" onClose={() => setWarn(null)} busy={busy}
          footer={<>
            <button type="button" className="btn" onClick={() => setWarn(null)} disabled={busy}>Batal</button>
            <button type="button" className="btn btn-danger-solid" disabled={busy} onClick={() => void send(warn.status, warn.reason, warn.next, true)}>Lanjutkan</button>
          </>}>
          <Banner kind="warn">Submission ini sudah lebih dari 90 hari. Bila tidak lagi diterima, submission ini <strong>terhapus permanen</strong> pada putaran cleanup berikutnya (paling lama 1 jam).</Banner>
        </Dialog>
      )}
    </div>
  );
}
