import { faMagnifyingGlass, faRotate, faUserSlash } from "@fortawesome/free-solid-svg-icons";
import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { Icon } from "../../components/Icon";
import { Banner, Spinner } from "../../components/ui";
import { formatDateTime } from "../../lib/time";
import { adminApi } from "./api";
import { useAdmin, usePageTitle } from "./context";
import { adminErrorMessage } from "./errors";
import type { AdminUser } from "./types";
import { useIsPhone } from "./ui/useMediaQuery";

const PAGE = 20;

export function BlockedBadge({ blocked }: { blocked: boolean }) {
  return blocked
    ? <span className="badge badge-danger"><Icon icon={faUserSlash} /> Diblokir</span>
    : <span className="badge badge-ok">Aktif</span>;
}

/** Aktivitas terakhir = submission terakhir, atau login terakhir bila belum pernah mengirim. */
const lastActivity = (u: AdminUser) => u.stats.last_submission_at ?? u.last_login_at;

/** /admin/annotator (hanya auth.mode = otp): tabel annotator, cari nama/nomor, filter status. */
export function AnnotatorsPage() {
  usePageTitle("Annotator");
  const { authMode } = useAdmin();
  const isPhone = useIsPhone();
  const [sp, setSp] = useSearchParams();
  const q = sp.get("q") ?? "";
  const status = sp.get("status") === "active" || sp.get("status") === "blocked" ? (sp.get("status") as "active" | "blocked") : undefined;
  const [text, setText] = useState(q);
  const [items, setItems] = useState<AdminUser[]>();
  const [cursor, setCursor] = useState<string | null>(null);
  const [error, setError] = useState<string>();
  const [more, setMore] = useState(false);

  const load = useCallback(async (after?: string) => {
    setError(undefined);
    if (after) setMore(true);
    try {
      const p = await adminApi.users({ q: q || undefined, status, limit: PAGE, cursor: after });
      setItems((prev) => (after ? [...(prev ?? []), ...p.items] : p.items));
      setCursor(p.next_cursor || null);
    } catch (e) {
      setError(adminErrorMessage(e));
      if (!after) setItems([]);
    } finally {
      setMore(false);
    }
  }, [q, status]);

  useEffect(() => {
    // tunggu /admin/me: pada mode none endpoint ini 404 dan tidak boleh dipanggil
    if (authMode !== "otp") return;
    setItems(undefined);
    void load();
  }, [load, authMode]);
  useEffect(() => setText(q), [q]);

  const set = (p: Record<string, string | undefined>) => {
    const n = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(p)) if (v) n.set(k, v); else n.delete(k);
    setSp(n, { replace: true });
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    set({ q: text.trim() || undefined });
  };
  const rows = useMemo(() => items ?? [], [items]);

  if (authMode === "none") {
    return <div className="admin-page"><h1>Annotator</h1><Banner kind="info">Server berjalan tanpa akun annotator (mode tanpa login), jadi tidak ada data annotator.</Banner></div>;
  }

  return (
    <div className="admin-page">
      <div className="page-head"><h1>Annotator</h1></div>
      <form className="card filter-bar" onSubmit={submit} role="search" aria-label="Cari annotator">
        <div className="field filter-q">
          <label htmlFor="ann-q">Cari nama atau nomor</label>
          <div className="row nowrap-row">
            <input id="ann-q" type="search" value={text} onChange={(e) => setText(e.target.value)} placeholder="Nama atau nomor HP" />
            <button type="submit" className="btn" aria-label="Cari" title="Cari"><Icon icon={faMagnifyingGlass} /></button>
          </div>
        </div>
        <label className="field">Status
          <select value={status ?? ""} onChange={(e) => set({ status: e.target.value || undefined })}>
            <option value="">Aktif dan diblokir</option>
            <option value="active">Aktif</option>
            <option value="blocked">Diblokir</option>
          </select>
        </label>
      </form>

      {error && <Banner kind="error">{error} <button type="button" className="btn btn-small" onClick={() => void load()}><Icon icon={faRotate} /> Coba lagi</button></Banner>}
      {!items && !error && <Spinner label="Memuat annotator" />}
      {items?.length === 0 && !error && <div className="card"><p>Tidak ada annotator yang cocok.</p></div>}

      {rows.length > 0 && (isPhone ? (
        <ul className="card-list" aria-label="Daftar annotator">
          {rows.map((u) => (
            <li key={u.id}>
              <Link to={`/admin/annotator/${encodeURIComponent(u.id)}`} className="card user-card">
                <span className="row"><strong className="label-name">{u.name}</strong> <BlockedBadge blocked={u.blocked} /></span>
                <span className="muted">{u.phone}</span>
                <span className="muted">{u.stats.submissions} submission · {u.stats.annotations} kotak · {u.stats.accepted} diterima · {u.stats.rejected} ditolak</span>
                <span className="muted">Aktivitas terakhir: {lastActivity(u) ? formatDateTime(lastActivity(u)!) : "-"}</span>
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        <div className="table-wrap">
          <table className="admin-table">
            <thead>
              <tr>
                <th scope="col">Nama</th>
                <th scope="col">Nomor HP</th>
                <th scope="col" className="col-secondary">Email</th>
                <th scope="col" className="num">Submission</th>
                <th scope="col" className="num col-secondary">Kotak</th>
                <th scope="col" className="num">Diterima</th>
                <th scope="col" className="num">Ditolak</th>
                <th scope="col" className="col-secondary">Aktivitas terakhir</th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((u) => (
                <tr key={u.id}>
                  <td><Link to={`/admin/annotator/${encodeURIComponent(u.id)}`}>{u.name}</Link></td>
                  <td className="nowrap">{u.phone}</td>
                  <td className="col-secondary">{u.email}</td>
                  <td className="num">{u.stats.submissions}</td>
                  <td className="num col-secondary">{u.stats.annotations}</td>
                  <td className="num">{u.stats.accepted}</td>
                  <td className="num">{u.stats.rejected}</td>
                  <td className="col-secondary">{lastActivity(u) ? formatDateTime(lastActivity(u)!) : "-"}</td>
                  <td><BlockedBadge blocked={u.blocked} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ))}
      {cursor && items && (
        <div className="load-more">
          <button type="button" className="btn" onClick={() => void load(cursor)} disabled={more}>{more ? "Memuat…" : "Muat lagi"}</button>
        </div>
      )}
    </div>
  );
}
