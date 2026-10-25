import type { ReactNode } from "react";

// Komponen tampilan kecil yang dipakai di banyak halaman.

export function Banner({ kind = "info", children }: { kind?: "info" | "warn" | "error" | "ok"; children: ReactNode }) {
  return (
    <div className={`banner banner-${kind}`} role={kind === "error" || kind === "warn" ? "alert" : "status"}>
      {children}
    </div>
  );
}

export function ProgressBar({ value, label }: { value: number; label: string }) {
  const pct = Math.round(Math.min(1, Math.max(0, value)) * 100);
  return (
    <div className="progress" role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct}>
      <div style={{ width: `${pct}%` }} />
    </div>
  );
}

export function Spinner({ label = "Memuat" }: { label?: string }) {
  return (
    <span className="row" role="status">
      <span className="spinner" aria-hidden="true" />
      <span>{label}…</span>
    </span>
  );
}
