import { partError, type SystemStatus } from "./types";

// Penilaian kartu status sistem (spec-labels-admin §5.10, §7.2). Ambang sama dengan alert Grafana:
// SSD > 80%, sisa < min_free_bytes, kanal WhatsApp tidak tersedia, antrean > 80%, OTP harian > 80%,
// suhu > 75 °C. Bagian yang gagal dihitung ({ error }) = bahaya, bagian lain tetap tampil.

export type Level = "ok" | "warn" | "danger";
export type CardKey = "database" | "storage" | "uploads" | "whatsapp" | "cleanup" | "host";

export interface StatusCard {
  key: CardKey;
  title: string;
  level: Level;
  /** Ringkasan satu baris (ikut status, tidak hanya warna). */
  summary: string;
  lines: string[];
  /** 0..1 untuk bar pemakaian (SSD). */
  meter?: number;
}

const pct = (a: number, b: number) => (b > 0 ? a / b : 0);
const worst = (...ls: Level[]): Level => (ls.includes("danger") ? "danger" : ls.includes("warn") ? "warn" : "ok");

export function formatBytes(n: number): string {
  if (n >= 1e12) return `${(n / 1e12).toFixed(1)} TB`;
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)} GB`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(0)} MB`;
  return `${Math.round(n / 1e3)} kB`;
}

const fmtPct = (v: number) => `${Math.round(v * 100)}%`;

function failed(key: CardKey, title: string, err: string): StatusCard {
  return { key, title, level: "danger", summary: "Tidak bisa dihitung", lines: [err] };
}

export function assess(s: SystemStatus): StatusCard[] {
  const cards: StatusCard[] = [];

  const db = s.database;
  const dbErr = partError(db);
  if (dbErr || "error" in db) cards.push(failed("database", "Database", dbErr ?? ""));
  else cards.push({ key: "database", title: "Database", level: db.ok ? "ok" : "danger", summary: db.ok ? "Terhubung" : "Gagal", lines: [`Latensi ${db.latency_ms} ms`] });

  const st = s.storage;
  const stErr = partError(st);
  if (stErr || "error" in st) cards.push(failed("storage", "SSD", stErr ?? ""));
  else if (!st.available) cards.push({ key: "storage", title: "SSD", level: "danger", summary: "Tidak tersedia (tidak ter-mount?)", lines: [] });
  else {
    const used = st.used_pct / 100;
    const level: Level = st.free_bytes < st.min_free_bytes ? "danger" : st.used_pct > 80 ? "warn" : "ok";
    cards.push({
      key: "storage", title: "SSD", level, meter: used,
      summary: level === "danger" ? "Hampir penuh: upload ditolak" : level === "warn" ? `Terpakai ${st.used_pct.toFixed(1)}%` : `Terpakai ${st.used_pct.toFixed(1)}%`,
      lines: [`Sisa ${formatBytes(st.free_bytes)} dari ${formatBytes(st.total_bytes)} (minimum ${formatBytes(st.min_free_bytes)})`, `${st.photos_pending} foto menunggu dikirim`],
    });
  }

  const up = s.uploads;
  const upErr = partError(up);
  if (upErr || "error" in up) cards.push(failed("uploads", "Upload aktif", upErr ?? ""));
  else {
    const r = pct(up.active, up.max);
    cards.push({ key: "uploads", title: "Upload aktif", level: r > 0.8 ? "warn" : "ok", summary: `${up.active} dari ${up.max} slot`, lines: [] });
  }

  const wa = s.whatsapp;
  const waErr = partError(wa);
  if (waErr || "error" in wa) cards.push(failed("whatsapp", "WhatsApp / OTP", waErr ?? ""));
  else {
    const q = pct(wa.queued, wa.max_queue), d = pct(wa.sent_24h, wa.daily_cap);
    const level = worst(wa.otp_available ? "ok" : "danger", q > 0.8 ? "warn" : "ok", d > 0.8 ? "warn" : "ok");
    cards.push({
      key: "whatsapp", title: "WhatsApp / OTP", level,
      summary: !wa.otp_available ? "Kanal tidak tersedia" : level === "warn" ? "Mendekati batas" : "Tersedia",
      lines: [`Antrean ${wa.queued} dari ${wa.max_queue} (${fmtPct(q)})`, `Terkirim 24 jam ${wa.sent_24h} dari batas ${wa.daily_cap} (${fmtPct(d)}), gagal ${wa.failed_24h}`],
    });
  }

  const cl = s.cleanup;
  const clErr = partError(cl);
  if (clErr || "error" in cl) cards.push(failed("cleanup", "Cleanup", clErr ?? ""));
  else {
    const level: Level = !cl.last_ok ? "warn" : "ok";
    cards.push({
      key: "cleanup", title: "Cleanup", level, summary: cl.last_run_at ? (cl.last_ok ? "Putaran terakhir berhasil" : "Putaran terakhir gagal") : "Belum pernah berjalan",
      lines: [`${cl.files_pending_delete} file menunggu dihapus`],
    });
  }

  const h = s.host;
  const hErr = partError(h);
  if (hErr || "error" in h) cards.push({ key: "host", title: "Suhu CPU", level: "warn", summary: "Tidak terbaca", lines: hErr ? [hErr] : [] });
  else if (h.cpu_temp_c == null) cards.push({ key: "host", title: "Suhu CPU", level: "ok", summary: "Tidak terbaca dari container", lines: [] });
  else cards.push({ key: "host", title: "Suhu CPU", level: h.cpu_temp_c > 75 ? "warn" : "ok", summary: `${h.cpu_temp_c.toFixed(1)} °C`, lines: [] });

  return cards;
}

export const LEVEL_TEXT: Record<Level, string> = { ok: "OK", warn: "Waspada", danger: "Bahaya" };
