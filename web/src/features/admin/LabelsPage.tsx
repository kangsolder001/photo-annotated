import { faArrowDown } from "@fortawesome/free-solid-svg-icons/faArrowDown";
import { faArrowUp } from "@fortawesome/free-solid-svg-icons/faArrowUp";
import { faBoxArchive } from "@fortawesome/free-solid-svg-icons/faBoxArchive";
import { faBoxOpen } from "@fortawesome/free-solid-svg-icons/faBoxOpen";
import { faPen } from "@fortawesome/free-solid-svg-icons/faPen";
import { faPlus } from "@fortawesome/free-solid-svg-icons/faPlus";
import { faRotate } from "@fortawesome/free-solid-svg-icons/faRotate";
import { faTrashCan } from "@fortawesome/free-solid-svg-icons/faTrashCan";
import { useCallback, useEffect, useId, useState, type FormEvent } from "react";
import { Icon, IconButton } from "../../components/Icon";
import { Banner, Spinner } from "../../components/ui";
import { limits } from "../../config";
import { isHexColor } from "../../lib/color";
import { isCode } from "../../lib/errors";
import { normalizeText, textProblem, textProblemMessage } from "../../lib/text";
import { adminApi } from "./api";
import { usePageTitle } from "./context";
import { adminErrorMessage } from "./errors";
import type { AdminLabel, LabelReplace } from "./types";
import { Dialog } from "./ui/Dialog";
import { RowMenu } from "./ui/RowMenu";
import { useToast } from "./ui/Toasts";
import { useIsPhone } from "./ui/useMediaQuery";

/** Label aktif urut sort_order lalu nama; label arsip di bawah, urut nama. */
export function sortLabels(ls: AdminLabel[]): AdminLabel[] {
  const byName = (a: AdminLabel, b: AdminLabel) => a.name.localeCompare(b.name, "id");
  return [
    ...ls.filter((l) => !l.archived).sort((a, b) => a.sort_order - b.sort_order || byName(a, b)),
    ...ls.filter((l) => l.archived).sort(byName),
  ];
}

const replaceOf = (l: AdminLabel, p: Partial<LabelReplace> = {}): LabelReplace => ({
  name: l.name, description: l.description ?? null, color: l.color, archived: l.archived, ...p,
});

/** /admin/label: kelola label (spec-labels-admin §5.3, §7.2). */
export function LabelsPage() {
  usePageTitle("Label");
  const toast = useToast();
  const isPhone = useIsPhone();
  const [labels, setLabels] = useState<AdminLabel[]>();
  const [loadError, setLoadError] = useState<string>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState<number | null>(null);
  const [adding, setAdding] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState<AdminLabel | null>(null);

  const load = useCallback(async () => {
    setLoadError(undefined);
    try {
      setLabels(sortLabels(await adminApi.labels()));
    } catch (e) {
      setLoadError(adminErrorMessage(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const run = async (fn: () => Promise<void>) => {
    if (busy) return;
    setBusy(true);
    setError(undefined);
    try {
      await fn();
    } catch (e) {
      setError(adminErrorMessage(e));
      if (isCode(e, "LABEL_ORDER_STALE", "NOT_FOUND")) await load();
    } finally {
      setBusy(false);
    }
  };

  const active = labels?.filter((l) => !l.archived) ?? [];

  const move = (l: AdminLabel, dir: -1 | 1) => run(async () => {
    const ids = active.map((x) => x.id);
    const i = ids.indexOf(l.id);
    const j = i + dir;
    if (i < 0 || j < 0 || j >= ids.length) return;
    [ids[i], ids[j]] = [ids[j]!, ids[i]!];
    await adminApi.reorderLabels(ids);
    // urutan baru langsung tampil; sort_order mengikuti posisi
    const pos = new Map(ids.map((id, k) => [id, k + 1]));
    setLabels((ls) => sortLabels((ls ?? []).map((x) => (pos.has(x.id) ? { ...x, sort_order: pos.get(x.id)! } : x))));
  });

  const setArchived = (l: AdminLabel, archived: boolean) => run(async () => {
    await adminApi.updateLabel(l.id, replaceOf(l, { archived }));
    toast("ok", archived ? `Label "${l.name}" diarsipkan. Tidak muncul lagi di pilihan user.` : `Label "${l.name}" aktif kembali.`);
    await load();
  });

  const remove = (l: AdminLabel) => run(async () => {
    try {
      await adminApi.deleteLabel(l.id);
      toast("ok", `Label "${l.name}" dihapus.`);
      setConfirmDelete(null);
      await load();
    } catch (e) {
      setConfirmDelete(null);
      if (isCode(e, "LABEL_IN_USE")) await load();
      throw e;
    }
  });

  const rowActions = (l: AdminLabel, i: number) => [
    { label: "Ubah", icon: faPen, onSelect: () => setEditing(l.id) },
    ...(!l.archived ? [
      { label: "Naikkan urutan", icon: faArrowUp, onSelect: () => void move(l, -1), disabled: i === 0 },
      { label: "Turunkan urutan", icon: faArrowDown, onSelect: () => void move(l, 1), disabled: i === active.length - 1 },
    ] : []),
    l.archived
      ? { label: "Buka arsip", icon: faBoxOpen, onSelect: () => void setArchived(l, false) }
      : { label: "Arsipkan", icon: faBoxArchive, onSelect: () => void setArchived(l, true) },
    ...(l.usage_count === 0 ? [{ label: "Hapus", icon: faTrashCan, onSelect: () => setConfirmDelete(l), danger: true }] : []),
  ];

  return (
    <div className="admin-page">
      <div className="page-head">
        <h1>Label</h1>
        <div className="row">
          <button type="button" className="btn" onClick={() => void load()} disabled={busy}><Icon icon={faRotate} /> Muat ulang</button>
          {!adding && <button type="button" className="btn btn-primary" onClick={() => setAdding(true)}><Icon icon={faPlus} /> Tambah label</button>}
        </div>
      </div>
      <p className="muted">Label yang dipilih annotator untuk tiap kotak. Label yang sudah dipakai tidak bisa dihapus, hanya diarsipkan: tidak muncul lagi di pilihan user, tetapi tetap tampil di anotasi lama dan export.</p>

      {adding && (
        <div className="card">
          <h2>Label baru</h2>
          <LabelForm
            submitLabel="Tambah"
            onCancel={() => setAdding(false)}
            onSubmit={async (v) => {
              const l = await adminApi.createLabel({ name: v.name, description: v.description ?? undefined, ...(v.color ? { color: v.color } : {}) });
              toast("ok", `Label "${l.name}" ditambahkan.`);
              setAdding(false);
              await load();
            }}
          />
        </div>
      )}

      {error && <Banner kind="error">{error}</Banner>}
      {loadError && (
        <Banner kind="error">{loadError} <button type="button" className="btn btn-small" onClick={() => void load()}>Coba lagi</button></Banner>
      )}
      {!labels && !loadError && <Spinner label="Memuat label" />}
      {labels?.length === 0 && <div className="card"><p>Belum ada label. Annotator tidak bisa mengirim anotasi sampai ada minimal satu label.</p></div>}

      {labels && labels.length > 0 && (isPhone ? (
        <ul className="card-list" aria-label="Daftar label">
          {labels.map((l) => {
            const i = active.indexOf(l);
            return (
              <li key={l.id} className={`card label-card${l.archived ? " is-archived" : ""}`}>
                {editing === l.id ? (
                  <LabelForm label={l} submitLabel="Simpan" onCancel={() => setEditing(null)} onSubmit={async (v) => {
                    await adminApi.updateLabel(l.id, replaceOf(l, { name: v.name, description: v.description, color: v.color ?? l.color }));
                    toast("ok", "Label disimpan.");
                    setEditing(null);
                    await load();
                  }} />
                ) : (
                  <>
                    <div className="label-card-head">
                      <span className="swatch" style={{ background: l.color }} aria-hidden="true" />
                      <strong className="label-name">{l.name}</strong>
                      <RowMenu label={`Aksi label ${l.name}`} actions={rowActions(l, i)} />
                    </div>
                    {l.description && <p className="muted">{l.description}</p>}
                    <div className="row muted">
                      <span>Dipakai {l.usage_count} kotak</span>
                      <StatusBadge archived={l.archived} />
                      {!l.archived && <span>Urutan {i + 1}</span>}
                    </div>
                  </>
                )}
              </li>
            );
          })}
        </ul>
      ) : (
        <div className="table-wrap">
          <table className="admin-table">
            <thead>
              <tr>
                <th scope="col">Urutan</th>
                <th scope="col">Warna</th>
                <th scope="col">Nama</th>
                <th scope="col" className="col-secondary">Deskripsi</th>
                <th scope="col" className="num">Dipakai</th>
                <th scope="col">Status</th>
                <th scope="col"><span className="visually-hidden">Aksi</span></th>
              </tr>
            </thead>
            <tbody>
              {labels.map((l) => {
                const i = active.indexOf(l);
                if (editing === l.id) {
                  return (
                    <tr key={l.id}>
                      <td colSpan={7}>
                        <LabelForm label={l} submitLabel="Simpan" onCancel={() => setEditing(null)} onSubmit={async (v) => {
                          await adminApi.updateLabel(l.id, replaceOf(l, { name: v.name, description: v.description, color: v.color ?? l.color }));
                          toast("ok", "Label disimpan.");
                          setEditing(null);
                          await load();
                        }} />
                      </td>
                    </tr>
                  );
                }
                return (
                  <tr key={l.id} className={l.archived ? "is-archived" : undefined}>
                    <td>
                      {!l.archived && (
                        <span className="order-btns">
                          <IconButton icon={faArrowUp} label={`Naikkan ${l.name}`} disabled={busy || i === 0} onClick={() => void move(l, -1)} />
                          <IconButton icon={faArrowDown} label={`Turunkan ${l.name}`} disabled={busy || i === active.length - 1} onClick={() => void move(l, 1)} />
                        </span>
                      )}
                    </td>
                    <td className="nowrap"><span className="swatch" style={{ background: l.color }} aria-hidden="true" /> <code className="muted">{l.color}</code></td>
                    <td className="label-name">{l.name}</td>
                    <td className="col-secondary muted">{l.description}</td>
                    <td className="num">{l.usage_count}</td>
                    <td><StatusBadge archived={l.archived} /></td>
                    <td>
                      <div className="row-actions">
                        <button type="button" className="btn btn-small" onClick={() => setEditing(l.id)} disabled={busy}><Icon icon={faPen} /> Ubah</button>
                        {l.archived ? (
                          <button type="button" className="btn btn-small" onClick={() => void setArchived(l, false)} disabled={busy}><Icon icon={faBoxOpen} /> Buka arsip</button>
                        ) : l.usage_count > 0 ? (
                          <button type="button" className="btn btn-small" onClick={() => void setArchived(l, true)} disabled={busy}><Icon icon={faBoxArchive} /> Arsipkan</button>
                        ) : (
                          <>
                            <button type="button" className="btn btn-small" onClick={() => void setArchived(l, true)} disabled={busy}><Icon icon={faBoxArchive} /> Arsipkan</button>
                            <button type="button" className="btn btn-small btn-danger" onClick={() => setConfirmDelete(l)} disabled={busy}><Icon icon={faTrashCan} /> Hapus</button>
                          </>
                        )}
                        {l.archived && l.usage_count === 0 && (
                          <button type="button" className="btn btn-small btn-danger" onClick={() => setConfirmDelete(l)} disabled={busy}><Icon icon={faTrashCan} /> Hapus</button>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ))}

      {confirmDelete && (
        <Dialog title="Hapus label?" onClose={() => setConfirmDelete(null)} busy={busy}
          footer={<>
            <button type="button" className="btn" onClick={() => setConfirmDelete(null)} disabled={busy}>Batal</button>
            <button type="button" className="btn btn-danger-solid" onClick={() => void remove(confirmDelete)} disabled={busy}>
              <Icon icon={faTrashCan} /> Hapus label
            </button>
          </>}>
          <p>Label <strong>{confirmDelete.name}</strong> belum pernah dipakai dan akan dihapus. Id label ini tidak akan dipakai ulang.</p>
        </Dialog>
      )}
    </div>
  );
}

function StatusBadge({ archived }: { archived: boolean }) {
  return archived
    ? <span className="badge badge-muted"><Icon icon={faBoxArchive} /> Diarsipkan</span>
    : <span className="badge badge-ok">Aktif</span>;
}

interface FormValue { name: string; description: string | null; color: string | null }

/** Isian tambah/ubah label. Peringatan bila mengganti nama label yang sudah dipakai. */
export function LabelForm(props: {
  label?: AdminLabel;
  submitLabel: string;
  onSubmit(v: FormValue): Promise<void>;
  onCancel(): void;
}) {
  const id = useId();
  const l = props.label;
  const [name, setName] = useState(l?.name ?? "");
  const [description, setDescription] = useState(l?.description ?? "");
  const [autoColor, setAutoColor] = useState(!l);
  const [color, setColor] = useState(l?.color ?? "#e4572e");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  const nameP = textProblem(name, limits.maxLabelName);
  const descP = textProblem(description, limits.maxLabelDescription, { optional: true });
  const hex = color.trim().toLowerCase();
  const colorBad = !autoColor && !isHexColor(hex);
  const renamed = !!l && l.usage_count > 0 && normalizeText(name) !== l.name;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (nameP || descP || colorBad || busy) return;
    setBusy(true);
    setError(undefined);
    try {
      await props.onSubmit({
        name: normalizeText(name),
        description: normalizeText(description) || null,
        color: autoColor ? (l?.color ?? null) : hex,
      });
    } catch (err) {
      setError(adminErrorMessage(err));
      setBusy(false);
    }
  };

  return (
    <form className="label-form" onSubmit={submit} noValidate>
      <div className="field">
        <label htmlFor={`${id}-n`}>Nama</label>
        <input id={`${id}-n`} value={name} onChange={(e) => setName(e.target.value)} maxLength={limits.maxLabelName * 2} required autoFocus
          aria-invalid={touched && !!nameP} aria-describedby={touched && nameP ? `${id}-name` : undefined} />
        {touched && nameP && <span id={`${id}-name`} className="field-error">{textProblemMessage(nameP, "Nama", limits.maxLabelName)}</span>}
      </div>
      {renamed && (
        <Banner kind="warn">
          Label ini dipakai di {l.usage_count} kotak. Nama baru langsung berlaku di semua anotasi itu dan di export berikutnya.
          Bila artinya berubah (mis. "Retak" menjadi "Penyok"), buat label baru dan arsipkan yang lama.
        </Banner>
      )}
      <div className="field">
        <label htmlFor={`${id}-d`}>Deskripsi</label>
        <span className="hint">Opsional. Tampil sebagai petunjuk di pemilih label user.</span>
        <input id={`${id}-d`} value={description} onChange={(e) => setDescription(e.target.value)} maxLength={limits.maxLabelDescription * 2}
          aria-invalid={touched && !!descP} />
        {touched && descP && <span className="field-error">{textProblemMessage(descP, "Deskripsi", limits.maxLabelDescription)}</span>}
      </div>
      <fieldset className="field color-field">
        <legend>Warna</legend>
        {!l && (
          <label className="check">
            <input type="checkbox" checked={autoColor} onChange={(e) => setAutoColor(e.target.checked)} /> Pilih otomatis dari palet
          </label>
        )}
        {(!autoColor || l) && (
          <div className="row">
            <input type="color" value={isHexColor(hex) ? hex : "#000000"} onChange={(e) => { setColor(e.target.value); setAutoColor(false); }} aria-label="Pilih warna" className="color-input" />
            <input value={color} onChange={(e) => { setColor(e.target.value); setAutoColor(false); }} aria-label="Kode warna" maxLength={7} className="color-hex"
              aria-invalid={touched && colorBad} />
            <span className="swatch swatch-lg" style={{ background: isHexColor(hex) ? hex : "transparent" }} aria-hidden="true" />
          </div>
        )}
        {touched && colorBad && <span className="field-error">Warna harus berbentuk #rrggbb.</span>}
      </fieldset>
      {error && <Banner kind="error">{error}</Banner>}
      <div className="row">
        <button type="submit" className="btn btn-primary" disabled={busy}>{busy ? <><span className="spinner" aria-hidden="true" /> Menyimpan…</> : props.submitLabel}</button>
        <button type="button" className="btn" onClick={props.onCancel} disabled={busy}>Batal</button>
      </div>
    </form>
  );
}
