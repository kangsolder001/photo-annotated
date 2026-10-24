import { Link, useSearchParams } from "react-router-dom";
import { Banner } from "../../components/ui";
import { history } from "../../lib/history";
import { currentOwner } from "../auth/owner";

const fmt = new Intl.DateTimeFormat("id-ID", { dateStyle: "medium", timeStyle: "short" });

export function HistoryPage() {
  const [params] = useSearchParams();
  const items = history.list(currentOwner());
  const created = params.get("baru");
  return (
    <>
      <h1>Riwayat laporan</h1>
      {created && <Banner kind="ok">Laporan terkirim. Anda masih bisa mengubahnya selama 24 jam.</Banner>}
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
              </Link>
            </li>
          ))}
        </ul>
      )}
      <p className="muted">Riwayat disimpan di perangkat ini saja.</p>
    </>
  );
}
