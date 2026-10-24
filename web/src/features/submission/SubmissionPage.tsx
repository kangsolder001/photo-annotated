import { useCallback, useEffect, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { api } from "../../api/endpoints";
import type { SubmissionView } from "../../api/types";
import { Banner, Spinner } from "../../components/ui";
import { config } from "../../config";
import { fromView } from "../../lib/annotation";
import { drafts } from "../../lib/draft";
import { errorMessage, isCode } from "../../lib/errors";
import { history } from "../../lib/history";
import { optionsFromView } from "../../lib/labels";
import { formatDateTime, formatRemaining } from "../../lib/time";
import { PhotoAnnotator } from "../annotate/PhotoAnnotator";
import { currentOwner } from "../auth/owner";
import { EditSubmission } from "./EditSubmission";
import { usePhotoURLs } from "./usePhotoURLs";

/** Satu laporan: baca, dan edit selama jendela 24 jam (spec §4.4). */
export function SubmissionPage() {
  const { id = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const owner = currentOwner();
  const editToken = history.get(id, owner)?.editToken;
  const [view, setView] = useState<SubmissionView>();
  const [error, setError] = useState<string>();
  const [editing, setEditing] = useState(false);
  const [notice, setNotice] = useState<string | undefined>(params.get("baru") ? "Laporan terkirim. Anda masih bisa mengubahnya selama 24 jam." : undefined);
  const [now, setNow] = useState(() => Date.now());

  const load = useCallback(async () => {
    setError(undefined);
    try {
      setView(await api.getSubmission(id, editToken));
    } catch (e) {
      if (isCode(e, "NOT_FOUND") && history.get(id, owner)) {
        // tercatat di riwayat perangkat ini tetapi sudah tidak ada: dihapus admin (spec-labels-admin §5.5)
        history.remove(id);
        drafts.clear(id, owner);
        setError("Laporan ini sudah dihapus. Laporan tersebut juga dihapus dari riwayat di perangkat ini.");
        return;
      }
      setError(isCode(e, "NOT_FOUND") ? "Laporan tidak ditemukan, atau perangkat/akun ini tidak punya akses ke laporan tersebut." : errorMessage(e));
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
        <Link to="/riwayat" className="btn">Kembali ke riwayat</Link>
      </>
    );
  }
  if (!view) return <Spinner label="Memuat laporan" />;

  const remainingMs = new Date(view.editable_until).getTime() - now;
  const editable = view.editable && remainingMs > 0;
  // mode none: tanpa edit_token di perangkat ini, laporan tidak bisa diubah dari sini
  const canEdit = editable && (config.authMode === "otp" || !!editToken);

  if (editing && canEdit) {
    return (
      <>
        <h1>Edit laporan</h1>
        <p className="muted">Sisa waktu edit: {formatRemaining(remainingMs)}.</p>
        <EditSubmission
          view={view}
          urls={urls}
          refreshUrl={(pid) => void refresh(pid)}
          editToken={editToken}
          onSaved={() => {
            setEditing(false);
            setNotice("Perubahan tersimpan.");
            void load();
          }}
          onCancel={() => {
            drafts.clear(view.id, owner);
            setEditing(false);
          }}
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
      <h1>Laporan</h1>
      {notice && <Banner kind="ok">{notice}</Banner>}
      <div className="card stack">
        <div className="muted">Dibuat {formatDateTime(view.created_at)}{view.version > 1 && ` · diubah ${formatDateTime(view.updated_at)}`}</div>
        {canEdit ? (
          <div className="row">
            <span>Bisa diedit sampai {formatDateTime(view.editable_until)} (sisa {formatRemaining(remainingMs)}).</span>
            <button type="button" className="btn btn-primary" onClick={() => { setNotice(undefined); setEditing(true); }}>Edit</button>
          </div>
        ) : (
          <span className="muted">{editable ? "Laporan ini hanya bisa diedit dari perangkat yang membuatnya." : "Batas waktu edit (24 jam) sudah lewat. Laporan hanya bisa dibaca."}</span>
        )}
      </div>
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
