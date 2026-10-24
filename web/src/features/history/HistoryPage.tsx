import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api/endpoints";
import { ReviewBadge } from "../../components/ReviewBadge";
import { isCode } from "../../lib/errors";
import { history, type HistoryEntry } from "../../lib/history";
import { currentOwner } from "../auth/owner";

const fmt = new Intl.DateTimeFormat("id-ID", { dateStyle: "medium", timeStyle: "short" });

/** Status review hanya diperbarui untuk laporan terbaru agar tidak membanjiri API. */
export const REFRESH_LATEST = 20;

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
            if (v.review) history.update(e.id, { review: v.review });
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
      <h1>Riwayat laporan</h1>
      {items.length === 0 ? (
        <div className="card">
          <p>Belum ada laporan dari perangkat ini.</p>
          <Link className="btn btn-primary" to="/">Buat laporan</Link>
        </div>
      ) : (
        <ul className="history-list">
          {items.map((e) => (
            <li key={e.id}>
              <Link to={`/s/${e.id}`} className="card history-item">
                <strong>{fmt.format(new Date(e.createdAt))}</strong>
                <span className="muted">{e.photos} foto · {e.boxes} kotak</span>
                {e.review && (
                  <span className="row">
                    <ReviewBadge status={e.review.status} />
                    {e.review.status === "rejected" && <span className="muted">Perbaiki: buka laporan</span>}
                  </span>
                )}
                {e.review?.status === "rejected" && e.review.reason && <span className="review-reason muted">Alasan: {e.review.reason}</span>}
              </Link>
            </li>
          ))}
        </ul>
      )}
      <p className="muted">Riwayat disimpan di perangkat ini saja. Laporan yang dihapus admin hilang dari daftar ini.</p>
    </>
  );
}
