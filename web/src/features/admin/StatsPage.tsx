import { faRotate } from "@fortawesome/free-solid-svg-icons/faRotate";
import { useCallback, useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { Icon } from "../../components/Icon";
import { Banner, Spinner } from "../../components/ui";
import { adminApi } from "./api";
import { usePageTitle } from "./context";
import { adminErrorMessage } from "./errors";
import { dayAfter, dayStart } from "./filters";
import type { LabelStat, LabelStats, ReviewStatus } from "./types";

const nf = new Intl.NumberFormat("id-ID");
const pf = new Intl.NumberFormat("id-ID", { style: "percent", maximumFractionDigits: 1 });

/**
 * /admin/statistik/label: jumlah kotak per label (batang horizontal berwarna label, urut terbanyak), dengan
 * angka kotak / foto / submission + persentase; label tanpa contoh di bawah. Filter tanggal dan
 * status review (default: diterima = dataset).
 */
export function StatsPage() {
  usePageTitle("Statistik");
  const [sp, setSp] = useSearchParams();
  const from = sp.get("from") ?? "";
  const to = sp.get("to") ?? "";
  const rs = sp.has("review_status") ? (sp.get("review_status") as ReviewStatus | "") : "accepted";
  const [data, setData] = useState<LabelStats>();
  const [error, setError] = useState<string>();

  const load = useCallback(async () => {
    setError(undefined);
    try {
      setData(await adminApi.labelStats({ from: from ? dayStart(from) : undefined, to: to ? dayAfter(to) : undefined, review_status: rs || undefined }));
    } catch (e) {
      setError(adminErrorMessage(e));
    }
  }, [from, to, rs]);

  useEffect(() => {
    setData(undefined);
    void load();
  }, [load]);

  const set = (k: string, v: string) => {
    const n = new URLSearchParams(sp);
    n.set(k, v);
    if (k !== "review_status" && !v) n.delete(k);
    setSp(n, { replace: true });
  };

  const sorted = [...(data?.labels ?? [])].sort((a, b) => b.annotations - a.annotations || a.name.localeCompare(b.name, "id"));
  const used = sorted.filter((l) => l.annotations > 0);
  const empty = sorted.filter((l) => l.annotations === 0);
  const max = Math.max(1, ...used.map((l) => l.annotations));

  return (
    <div className="admin-page">
      <div className="page-head">
        <h1>Statistik label</h1>
        <div className="row">
          <Link className="btn" to="/admin/statistik">Statistik pendataan</Link>
          <button type="button" className="btn" onClick={() => void load()}><Icon icon={faRotate} /> Muat ulang</button>
        </div>
      </div>
      <form className="card filter-bar" aria-label="Filter statistik" onSubmit={(e) => e.preventDefault()}>
        <label className="field">Dari<input type="date" value={from} max={to || undefined} onChange={(e) => set("from", e.target.value)} /></label>
        <label className="field">Sampai<input type="date" value={to} min={from || undefined} onChange={(e) => set("to", e.target.value)} /></label>
        <label className="field">Status review
          <select value={rs} onChange={(e) => set("review_status", e.target.value)}>
            <option value="accepted">Diterima (dataset)</option>
            <option value="">Semua status</option>
            <option value="pending">Menunggu review</option>
            <option value="rejected">Ditolak</option>
          </select>
        </label>
      </form>

      {error && <Banner kind="error">{error}</Banner>}
      {!data && !error && <Spinner label="Memuat statistik" />}
      {data && (
        <>
          <div className="stat-tiles">
            <div className="card stat-tile"><span className="stat-value">{nf.format(data.totals.annotations)}</span><span className="muted">kotak</span></div>
            <div className="card stat-tile"><span className="stat-value">{nf.format(data.totals.photos)}</span><span className="muted">foto</span></div>
            <div className="card stat-tile"><span className="stat-value">{nf.format(data.totals.submissions)}</span><span className="muted">submission</span></div>
            {data.totals.legacy_text_annotations > 0 && (
              <div className="card stat-tile"><span className="stat-value">{nf.format(data.totals.legacy_text_annotations)}</span><span className="muted">catatan lama (tanpa label)</span></div>
            )}
          </div>

          <section aria-labelledby="bars-h" className="card">
            <h2 id="bars-h">Kotak per label</h2>
            {used.length === 0 ? <p className="muted">Belum ada kotak berlabel untuk filter ini.</p> : (
              <table className="bar-table">
                <caption className="visually-hidden">Jumlah kotak, foto, dan submission per label, urut terbanyak</caption>
                <thead>
                  <tr>
                    <th scope="col">Label</th>
                    <th scope="col" className="bar-col"><span className="visually-hidden">Batang</span></th>
                    <th scope="col" className="num">Kotak</th>
                    <th scope="col" className="num">Porsi</th>
                    <th scope="col" className="num">Foto</th>
                    <th scope="col" className="num">Submission</th>
                  </tr>
                </thead>
                <tbody>{used.map((l) => <BarRow key={l.id} l={l} max={max} />)}</tbody>
              </table>
            )}
          </section>

          {empty.length > 0 && (
            <section aria-labelledby="empty-h" className="card">
              <h2 id="empty-h">Belum ada contoh ({empty.length})</h2>
              <p className="muted">Label ini belum dipakai kotak mana pun untuk filter ini; dataset kekurangan contoh.</p>
              <ul className="chips">
                {empty.map((l) => (
                  <li key={l.id} className="chip"><span className="label-dot" style={{ background: l.color }} aria-hidden="true" />{l.name}{l.archived && <span className="muted"> (diarsipkan)</span>}</li>
                ))}
              </ul>
            </section>
          )}
        </>
      )}
    </div>
  );
}

function BarRow({ l, max }: { l: LabelStat; max: number }) {
  const w = (l.annotations / max) * 100;
  const tip = `${l.name}: ${nf.format(l.annotations)} kotak (${pf.format(l.share)}), ${nf.format(l.photos)} foto, ${nf.format(l.submissions)} submission`;
  return (
    <tr title={tip}>
      <th scope="row" className="bar-name">
        <span className="bar-name-in">
          <span className="label-dot" style={{ background: l.color }} aria-hidden="true" />
          <span className="label-name">{l.name}</span>{l.archived && <span className="badge badge-muted">diarsipkan</span>}
        </span>
      </th>
      <td className="bar-col" aria-hidden="true">
        <div className="bar-track"><div className="bar" style={{ width: `${Math.max(w, 0.5)}%`, background: l.color }} /></div>
      </td>
      <td className="num" data-label="Kotak">{nf.format(l.annotations)}</td>
      <td className="num" data-label="Porsi">{pf.format(l.share)}</td>
      <td className="num" data-label="Foto">{nf.format(l.photos)}</td>
      <td className="num" data-label="Submission">{nf.format(l.submissions)}</td>
    </tr>
  );
}
