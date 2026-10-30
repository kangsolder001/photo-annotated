import { faBoxArchive } from "@fortawesome/free-solid-svg-icons/faBoxArchive";
import { faBoxOpen } from "@fortawesome/free-solid-svg-icons/faBoxOpen";
import { faPen } from "@fortawesome/free-solid-svg-icons/faPen";
import { faPlus } from "@fortawesome/free-solid-svg-icons/faPlus";
import { faRotate } from "@fortawesome/free-solid-svg-icons/faRotate";
import { faTrashCan } from "@fortawesome/free-solid-svg-icons/faTrashCan";
import { useCallback, useEffect, useId, useState, type FormEvent } from "react";
import { Dialog } from "../../components/Dialog";
import { Icon } from "../../components/Icon";
import { Banner, Spinner } from "../../components/ui";
import { isCode } from "../../lib/errors";
import { normalizeText, textProblem, textProblemMessage } from "../../lib/text";
import { adminApi } from "./api";
import { usePageTitle } from "./context";
import { adminErrorMessage } from "./errors";
import type { AdminGroup } from "./types";
import { RowMenu } from "./ui/RowMenu";
import { useToast } from "./ui/Toasts";
import { useIsPhone } from "./ui/useMediaQuery";

/** Panjang nama kelompok (spec-pendataan §5.5). */
const MAX_GROUP_NAME = 100;

/** Kelompok aktif urut nama, arsip di bawah. */
export function sortGroups(gs: AdminGroup[]): AdminGroup[] {
  const byName = (a: AdminGroup, b: AdminGroup) => a.name.localeCompare(b.name, "id");
  return [...gs.filter((g) => !g.archived).sort(byName), ...gs.filter((g) => g.archived).sort(byName)];
}

/** /admin/kelompok: kelola daftar kelompok mahasiswa (spec-pendataan §5.5, §7.2). Pola sama dengan label. */
export function GroupsPage() {
  usePageTitle("Kelompok");
  const toast = useToast();
  const isPhone = useIsPhone();
  const [groups, setGroups] = useState<AdminGroup[]>();
  const [loadError, setLoadError] = useState<string>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState<number | null>(null);
  const [adding, setAdding] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState<AdminGroup | null>(null);

  const load = useCallback(async () => {
    setLoadError(undefined);
    try {
      setGroups(sortGroups(await adminApi.groups()));
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
      if (isCode(e, "GROUP_IN_USE", "NOT_FOUND")) await load();
    } finally {
      setBusy(false);
    }
  };

  const setArchived = (g: AdminGroup, archived: boolean) => run(async () => {
    await adminApi.updateGroup(g.id, { name: g.name, archived });
    toast("ok", archived ? `Kelompok "${g.name}" diarsipkan. Tidak muncul lagi di pilihan registrasi.` : `Kelompok "${g.name}" aktif kembali.`);
    await load();
  });

  const remove = (g: AdminGroup) => run(async () => {
    setConfirmDelete(null);
    await adminApi.deleteGroup(g.id);
    toast("ok", `Kelompok "${g.name}" dihapus.`);
    await load();
  });

  const rename = (g: AdminGroup) => async (name: string) => {
    await adminApi.updateGroup(g.id, { name, archived: g.archived });
    toast("ok", "Kelompok disimpan.");
    setEditing(null);
    await load();
  };

  const actions = (g: AdminGroup) => [
    { label: "Ubah nama", icon: faPen, onSelect: () => setEditing(g.id) },
    g.archived
      ? { label: "Aktifkan", icon: faBoxOpen, onSelect: () => void setArchived(g, false) }
      : { label: "Arsipkan", icon: faBoxArchive, onSelect: () => void setArchived(g, true) },
    ...(g.user_count === 0 ? [{ label: "Hapus", icon: faTrashCan, onSelect: () => setConfirmDelete(g), danger: true }] : []),
  ];

  return (
    <div className="admin-page">
      <div className="page-head">
        <h1>Kelompok</h1>
        <div className="row">
          <button type="button" className="btn" onClick={() => void load()} disabled={busy}><Icon icon={faRotate} /> Muat ulang</button>
          {!adding && <button type="button" className="btn btn-primary" onClick={() => setAdding(true)}><Icon icon={faPlus} /> Tambah kelompok</button>}
        </div>
      </div>
      <p className="muted">Pilihan kelompok di form registrasi mahasiswa. Kelompok yang sudah dipakai akun tidak bisa dihapus, hanya diarsipkan: tidak muncul lagi di registrasi, tetapi tetap tercatat di akun lama.</p>

      {adding && (
        <div className="card">
          <h2>Kelompok baru</h2>
          <GroupForm submitLabel="Tambah" onCancel={() => setAdding(false)} onSubmit={async (name) => {
            const g = await adminApi.createGroup({ name });
            toast("ok", `Kelompok "${g.name}" ditambahkan.`);
            setAdding(false);
            await load();
          }} />
        </div>
      )}

      {error && <Banner kind="error">{error}</Banner>}
      {loadError && <Banner kind="error">{loadError} <button type="button" className="btn btn-small" onClick={() => void load()}>Coba lagi</button></Banner>}
      {!groups && !loadError && <Spinner label="Memuat kelompok" />}
      {groups?.length === 0 && <div className="card"><p>Belum ada kelompok. Mahasiswa tidak bisa mendaftar sampai ada minimal satu kelompok aktif.</p></div>}

      {groups && groups.length > 0 && (isPhone ? (
        <ul className="card-list" aria-label="Daftar kelompok">
          {groups.map((g) => (
            <li key={g.id} className={`card label-card${g.archived ? " is-archived" : ""}`}>
              {editing === g.id ? (
                <GroupForm group={g} submitLabel="Simpan" onCancel={() => setEditing(null)} onSubmit={rename(g)} />
              ) : (
                <>
                  <div className="label-card-head">
                    <strong className="label-name">{g.name}</strong>
                    <RowMenu label={`Aksi kelompok ${g.name}`} actions={actions(g)} />
                  </div>
                  <div className="row muted">
                    <span>{g.user_count} mahasiswa</span>
                    <StatusBadge archived={g.archived} />
                  </div>
                </>
              )}
            </li>
          ))}
        </ul>
      ) : (
        <div className="table-wrap">
          <table className="admin-table">
            <thead>
              <tr>
                <th scope="col">Nama</th>
                <th scope="col" className="num">Mahasiswa</th>
                <th scope="col">Status</th>
                <th scope="col"><span className="visually-hidden">Aksi</span></th>
              </tr>
            </thead>
            <tbody>
              {groups.map((g) =>
                editing === g.id ? (
                  <tr key={g.id}>
                    <td colSpan={4}><GroupForm group={g} submitLabel="Simpan" onCancel={() => setEditing(null)} onSubmit={rename(g)} /></td>
                  </tr>
                ) : (
                  <tr key={g.id} className={g.archived ? "is-archived" : undefined}>
                    <td className="label-name">{g.name}</td>
                    <td className="num">{g.user_count}</td>
                    <td><StatusBadge archived={g.archived} /></td>
                    <td>
                      <div className="row-actions">
                        <button type="button" className="btn btn-small" onClick={() => setEditing(g.id)} disabled={busy}><Icon icon={faPen} /> Ubah nama</button>
                        {g.archived ? (
                          <button type="button" className="btn btn-small" onClick={() => void setArchived(g, false)} disabled={busy}><Icon icon={faBoxOpen} /> Aktifkan</button>
                        ) : (
                          <button type="button" className="btn btn-small" onClick={() => void setArchived(g, true)} disabled={busy}><Icon icon={faBoxArchive} /> Arsipkan</button>
                        )}
                        {g.user_count === 0 && (
                          <button type="button" className="btn btn-small btn-danger" onClick={() => setConfirmDelete(g)} disabled={busy}><Icon icon={faTrashCan} /> Hapus</button>
                        )}
                      </div>
                    </td>
                  </tr>
                ),
              )}
            </tbody>
          </table>
        </div>
      ))}

      {confirmDelete && (
        <Dialog title="Hapus kelompok?" onClose={() => setConfirmDelete(null)} busy={busy}
          footer={<>
            <button type="button" className="btn" onClick={() => setConfirmDelete(null)} disabled={busy}>Batal</button>
            <button type="button" className="btn btn-danger-solid" onClick={() => void remove(confirmDelete)} disabled={busy}>
              <Icon icon={faTrashCan} /> Hapus kelompok
            </button>
          </>}>
          <p>Kelompok <strong>{confirmDelete.name}</strong> belum dipakai akun mana pun dan akan dihapus.</p>
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

/** Isian tambah/ubah nama kelompok. */
function GroupForm(props: { group?: AdminGroup; submitLabel: string; onSubmit(name: string): Promise<void>; onCancel(): void }) {
  const id = useId();
  const [name, setName] = useState(props.group?.name ?? "");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const problem = textProblem(name, MAX_GROUP_NAME);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (problem || busy) return;
    setBusy(true);
    setError(undefined);
    try {
      await props.onSubmit(normalizeText(name));
    } catch (err) {
      setError(adminErrorMessage(err));
      setBusy(false);
    }
  };

  return (
    <form className="label-form" onSubmit={submit} noValidate>
      <div className="field">
        <label htmlFor={id}>Nama kelompok</label>
        <span className="hint">Contoh: Kelompok 4 – CL-05</span>
        <input id={id} value={name} onChange={(e) => setName(e.target.value)} maxLength={MAX_GROUP_NAME * 2} required autoFocus
          aria-invalid={touched && !!problem} aria-describedby={touched && problem ? `${id}-err` : undefined} />
        {touched && problem && <span id={`${id}-err`} className="field-error">{textProblemMessage(problem, "Nama kelompok", MAX_GROUP_NAME)}</span>}
      </div>
      {error && <Banner kind="error">{error}</Banner>}
      <div className="row">
        <button type="submit" className="btn btn-primary" disabled={busy}>{busy ? <><span className="spinner" aria-hidden="true" /> Menyimpan…</> : props.submitLabel}</button>
        <button type="button" className="btn" onClick={props.onCancel} disabled={busy}>Batal</button>
      </div>
    </form>
  );
}
