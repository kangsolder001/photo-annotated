import type { IconDefinition } from "@fortawesome/fontawesome-svg-core";
import { faClock } from "@fortawesome/free-regular-svg-icons/faClock";
import { faBroom } from "@fortawesome/free-solid-svg-icons/faBroom";
import { faCircleCheck } from "@fortawesome/free-solid-svg-icons/faCircleCheck";
import { faCircleXmark } from "@fortawesome/free-solid-svg-icons/faCircleXmark";
import { faCloudArrowUp } from "@fortawesome/free-solid-svg-icons/faCloudArrowUp";
import { faDatabase } from "@fortawesome/free-solid-svg-icons/faDatabase";
import { faHardDrive } from "@fortawesome/free-solid-svg-icons/faHardDrive";
import { faMessage } from "@fortawesome/free-solid-svg-icons/faMessage";
import { faRotate } from "@fortawesome/free-solid-svg-icons/faRotate";
import { faTemperatureHalf } from "@fortawesome/free-solid-svg-icons/faTemperatureHalf";
import { faTriangleExclamation } from "@fortawesome/free-solid-svg-icons/faTriangleExclamation";
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { Icon } from "../../components/Icon";
import { Banner, Spinner } from "../../components/ui";
import { formatDateTime } from "../../lib/time";
import { adminApi } from "./api";
import { usePageTitle } from "./context";
import { adminErrorMessage } from "./errors";
import { assess, LEVEL_TEXT, type CardKey, type Level } from "./systemStatus";
import { partError, type SystemStatus } from "./types";

const CARD_ICON: Record<CardKey, IconDefinition> = {
  database: faDatabase, storage: faHardDrive, uploads: faCloudArrowUp, whatsapp: faMessage, cleanup: faBroom, host: faTemperatureHalf,
};
const LEVEL_ICON: Record<Level, IconDefinition> = { ok: faCircleCheck, warn: faTriangleExclamation, danger: faCircleXmark };
export const REFRESH_MS = 30_000;
const QUEUE = "/admin/anotasi?review_status=pending&sort=oldest";

/** Beranda: status sistem saat ini (dimuat ulang tiap 30 detik selama tab terlihat) + ringkasan pekerjaan. */
export function HomePage() {
  usePageTitle("Beranda");
  const [s, setS] = useState<SystemStatus>();
  const [error, setError] = useState<string>();

  const load = useCallback(async () => {
    try {
      setS(await adminApi.system());
      setError(undefined);
    } catch (e) {
      setError(adminErrorMessage(e));
    }
  }, []);

  useEffect(() => {
    void load();
    const t = setInterval(() => {
      if (document.visibilityState === "visible") void load();
    }, REFRESH_MS);
    const onVis = () => document.visibilityState === "visible" && void load();
    document.addEventListener("visibilitychange", onVis);
    return () => {
      clearInterval(t);
      document.removeEventListener("visibilitychange", onVis);
    };
  }, [load]);

  const data = s && !partError(s.data) && !("error" in s.data) ? s.data : null;
  const api = s && !("error" in s.api) ? s.api : null;

  return (
    <div className="admin-page">
      <div className="page-head">
        <h1>Beranda</h1>
        <button type="button" className="btn" onClick={() => void load()}><Icon icon={faRotate} /> Muat ulang</button>
      </div>
      {error && <Banner kind="error">Status sistem gagal dimuat: {error}</Banner>}
      {!s && !error && <Spinner label="Memuat status sistem" />}
      {s && (
        <>
          <section aria-labelledby="work-h" className="work">
            <h2 id="work-h">Pekerjaan</h2>
            {data ? (
              <div className="stat-tiles">
                <Link to={QUEUE} className="card stat-tile stat-link">
                  <span className="stat-value">{data.review_pending}</span>
                  <span><Icon icon={faClock} /> menunggu review</span>
                </Link>
                <Link to={QUEUE} className={`card stat-tile stat-link${data.retention_due_7d > 0 ? " is-warn" : ""}`}>
                  <span className="stat-value">{data.retention_due_7d}</span>
                  <span>akan terhapus retensi dalam 7 hari</span>
                </Link>
                <div className="card stat-tile"><span className="stat-value">{data.submissions}</span><span className="muted">submission</span></div>
                <div className="card stat-tile"><span className="stat-value">{data.accepted}</span><span className="muted">diterima</span></div>
                <div className="card stat-tile"><span className="stat-value">{data.rejected}</span><span className="muted">ditolak</span></div>
                {api?.auth_mode !== "none" && (
                  <div className="card stat-tile"><span className="stat-value">{data.users_active}</span><span className="muted">annotator aktif{data.users_blocked ? `, ${data.users_blocked} diblokir` : ""}</span></div>
                )}
              </div>
            ) : <Banner kind="warn">Ringkasan data tidak bisa dihitung: {partError(s.data)}</Banner>}
          </section>

          <section aria-labelledby="sys-h">
            <h2 id="sys-h">Status sistem</h2>
            <ul className="status-grid">
              {assess(s).map((c) => (
                <li key={c.key} className={`card status-card level-${c.level}`}>
                  <div className="status-head">
                    <span className="status-title"><Icon icon={CARD_ICON[c.key]} /> {c.title}</span>
                    <span className={`badge badge-${c.level === "ok" ? "ok" : c.level === "warn" ? "warn" : "danger"}`}>
                      <Icon icon={LEVEL_ICON[c.level]} /> {LEVEL_TEXT[c.level]}
                    </span>
                  </div>
                  <strong className="status-summary">{c.summary}</strong>
                  {c.meter !== undefined && (
                    <div className="meter" role="meter" aria-label="Pemakaian SSD" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(c.meter * 100)}>
                      <div style={{ width: `${Math.min(100, c.meter * 100)}%` }} />
                    </div>
                  )}
                  {c.lines.map((l) => <span key={l} className="muted">{l}</span>)}
                </li>
              ))}
            </ul>
            <p className="muted">
              Diperiksa {formatDateTime(s.checked_at)} · dimuat ulang tiap 30 detik
              {api && <> · API versi <code>{api.version}</code>, jalan sejak {formatDateTime(api.started_at)}, mode {api.auth_mode === "otp" ? "login OTP" : "tanpa login"}</>}
            </p>
          </section>
        </>
      )}
    </div>
  );
}
