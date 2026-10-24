import { faClock } from "@fortawesome/free-regular-svg-icons/faClock";
import { faCheck } from "@fortawesome/free-solid-svg-icons/faCheck";
import { faCheckDouble } from "@fortawesome/free-solid-svg-icons/faCheckDouble";
import { faDownload } from "@fortawesome/free-solid-svg-icons/faDownload";
import { faFilter } from "@fortawesome/free-solid-svg-icons/faFilter";
import { faMagnifyingGlass } from "@fortawesome/free-solid-svg-icons/faMagnifyingGlass";
import { faRotate } from "@fortawesome/free-solid-svg-icons/faRotate";
import { faTrashCan } from "@fortawesome/free-solid-svg-icons/faTrashCan";
import { faUserSecret } from "@fortawesome/free-solid-svg-icons/faUserSecret";
import { faUserSlash } from "@fortawesome/free-solid-svg-icons/faUserSlash";
import { faXmark } from "@fortawesome/free-solid-svg-icons/faXmark";
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { Icon } from "../../components/Icon";
import { ReviewBadge } from "../../components/ReviewBadge";
import { Banner, Spinner } from "../../components/ui";
import { formatDateTime } from "../../lib/time";
import { adminApi, exportJSONL, saveBlob } from "./api";
import { usePageTitle } from "./context";
import { adminErrorMessage } from "./errors";
import { daysUntil, exportFilename, filtersToParams, parseFilters, toQuery, type Filters } from "./filters";
import { listContext } from "./listContext";
import type { AdminLabel, ReviewStatus, SubmissionListItem } from "./types";
import { AnnotatedPhoto } from "./ui/AnnotatedPhoto";
import { ConfirmDelete } from "./ui/ConfirmDelete";
import { Dialog } from "./ui/Dialog";
import { ReasonDialog } from "./ui/ReasonDialog";
import { useToast } from "./ui/Toasts";
import { useIsPhone } from "./ui/useMediaQuery";

export const PAGE_SIZE = 24;
/** Batas aksi massal (hapus/review) per permintaan. */
export const MAX_BULK = 50;

const REVIEW_OPTIONS: [ReviewStatus | "", string][] = [["", "Semua status"], ["pending", "Menunggu review"], ["accepted", "Diterima"], ["rejected", "Ditolak"]];

/** /admin/anotasi: daftar submission terbaru dengan filter, cursor "Muat lagi", dan export. */
export function SubmissionsPage() {
  usePageTitle("Anotasi");
  const [sp, setSp] = useSearchParams();
  const search = sp.toString();
  const filters = useMemo(() => parseFilters(new URLSearchParams(search)), [search]);
  const [labels, setLabels] = useState<AdminLabel[]>([]);
  const [items, setItems] = useState<SubmissionListItem[]>();
  const [cursor, setCursor] = useState<string | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string>();
  const [exporting, setExporting] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [selectMode, setSelectMode] = useState(false);
  const [bulk, setBulk] = useState<"delete" | "reject" | null>(null);
  const [bulkBusy, setBulkBusy] = useState(false);
  const isPhone = useIsPhone();
  const toast = useToast();
  const abort = useRef<AbortController | null>(null);

  useEffect(() => {
    adminApi.labels().then(setLabels, () => {});
  }, []);

  const load = useCallback(async (after?: string) => {
    abort.current?.abort();
    const ac = new AbortController();
    abort.current = ac;
    setError(undefined);
    if (after) setLoadingMore(true);
    try {
      const page = await adminApi.submissions({ ...toQuery(filters), limit: PAGE_SIZE, cursor: after }, ac.signal);
      if (ac.signal.aborted) return;
      setItems((prev) => (after ? [...(prev ?? []), ...page.items] : page.items));
      setCursor(page.next_cursor || null);
    } catch (e) {
      if (e instanceof DOMException && e.name === "AbortError") return;
      setError(adminErrorMessage(e));
      if (!after) setItems([]);
    } finally {
      if (!ac.signal.aborted) setLoadingMore(false);
    }
  }, [filters]);

  useEffect(() => {
    setItems(undefined);
    setSelected(new Set());
    void load();
    return () => abort.current?.abort();
  }, [load]);

  // urutan daftar saat ini, untuk navigasi berikutnya/sebelumnya di halaman detail
  useEffect(() => {
    if (items) listContext.save(search, items.map((i) => i.id), cursor);
  }, [items, search, cursor]);

  const setFilters = (f: Filters) => setSp(filtersToParams(f), { replace: true });
  const colors = useMemo(() => {
    const m = new Map(labels.map((l) => [l.id, l.color]));
    for (const it of items ?? []) for (const l of it.labels) m.set(l.id, l.color);
    return m;
  }, [labels, items]);
  const isQueue = filters.review_status === "pending" && filters.sort === "oldest";
  const selecting = !isPhone || selectMode;
  const chosen = (items ?? []).filter((i) => selected.has(i.id));
  const toggle = (id: string) => setSelected((s) => {
    const n = new Set(s);
    if (n.has(id)) n.delete(id);
    else if (n.size < MAX_BULK) n.add(id);
    return n;
  });
  const allShown = !!items?.length && items.slice(0, MAX_BULK).every((i) => selected.has(i.id));
  const selectAll = () => setSelected(allShown ? new Set() : new Set((items ?? []).slice(0, MAX_BULK).map((i) => i.id)));
  const endSelect = () => {
    setSelected(new Set());
    setSelectMode(false);
  };
  /** Buang item yang sudah tidak ada (dihapus) dari daftar. */
  const dropItems = (ids: string[]) => {
    setItems((it) => it?.filter((x) => !ids.includes(x.id)));
    setSelected((s) => new Set([...s].filter((x) => !ids.includes(x))));
    listContext.drop(ids);
  };

  /** Terima/tolak terpilih (satu alasan untuk semua); hasil per item. */
  const bulkReview = async (status: ReviewStatus, reason: string | null) => {
    setBulkBusy(true);
    try {
      const r = await adminApi.reviewMany(chosen.map((c) => ({ id: c.id, version: c.version })), status, reason);
      const ok = new Set(r.results.filter((x) => x.result === "ok").map((x) => x.id));
      const missing = r.results.filter((x) => x.result === "not_found").map((x) => x.id);
      const conflicts = r.results.filter((x) => x.result === "version_conflict").length;
      // yang statusnya tidak lagi cocok dengan filter status (mis. antrean "menunggu") keluar dari daftar
      const leaves = filters.review_status && filters.review_status !== status;
      setItems((it) => it
        ?.filter((x) => !missing.includes(x.id) && !(leaves && ok.has(x.id)))
        .map((x) => (ok.has(x.id) ? { ...x, review: { status, reason, reviewed_at: new Date().toISOString() }, retention_deletes_at: status === "accepted" ? null : x.retention_deletes_at } : x)));
      if (missing.length) listContext.drop(missing);
      const parts = [`${ok.size} submission ${status === "accepted" ? "diterima" : "ditolak"}`];
      if (conflicts) parts.push(`${conflicts} baru diubah annotator (buka untuk melihat isi terbaru)`);
      if (missing.length) parts.push(`${missing.length} sudah tidak ada`);
      toast(conflicts || missing.length ? "warn" : "ok", parts.join("; ") + ".");
      setBulk(null);
      endSelect();
    } catch (e) {
      toast("error", adminErrorMessage(e));
      if (status === "rejected") throw e;
    } finally {
      setBulkBusy(false);
    }
  };

  const bulkDelete = async () => {
    const r = await adminApi.deleteSubmissions(chosen.map((c) => c.id));
    // yang sudah tidak ada (not_found) juga dibuang dari daftar
    const gone = r.results.map((x) => x.id);
    dropItems(gone);
    const deleted = r.results.filter((x) => x.status === "deleted").length;
    const pending = r.results.filter((x) => x.status === "cleanup_pending").length;
    const missing = r.results.filter((x) => x.status === "not_found").length;
    const parts = [`${deleted + pending} submission dihapus permanen`];
    if (pending) parts.push(`${pending} di antaranya sebagian filenya dibersihkan otomatis`);
    if (missing) parts.push(`${missing} sudah tidak ada`);
    toast(pending || missing ? "warn" : "ok", parts.join("; ") + ".");
    setBulk(null);
    endSelect();
  };

  return (
    <div className="admin-page">
      <div className="page-head">
        <h1>Anotasi</h1>
        <div className="row">
          <button type="button" className={`btn${isQueue ? " btn-primary" : ""}`} aria-pressed={isQueue}
            onClick={() => setFilters(isQueue ? {} : { review_status: "pending", sort: "oldest" })}>
            <Icon icon={faClock} /> Antrean review
          </button>
          <button type="button" className="btn" onClick={() => setExporting(true)}><Icon icon={faDownload} /> Export JSONL</button>
          {isPhone && !selectMode && <button type="button" className="btn" onClick={() => setSelectMode(true)}><Icon icon={faCheckDouble} /> Pilih</button>}
        </div>
      </div>
      {isQueue && <p className="muted">Antrean review: submission yang belum direview, paling dekat batas retensi lebih dulu.</p>}

      <FilterBar filters={filters} labels={labels} onChange={setFilters} />

      {error && <Banner kind="error">{error} <button type="button" className="btn btn-small" onClick={() => void load()}><Icon icon={faRotate} /> Coba lagi</button></Banner>}
      {!items && !error && <Spinner label="Memuat anotasi" />}
      {items?.length === 0 && !error && <div className="card"><p>Tidak ada submission yang cocok dengan filter ini.</p></div>}

      {items && items.length > 0 && selecting && (selected.size > 0 || selectMode) && (
        <div className={`bulk-bar${isPhone ? " bulk-bar-bottom" : ""}`} role="region" aria-label="Aksi untuk yang dipilih">
          <label className="check"><input type="checkbox" checked={allShown} onChange={selectAll} /> Pilih semua yang tampil</label>
          <span className="bulk-count" aria-live="polite">{selected.size} dipilih{selected.size >= MAX_BULK ? ` (maks ${MAX_BULK})` : ""}</span>
          <div className="row bulk-actions">
            <button type="button" className="btn btn-primary" disabled={!selected.size || bulkBusy} onClick={() => void bulkReview("accepted", null)}><Icon icon={faCheck} /> Terima terpilih</button>
            <button type="button" className="btn" disabled={!selected.size || bulkBusy} onClick={() => setBulk("reject")}><Icon icon={faXmark} /> Tolak terpilih</button>
            <button type="button" className="btn btn-danger" disabled={!selected.size} onClick={() => setBulk("delete")}><Icon icon={faTrashCan} /> Hapus terpilih</button>
            <button type="button" className="btn" onClick={endSelect}><Icon icon={faXmark} /> Batal</button>
          </div>
        </div>
      )}

      {items && items.length > 0 && (
        <ul className="sub-grid" aria-label="Daftar submission">
          {items.map((it) => (
            <SubmissionCard key={it.id} item={it} colors={colors}
              selectable={selecting} selected={selected.has(it.id)} onToggle={() => toggle(it.id)} />
          ))}
        </ul>
      )}
      {cursor && items && (
        <div className="load-more">
          <button type="button" className="btn" onClick={() => void load(cursor)} disabled={loadingMore}>
            {loadingMore ? <><span className="spinner" aria-hidden="true" /> Memuat…</> : "Muat lagi"}
          </button>
        </div>
      )}

      {exporting && <ExportDialog filters={filters} labels={labels} onClose={() => setExporting(false)} />}
      {bulk === "reject" && (
        <ReasonDialog title={`Tolak ${chosen.length} submission?`} label="Alasan penolakan (sama untuk semua, dibaca annotator)" danger
          confirmLabel={<><Icon icon={faXmark} /> Tolak terpilih</>} onClose={() => setBulk(null)} onConfirm={(r) => bulkReview("rejected", r)} />
      )}
      {bulk === "delete" && (
        <ConfirmDelete title={`Hapus ${chosen.length} submission?`} confirmLabel="Hapus terpilih" onClose={() => setBulk(null)} onConfirm={bulkDelete}
          what={`${chosen.length} submission (${chosen.reduce((n, c) => n + c.photo_count, 0)} foto, ${chosen.reduce((n, c) => n + c.annotation_count, 0)} kotak anotasi) dari ${owners(chosen)}`} />
      )}
    </div>
  );
}

export function AnnotatorLine({ a }: { a: SubmissionListItem["annotator"] }) {
  if (!a) return <span className="sub-who"><Icon icon={faUserSecret} /> Anonim</span>;
  return (
    <span className="sub-who">
      {a.blocked && <span className="badge badge-danger" title="Annotator diblokir"><Icon icon={faUserSlash} /> Diblokir</span>}
      <strong>{a.name}</strong> <span className="muted nowrap">{a.phone}</span>
    </span>
  );
}

/** "Budi, Siti, dan 3 lainnya" */
function owners(items: SubmissionListItem[]): string {
  const names = [...new Set(items.map((i) => i.annotator?.name ?? "Anonim"))];
  return names.length <= 2 ? names.join(" dan ") : `${names.slice(0, 2).join(", ")}, dan ${names.length - 2} lainnya`;
}

function SubmissionCard({ item, colors, selectable, selected, onToggle }: {
  item: SubmissionListItem; colors: Map<number, string>; selectable: boolean; selected: boolean; onToggle(): void;
}) {
  const c = item.cover;
  return (
    <li className={`card sub-card${selected ? " is-selected" : ""}`}>
      {selectable && (
        <label className="sub-check" title="Pilih">
          <input type="checkbox" checked={selected} onChange={onToggle}
            aria-label={`Pilih submission ${formatDateTime(item.created_at)}${item.annotator ? ` dari ${item.annotator.name}` : ""}`} />
        </label>
      )}
      <Link to={`/admin/anotasi/${item.id}`} className="sub-cover" aria-label={`Buka submission ${formatDateTime(item.created_at)}${item.annotator ? ` dari ${item.annotator.name}` : ""}`}>
        {c ? (
          <AnnotatedPhoto display src={c.url} width={c.width} height={c.height} alt="Foto sampul"
            boxes={c.annotations.map((a) => ({ labelId: a.label_id, x: a.x, y: a.y, w: a.w, h: a.h }))} colors={colors} />
        ) : <div className="sub-cover-empty">Tanpa foto</div>}
      </Link>
      <div className="sub-body">
        <AnnotatorLine a={item.annotator} />
        <span className="muted">{formatDateTime(item.created_at)} · {item.photo_count} foto · {item.annotation_count} kotak</span>
        {item.labels.length > 0 && (
          <ul className="chips" aria-label="Label">
            {item.labels.map((l) => (
              <li key={l.id} className="chip"><span className="label-dot" style={{ background: l.color }} aria-hidden="true" />{l.name} <span className="muted">{l.count}</span></li>
            ))}
          </ul>
        )}
        <div className="row">
          <ReviewBadge status={item.review.status} />
          {item.legacy_text_count > 0 && <span className="legacy-tag">{item.legacy_text_count} catatan lama</span>}
          {item.retention_deletes_at && <span className="badge badge-muted">terhapus dalam {daysUntil(item.retention_deletes_at)} hari</span>}
        </div>
        {item.review.status === "rejected" && item.review.reason && <p className="muted sub-reason">Alasan: {item.review.reason}</p>}
      </div>
    </li>
  );
}

function FilterBar({ filters, labels, onChange }: { filters: Filters; labels: AdminLabel[]; onChange(f: Filters): void }) {
  const isPhone = useIsPhone();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState(filters.q ?? "");
  useEffect(() => setQ(filters.q ?? ""), [filters.q]);
  const set = (p: Partial<Filters>) => onChange({ ...filters, ...p });
  const submitQ = (e: FormEvent) => {
    e.preventDefault();
    set({ q: q.trim() || undefined });
  };
  const labelName = (id: number) => labels.find((l) => l.id === id)?.name ?? `#${id}`;

  const chips: [string, () => void][] = [];
  if (filters.from) chips.push([`Dari ${filters.from}`, () => set({ from: undefined })]);
  if (filters.to) chips.push([`Sampai ${filters.to}`, () => set({ to: undefined })]);
  if (filters.label_id) chips.push([`Label: ${labelName(filters.label_id)}`, () => set({ label_id: undefined })]);
  if (filters.review_status) chips.push([REVIEW_OPTIONS.find(([v]) => v === filters.review_status)![1], () => set({ review_status: undefined, sort: undefined })]);
  if (filters.q) chips.push([`Cari: ${filters.q}`, () => set({ q: undefined })]);
  if (filters.user_id) chips.push(["Satu annotator", () => set({ user_id: undefined })]);
  if (filters.unlabeled) chips.push(["Masih ada catatan lama", () => set({ unlabeled: undefined })]);
  if (filters.sort === "oldest") chips.push(["Terlama dulu", () => set({ sort: undefined })]);

  const fields = (
    <form className="filter-bar" onSubmit={submitQ} role="search" aria-label="Filter anotasi">
      <label className="field">Dari
        <input type="date" value={filters.from ?? ""} max={filters.to} onChange={(e) => set({ from: e.target.value || undefined })} />
      </label>
      <label className="field">Sampai
        <input type="date" value={filters.to ?? ""} min={filters.from} onChange={(e) => set({ to: e.target.value || undefined })} />
      </label>
      <label className="field">Label
        <select value={filters.label_id ?? ""} onChange={(e) => set({ label_id: e.target.value ? Number(e.target.value) : undefined })}>
          <option value="">Semua label</option>
          {labels.map((l) => <option key={l.id} value={l.id}>{l.archived ? `${l.name} (diarsipkan)` : l.name}</option>)}
        </select>
      </label>
      <label className="field">Status review
        <select value={filters.review_status ?? ""} onChange={(e) => set({ review_status: (e.target.value || undefined) as ReviewStatus | undefined })}>
          {REVIEW_OPTIONS.map(([v, t]) => <option key={v} value={v}>{t}</option>)}
        </select>
      </label>
      <div className="field filter-q">
        <label htmlFor="filter-q">Annotator</label>
        <div className="row nowrap-row">
          <input id="filter-q" type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Nama atau nomor HP" />
          <button type="submit" className="btn" aria-label="Cari annotator" title="Cari annotator"><Icon icon={faMagnifyingGlass} /></button>
        </div>
      </div>
      <label className="check">
        <input type="checkbox" checked={!!filters.unlabeled} onChange={(e) => set({ unlabeled: e.target.checked || undefined })} /> Masih ada catatan lama
      </label>
    </form>
  );

  return (
    <div className="filters">
      {isPhone ? (
        <>
          <button type="button" className="btn" aria-expanded={open} aria-controls="filter-panel" onClick={() => setOpen((v) => !v)}>
            <Icon icon={faFilter} /> Filter{chips.length ? ` (${chips.length})` : ""}
          </button>
          {open && <div id="filter-panel" className="card">{fields}</div>}
        </>
      ) : <div className="card">{fields}</div>}
      {chips.length > 0 && (
        <ul className="chips filter-chips" aria-label="Filter aktif">
          {chips.map(([t, clear]) => (
            <li key={t} className="chip">
              {t}
              <button type="button" className="chip-x" aria-label={`Hapus filter ${t}`} title={`Hapus filter ${t}`} onClick={clear}><Icon icon={faXmark} /></button>
            </li>
          ))}
          <li><button type="button" className="link-button" onClick={() => onChange({})}>Hapus semua filter</button></li>
        </ul>
      )}
    </div>
  );
}

/** Export JSONL untuk rentang tanggal aktif; default hanya submission yang diterima (§5.6). */
function ExportDialog({ filters, labels, onClose }: { filters: Filters; labels: AdminLabel[]; onClose(): void }) {
  const toast = useToast();
  const [status, setStatus] = useState<ReviewStatus | "">("accepted");
  const [byLabel, setByLabel] = useState(!!filters.label_id);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const q = toQuery(filters);
  const label = labels.find((l) => l.id === filters.label_id);

  const run = async () => {
    setBusy(true);
    setError(undefined);
    try {
      const r = await exportJSONL({ from: q.from, to: q.to, review_status: status || undefined, label_id: byLabel ? filters.label_id : undefined });
      saveBlob(r.blob, exportFilename(filters));
      if (r.complete) toast("ok", `Export selesai: ${r.rows} baris (satu baris per foto).`);
      else toast("warn", `Export tidak lengkap (${r.rows} baris terbaca). Coba ulangi.`);
      onClose();
    } catch (e) {
      setError(adminErrorMessage(e));
      setBusy(false);
    }
  };

  return (
    <Dialog title="Export JSONL" onClose={onClose} busy={busy}
      footer={<>
        <button type="button" className="btn" onClick={onClose} disabled={busy}>Batal</button>
        <button type="button" className="btn btn-primary" onClick={() => void run()} disabled={busy}>
          {busy ? <><span className="spinner" aria-hidden="true" /> Mengunduh…</> : <><Icon icon={faDownload} /> Unduh</>}
        </button>
      </>}>
      <p>Rentang tanggal: <strong>{filters.from ?? "awal"}</strong> sampai <strong>{filters.to ?? "sekarang"}</strong> (dari filter aktif). Satu baris per foto, tanpa data pribadi annotator.</p>
      <label className="field">Status review
        <select value={status} onChange={(e) => setStatus(e.target.value as ReviewStatus | "")}>
          <option value="accepted">Hanya yang diterima (dataset)</option>
          <option value="">Semua status</option>
          <option value="pending">Menunggu review</option>
          <option value="rejected">Ditolak</option>
        </select>
      </label>
      {label && (
        <label className="check"><input type="checkbox" checked={byLabel} onChange={(e) => setByLabel(e.target.checked)} /> Hanya foto berlabel {label.name}</label>
      )}
      {error && <Banner kind="error">{error}</Banner>}
    </Dialog>
  );
}
