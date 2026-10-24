import { faArrowLeft, faEnvelope, faImages, faPhone, faRotate, faUser, faUserSecret, faUserSlash } from "@fortawesome/free-solid-svg-icons";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Icon } from "../../components/Icon";
import { ReviewBadge } from "../../components/ReviewBadge";
import { Banner, Spinner } from "../../components/ui";
import { textOn } from "../../lib/color";
import { isCode } from "../../lib/errors";
import { UNLABELED_COLOR } from "../../lib/labels";
import { formatDateTime } from "../../lib/time";
import { adminApi } from "./api";
import { useAdmin, usePageTitle } from "./context";
import { adminErrorMessage } from "./errors";
import { daysUntil } from "./filters";
import { listContext } from "./listContext";
import type { AdminPhoto, AdminSubmission } from "./types";
import { AnnotatedPhoto } from "./ui/AnnotatedPhoto";

/** /admin/anotasi/:id: semua foto + kotak, annotator, waktu, versi (spec-labels-admin §7.2). */
export function SubmissionDetailPage() {
  usePageTitle("Detail anotasi");
  const { id = "" } = useParams();
  const { authMode } = useAdmin();
  const [sub, setSub] = useState<AdminSubmission>();
  const [error, setError] = useState<{ text: string; gone?: boolean }>();
  const lastRefresh = useRef(0);

  const load = useCallback(async () => {
    setError(undefined);
    try {
      setSub(await adminApi.submission(id));
    } catch (e) {
      setError({ text: isCode(e, "NOT_FOUND") ? "Submission ini tidak ditemukan atau sudah dihapus." : adminErrorMessage(e), gone: isCode(e, "NOT_FOUND") });
    }
  }, [id]);

  useEffect(() => {
    setSub(undefined);
    void load();
  }, [load]);

  // URL foto bertanda tangan berumur pendek: gambar gagal dimuat → muat ulang detail (paling sering 1×/10 detik)
  const refreshUrls = () => {
    if (Date.now() - lastRefresh.current < 10_000) return;
    lastRefresh.current = Date.now();
    void load();
  };

  const back = `/admin/anotasi${listContext.get()?.search ? `?${listContext.get()!.search}` : ""}`;

  if (error) {
    return (
      <div className="admin-page">
        <Banner kind="error">{error.text}</Banner>
        <div className="row">
          <Link className="btn" to={back}><Icon icon={faArrowLeft} /> Kembali ke daftar</Link>
          {!error.gone && <button type="button" className="btn" onClick={() => void load()}><Icon icon={faRotate} /> Coba lagi</button>}
        </div>
      </div>
    );
  }
  if (!sub) return <div className="admin-page"><Spinner label="Memuat submission" /></div>;

  return (
    <div className="admin-page">
      <div className="page-head">
        <Link className="btn" to={back}><Icon icon={faArrowLeft} /> Daftar</Link>
      </div>
      <div className="detail-grid">
        <div className="detail-photos">
          {sub.photos.map((p, i) => (
            <PhotoCard key={p.photo_id} photo={p} index={i} total={sub.photos.length} onImageError={refreshUrls} />
          ))}
          {sub.photos.length === 0 && <div className="card"><p>Tidak ada foto.</p></div>}
        </div>
        <aside className="detail-panel" aria-label="Info submission">
          <AnnotatorCard sub={sub} showProfile={authMode === "otp"} />
          <div className="card stack">
            <h2>Submission</h2>
            <dl className="meta">
              <dt>Dibuat</dt><dd>{formatDateTime(sub.created_at)}</dd>
              <dt>Diubah</dt><dd>{formatDateTime(sub.updated_at)}</dd>
              <dt>Versi</dt><dd>{sub.version}</dd>
              <dt>Foto / kotak</dt><dd>{sub.photos.length} / {sub.photos.reduce((n, p) => n + p.annotations.length, 0)}</dd>
              <dt>Review</dt><dd><ReviewBadge status={sub.review.status} /></dd>
              <dt>Retensi</dt>
              <dd>{sub.retention_deletes_at ? `Terhapus otomatis ${formatDateTime(sub.retention_deletes_at)} (${daysUntil(sub.retention_deletes_at)} hari lagi)` : "Disimpan tanpa batas (diterima)"}</dd>
            </dl>
            {sub.review.reason && <p className="muted">Alasan: {sub.review.reason}</p>}
          </div>
        </aside>
      </div>
    </div>
  );
}

function AnnotatorCard({ sub, showProfile }: { sub: AdminSubmission; showProfile: boolean }) {
  const a = sub.annotator;
  if (!a) {
    return (
      <div className="card">
        <h2><Icon icon={faUserSecret} /> Anonim</h2>
        <p className="muted">Dikirim tanpa login (mode tanpa akun) atau akun annotator sudah dihapus.</p>
      </div>
    );
  }
  return (
    <div className="card stack">
      <h2><Icon icon={a.blocked ? faUserSlash : faUser} /> <span className="label-name">{a.name}</span></h2>
      {a.blocked && <span className="badge badge-danger"><Icon icon={faUserSlash} /> Diblokir</span>}
      <a href={`mailto:${a.email}`} className="contact"><Icon icon={faEnvelope} /> {a.email}</a>
      <a href={`tel:+${a.phone}`} className="contact"><Icon icon={faPhone} /> {a.phone}</a>
      <div className="row">
        <Link className="btn btn-small" to={`/admin/anotasi?user_id=${encodeURIComponent(a.id)}`}><Icon icon={faImages} /> Semua dari annotator ini</Link>
        {showProfile && <Link className="btn btn-small" to={`/admin/annotator/${encodeURIComponent(a.id)}`}><Icon icon={faUser} /> Profil</Link>}
      </div>
    </div>
  );
}

function PhotoCard({ photo, index, total, onImageError }: { photo: AdminPhoto; index: number; total: number; onImageError(): void }) {
  const colors = useMemo(() => new Map(photo.annotations.flatMap((a) => (a.label ? [[a.label.id, a.label.color] as const] : []))), [photo]);
  return (
    <section className="card photo-card" aria-label={`Foto ${index + 1}`}>
      <div className="photo-card-head">
        <h2>Foto {index + 1} dari {total}</h2>
        <span className="muted">{photo.width} × {photo.height} px · {photo.annotations.length} kotak</span>
      </div>
      <AnnotatedPhoto src={photo.url} width={photo.width} height={photo.height} alt={`Foto ${index + 1} dengan kotak anotasi`} display
        boxes={photo.annotations.map((a) => ({ labelId: a.label?.id ?? null, x: a.x, y: a.y, w: a.w, h: a.h }))} colors={colors} onImageError={onImageError} />
      {photo.annotations.length > 0 ? (
        <ol className="box-list">
          {photo.annotations.map((a, i) => {
            const color = a.label?.color ?? UNLABELED_COLOR;
            return (
              <li key={i}>
                <span className="ann-num" style={{ background: color, color: textOn(color) }} aria-hidden="true">{i + 1}</span>
                <div className="ann-body">
                  {a.label ? (
                    <span className="label-chip">
                      <span className="label-dot" style={{ background: a.label.color }} aria-hidden="true" />{a.label.name}
                      {a.label.archived && <span className="badge badge-muted">diarsipkan</span>}
                    </span>
                  ) : (
                    <p className="ann-legacy"><span className="legacy-tag">catatan lama</span> <span className="ann-text">{a.text}</span></p>
                  )}
                  <span className="muted">{Math.round(a.w * photo.width)} × {Math.round(a.h * photo.height)} px</span>
                </div>
              </li>
            );
          })}
        </ol>
      ) : <p className="muted">Tidak ada kotak pada foto ini.</p>}
    </section>
  );
}
