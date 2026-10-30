import { useCallback, useEffect, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { api } from "../../api/endpoints";
import type { SubmissionView } from "../../api/types";
import { ReviewBadge } from "../../components/ReviewBadge";
import { SurveyAnswers } from "../../components/SurveyAnswers";
import { Banner, Spinner } from "../../components/ui";
import { config } from "../../config";
import { fromView } from "../../lib/annotation";
import { drafts } from "../../lib/draft";
import { errorMessage, isCode } from "../../lib/errors";
import { history, surveySummary } from "../../lib/history";
import { optionsFromView } from "../../lib/labels";
import { formatDateTime, formatRemaining } from "../../lib/time";
import { PhotoAnnotator } from "../annotate/PhotoAnnotator";
import { currentOwner } from "../auth/owner";
import { SurveyWizard } from "../survey/SurveyWizard";
import { useSurveyForm } from "../survey/useSurveyForm";
import { usePhotoURLs } from "./usePhotoURLs";

/** Satu pendataan: baca (jawaban + foto), dan edit lewat wizard yang sama selama 24 jam (spec §4.4). */
export function SubmissionPage() {
  const { id = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const owner = currentOwner();
  const editToken = history.get(id, owner)?.editToken;
  const [view, setView] = useState<SubmissionView>();
  const [error, setError] = useState<string>();
  const [editing, setEditing] = useState(false);
  const [notice, setNotice] = useState<string | undefined>(params.get("baru") ? "Pendataan terkirim. Anda masih bisa mengubahnya selama 24 jam." : undefined);
  const [now, setNow] = useState(() => Date.now());
  const { form, error: formError, reload: reloadForm } = useSurveyForm();

  const load = useCallback(async () => {
    setError(undefined);
    // dibaca sebelum request: pemuatan yang berbarengan (StrictMode, muat ulang) bisa sudah menghapus
    // entri riwayat saat 404 kedua tiba, dan pesan "sudah dihapus" tidak boleh tertimpa
    const known = Boolean(history.get(id, owner));
    try {
      const v = await api.getSubmission(id, editToken);
      setView(v);
      history.update(id, { ...(v.review ? { review: v.review } : {}), ...(v.answers ? { survey: surveySummary(v.answers) } : {}) });
    } catch (e) {
      if (isCode(e, "NOT_FOUND") && known) {
        // tercatat di riwayat perangkat ini tetapi sudah tidak ada: dihapus admin (spec-labels-admin §5.5)
        history.remove(id);
        drafts.clear(id, owner);
        setError("Pendataan ini sudah dihapus. Pendataan tersebut juga dihapus dari riwayat di perangkat ini.");
        return;
      }
      setError(isCode(e, "NOT_FOUND") ? "Pendataan tidak ditemukan, atau akun ini tidak punya akses ke pendataan tersebut." : errorMessage(e));
    }
  }, [id, editToken, owner]);

  useEffect(() => {
    void load();
    if (params.has("baru")) setParams({}, { replace: true });
    // params sengaja tidak menjadi dependensi: penanda ?baru hanya dibaca sekali saat tiba
  }, [load]);

  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(t);
  }, []);

  const { urls, refresh } = usePhotoURLs(view?.photos.map((p) => p.photo_id) ?? [], editToken);

  if (error) {
    return (
      <>
        <Banner kind="error">{error}</Banner>
        <Link to="/" className="btn">Kembali ke riwayat</Link>
      </>
    );
  }
  if (!view) return <Spinner label="Memuat pendataan" />;
  if (formError) {
    return <Banner kind="error">Formulir gagal dimuat: {formError} <button type="button" className="btn btn-small" onClick={() => void reloadForm()}>Coba lagi</button></Banner>;
  }
  if (!form) return <Spinner label="Memuat formulir" />;

  const remainingMs = new Date(view.editable_until).getTime() - now;
  const editable = view.editable && remainingMs > 0;
  // mode none: tanpa edit_token di perangkat ini, laporan tidak bisa diubah dari sini
  const canEdit = editable && (config.authMode === "otp" || !!editToken);

  if (editing && canEdit) {
    return (
      <>
        <h1>Edit pendataan</h1>
        <p className="muted">Sisa waktu edit: {formatRemaining(remainingMs)}.</p>
        <SurveyWizard
          form={form}
          view={view}
          urls={urls}
          refreshUrl={(pid) => void refresh(pid)}
          editToken={editToken}
          onSaved={() => {
            setEditing(false);
            setNotice("Perubahan tersimpan.");
            void load();
          }}
          onCancel={() => setEditing(false)}
          onReload={() => {
            setEditing(false);
            void load();
          }}
        />
      </>
    );
  }

  return (
    <>
      <h1>Pendataan</h1>
      {notice && <Banner kind="ok">{notice}</Banner>}
      <div className="card stack">
        <div className="muted">Dibuat {formatDateTime(view.created_at)}{view.version > 1 && ` · diubah ${formatDateTime(view.updated_at)}`}</div>
        {view.review && (
          <div className="stack review-box">
            <div><ReviewBadge status={view.review.status} /></div>
            {view.review.status === "rejected" && view.review.reason && <p className="review-reason">Alasan: {view.review.reason}</p>}
            {view.review.status === "rejected" && canEdit && (
              <p className="muted">Anda bisa memperbaiki pendataan ini selama waktu edit masih ada; setelah disimpan pendataan kembali menunggu review.</p>
            )}
          </div>
        )}
        {canEdit ? (
          <div className="row">
            <span>Bisa diedit sampai {formatDateTime(view.editable_until)} (sisa {formatRemaining(remainingMs)}).</span>
            <button type="button" className="btn btn-primary" onClick={() => { setNotice(undefined); setEditing(true); }}>
              {view.review?.status === "rejected" ? "Perbaiki" : "Edit"}
            </button>
          </div>
        ) : (
          <span className="muted">{editable ? "Pendataan ini hanya bisa diedit dari perangkat yang membuatnya." : "Batas waktu edit (24 jam) sudah lewat. Pendataan hanya bisa dibaca."}</span>
        )}
      </div>
      {view.answers ? <SurveyAnswers form={form} answers={view.answers} /> : <p className="muted">Laporan lama tanpa jawaban pendataan.</p>}
      {view.photos.map((p, i) => {
        const url = urls[p.photo_id];
        return url ? (
          <PhotoAnnotator key={p.photo_id} src={url} width={p.width} height={p.height} label={`Foto ${i + 1}`}
            boxes={p.annotations.map(fromView)} labels={optionsFromView(p.annotations)} readOnly onImageError={() => void refresh(p.photo_id)} />
        ) : (
          <div key={p.photo_id} className="card"><Spinner label={`Memuat foto ${i + 1}`} /></div>
        );
      })}
    </>
  );
}
