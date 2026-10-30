import { faRotate } from "@fortawesome/free-solid-svg-icons/faRotate";
import { useCallback, useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { Icon } from "../../components/Icon";
import { Banner, Spinner } from "../../components/ui";
import { adminApi } from "./api";
import { usePageTitle } from "./context";
import { adminErrorMessage } from "./errors";
import type { SurveyStats } from "./types";

const nf = new Intl.NumberFormat("id-ID");
const pf = new Intl.NumberFormat("id-ID", { style: "percent", maximumFractionDigits: 1 });
const share = (n: number, total: number) => (total > 0 ? n / total : 0);

const SORTING: [keyof SurveyStats["r3_sorting"], string][] = [
  ["sorted", "Terpilah"], ["partial", "Sebagian"], ["mixed", "Tercampur"], ["unknown", "Tidak bisa dinilai"],
];

/**
 * /admin/statistik: statistik pendataan (spec-pendataan §5.8, §7.2): kartu total, tabel per RW dan per
 * mahasiswa, batang % Ya per butir B (CSS), distribusi C, dan kondisi pemilahan. Filter tanggal
 * pendataan + RW. Statistik per label kotak ada di /admin/statistik/label.
 */
export function SurveyStatsPage() {
  usePageTitle("Statistik");
  const [sp, setSp] = useSearchParams();
  const from = sp.get("from") ?? "";
  const to = sp.get("to") ?? "";
  const rw = sp.get("rw") ?? "";
  const [rwInput, setRwInput] = useState(rw);
  const [data, setData] = useState<SurveyStats>();
  const [error, setError] = useState<string>();

  const load = useCallback(async () => {
    setError(undefined);
    try {
      setData(await adminApi.surveyStats({ from: from || undefined, to: to || undefined, rw: rw || undefined }));
    } catch (e) {
      setError(adminErrorMessage(e));
    }
  }, [from, to, rw]);

  useEffect(() => {
    setData(undefined);
    void load();
  }, [load]);
  useEffect(() => setRwInput(rw), [rw]);

  const set = (k: string, v: string) => {
    const n = new URLSearchParams(sp);
    if (v) n.set(k, v);
    else n.delete(k);
    setSp(n, { replace: true });
  };

  const t = data?.totals;
  const sortingTotal = data ? SORTING.reduce((n, [k]) => n + data.r3_sorting[k], 0) : 0;

  return (
    <div className="admin-page">
      <div className="page-head">
        <h1>Statistik pendataan</h1>
        <div className="row">
          <Link className="btn" to="/admin/statistik/label">Statistik label kotak</Link>
          <button type="button" className="btn" onClick={() => void load()}><Icon icon={faRotate} /> Muat ulang</button>
        </div>
      </div>
      <form className="card filter-bar" aria-label="Filter statistik pendataan" onSubmit={(e) => { e.preventDefault(); set("rw", rwInput.trim()); }}>
        <label className="field">Dari<input type="date" value={from} max={to || undefined} onChange={(e) => set("from", e.target.value)} /></label>
        <label className="field">Sampai<input type="date" value={to} min={from || undefined} onChange={(e) => set("to", e.target.value)} /></label>
        <label className="field">RW
          <input value={rwInput} onChange={(e) => setRwInput(e.target.value)} onBlur={() => set("rw", rwInput.trim())} maxLength={10} inputMode="numeric" placeholder="Semua RW" />
        </label>
      </form>
      <p className="muted">Tanggal = tanggal pendataan. Butir B, C, dan kondisi pemilahan hanya dari rumah tangga yang bersedia; persentase dari jawaban yang ada.</p>

      {error && <Banner kind="error">{error}</Banner>}
      {!data && !error && <Spinner label="Memuat statistik" />}
      {data && t && (
        <>
          <div className="stat-tiles">
            <div className="card stat-tile"><span className="stat-value">{nf.format(t.households)}</span><span className="muted">rumah tangga</span></div>
            <div className="card stat-tile"><span className="stat-value">{nf.format(t.consented)}</span><span className="muted">bersedia</span></div>
            <div className="card stat-tile">
              <span className="stat-value">{nf.format(t.declined)}</span>
              <span className="muted">menolak ({pf.format(share(t.declined, t.households))})</span>
            </div>
          </div>

          <section aria-labelledby="rw-h" className="card">
            <h2 id="rw-h">Per RW</h2>
            {data.by_rw.length === 0 ? <p className="muted">Belum ada pendataan untuk filter ini.</p> : (
              <div className="table-wrap">
                <table className="admin-table">
                  <thead><tr><th scope="col">RW</th><th scope="col" className="num">Rumah tangga</th><th scope="col" className="num">Bersedia</th><th scope="col" className="num">Menolak</th></tr></thead>
                  <tbody>
                    {data.by_rw.map((r) => (
                      <tr key={r.rw}><th scope="row">{r.rw}</th><td className="num">{nf.format(r.households)}</td><td className="num">{nf.format(r.consented)}</td><td className="num">{nf.format(r.declined)}</td></tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          <section aria-labelledby="student-h" className="card">
            <h2 id="student-h">Per mahasiswa</h2>
            {data.by_student.length === 0 ? <p className="muted">Belum ada pendataan untuk filter ini.</p> : (
              <div className="table-wrap">
                <table className="admin-table">
                  <thead>
                    <tr><th scope="col">Mahasiswa</th><th scope="col">NIM</th><th scope="col" className="col-secondary">Kelompok</th><th scope="col" className="num">Rumah tangga</th><th scope="col" className="num">Menolak</th></tr>
                  </thead>
                  <tbody>
                    {data.by_student.map((s) => (
                      <tr key={s.user_id}>
                        <th scope="row" className="label-name">{s.name}</th>
                        <td className="nowrap">{s.nim}</td>
                        <td className="col-secondary">{s.group}</td>
                        <td className="num">{nf.format(s.households)}</td>
                        <td className="num">{nf.format(s.declined)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          <section aria-labelledby="b-h" className="card">
            <h2 id="b-h">Checklist B: % Ya per butir</h2>
            <table className="bar-table">
              <caption className="visually-hidden">Persentase jawaban Ya per butir B</caption>
              <thead>
                <tr>
                  <th scope="col">Butir</th>
                  <th scope="col" className="bar-col"><span className="visually-hidden">Batang</span></th>
                  <th scope="col" className="num">% Ya</th>
                  <th scope="col" className="num">Ya</th>
                  <th scope="col" className="num">Tidak</th>
                </tr>
              </thead>
              <tbody>
                {data.b.map((b) => (
                  <tr key={b.id} title={`${b.id.toUpperCase()}: ${pf.format(b.yes_share)} Ya`}>
                    <th scope="row" className="bar-name"><span className="survey-item"><strong>{b.id.toUpperCase()}</strong> {b.label}</span></th>
                    <td className="bar-col" aria-hidden="true">
                      <div className="bar-track bar-track-soft"><div className="bar bar-primary" style={{ width: `${Math.max(b.yes_share * 100, 0.5)}%` }} /></div>
                    </td>
                    <td className="num" data-label="% Ya">{pf.format(b.yes_share)}</td>
                    <td className="num" data-label="Ya">{nf.format(b.yes)}</td>
                    <td className="num" data-label="Tidak">{nf.format(b.no)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </section>

          <section aria-labelledby="c-h" className="card">
            <h2 id="c-h">Observasi C</h2>
            <div className="table-wrap">
              <table className="admin-table">
                <thead>
                  <tr><th scope="col">Butir</th><th scope="col" className="num">Ada/Sesuai</th><th scope="col" className="num">Tidak</th><th scope="col" className="num">Tidak teramati</th></tr>
                </thead>
                <tbody>
                  {data.c.map((c) => (
                    <tr key={c.id}>
                      <th scope="row"><span className="survey-item"><strong>{c.id.toUpperCase()}</strong> {c.label}</span></th>
                      <td className="num">{nf.format(c.yes)}</td>
                      <td className="num">{nf.format(c.no)}</td>
                      <td className="num">{nf.format(c.na)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>

          <section aria-labelledby="sorting-h" className="card">
            <h2 id="sorting-h">Kondisi pemilahan (label lapangan R3)</h2>
            <table className="bar-table">
              <caption className="visually-hidden">Jumlah rumah tangga per kondisi pemilahan</caption>
              <thead>
                <tr>
                  <th scope="col">Kondisi</th>
                  <th scope="col" className="bar-col"><span className="visually-hidden">Batang</span></th>
                  <th scope="col" className="num">Rumah tangga</th>
                  <th scope="col" className="num">Porsi</th>
                </tr>
              </thead>
              <tbody>
                {SORTING.map(([k, label]) => {
                  const n = data.r3_sorting[k];
                  return (
                    <tr key={k}>
                      <th scope="row" className="bar-name">{label}</th>
                      <td className="bar-col" aria-hidden="true">
                        <div className="bar-track bar-track-soft"><div className="bar bar-primary" style={{ width: `${Math.max(share(n, sortingTotal) * 100, 0.5)}%` }} /></div>
                      </td>
                      <td className="num" data-label="Rumah tangga">{nf.format(n)}</td>
                      <td className="num" data-label="Porsi">{pf.format(share(n, sortingTotal))}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </section>
        </>
      )}
    </div>
  );
}
