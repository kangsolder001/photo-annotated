import { faClipboardList } from "@fortawesome/free-solid-svg-icons/faClipboardList";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api/endpoints";
import { Icon } from "../../components/Icon";
import { ReviewBadge } from "../../components/ReviewBadge";
import { isCode } from "../../lib/errors";
import { history, surveySummary, type HistoryEntry } from "../../lib/history";
import { currentOwner } from "../auth/owner";

const fmt = new Intl.DateTimeFormat("id-ID", { dateStyle: "medium", timeStyle: "short" });
const fmtDate = new Intl.DateTimeFormat("id-ID", { dateStyle: "medium", timeZone: "UTC" });

/** Tanggal pendataan `YYYY-MM-DD` sebagai teks (tanpa geser zona waktu). */
function surveyDate(d: string): string {
  const t = Date.parse(`${d}T00:00:00Z`);
  return Number.isNaN(t) ? d : fmtDate.format(t);
}

/** Status review hanya diperbarui untuk laporan terbaru agar tidak membanjiri API. */
export const REFRESH_LATEST = 20;

/** Halaman utama: tombol besar "Pendataan baru" + riwayat pendataan di perangkat ini (spec-pendataan §3.3). */
export function HistoryPage() {
  const owner = currentOwner();
  const [items, setItems] = useState<HistoryEntry[]>(() => history.list(owner));

  // perbarui status review (dan deteksi laporan yang dihapus admin) untuk laporan terbaru
  useEffect(() => {
    let alive = true;
    const latest = history.list(owner).slice(0, REFRESH_LATEST);
    void (async () => {
      for (let i = 0; i < latest.length; i += 4) {
        await Promise.all(latest.slice(i, i + 4).map(async (e) => {
          try {
            const v = await api.getSubmission(e.id, e.editToken);
            history.update(e.id, { ...(v.review ? { review: v.review } : {}), ...(v.answers ? { survey: surveySummary(v.answers) } : {}) });
          } catch (err) {
            if (isCode(err, "NOT_FOUND")) history.remove(e.id);
          }
        }));
        if (!alive) return;
        setItems(history.list(owner));
      }
    })();
    return () => {
      alive = false;
    };
  }, [owner]);

  return (
    <>
      <h1 className="visually-hidden">Beranda</h1>
      <Link className="btn btn-primary btn-block btn-hero" to="/pendataan/baru">
        <Icon icon={faClipboardList} /> <span>Pendataan baru</span>
      </Link>
      <h2>Riwayat pendataan</h2>
      {items.length === 0 ? (
        <div className="card">
          <p>Belum ada pendataan dari perangkat ini.</p>
        </div>
      ) : (
        <ul className="history-list">
          {items.map((e) => (
            <li key={e.id}>
              <Link to={`/s/${e.id}`} className="card history-item">
                {e.survey ? (
                  <>
                    <span className="row">
                      <strong>{e.survey.date ? surveyDate(e.survey.date) : fmt.format(new Date(e.createdAt))}</strong>
                      <span className={`badge ${e.survey.consent ? "badge-ok" : "badge-muted"}`}>{e.survey.consent ? "Bersedia" : "Menolak"}</span>
                    </span>
                    <span>RT {e.survey.rt || "-"} / RW {e.survey.rw || "-"} · {e.survey.code || "-"}</span>
                    <span className="muted">{e.photos} foto · {e.boxes} kotak</span>
                  </>
                ) : (
                  <>
                    <strong>{fmt.format(new Date(e.createdAt))}</strong>
                    <span className="muted">{e.photos} foto · {e.boxes} kotak</span>
                  </>
                )}
                {e.review && (
                  <span className="row">
                    <ReviewBadge status={e.review.status} />
                    {e.review.status === "rejected" && <span className="muted">Perbaiki: buka pendataan</span>}
                  </span>
                )}
                {e.review?.status === "rejected" && e.review.reason && <span className="review-reason muted">Alasan: {e.review.reason}</span>}
              </Link>
            </li>
          ))}
        </ul>
      )}
      <p className="muted">Riwayat disimpan di perangkat ini saja. Pendataan yang dihapus admin hilang dari daftar ini.</p>
    </>
  );
}
