import { faArrowLeft } from "@fortawesome/free-solid-svg-icons/faArrowLeft";
import { faBan } from "@fortawesome/free-solid-svg-icons/faBan";
import { faEnvelope } from "@fortawesome/free-solid-svg-icons/faEnvelope";
import { faImages } from "@fortawesome/free-solid-svg-icons/faImages";
import { faLockOpen } from "@fortawesome/free-solid-svg-icons/faLockOpen";
import { faPhone } from "@fortawesome/free-solid-svg-icons/faPhone";
import { faRightLeft } from "@fortawesome/free-solid-svg-icons/faRightLeft";
import { faTrashCan } from "@fortawesome/free-solid-svg-icons/faTrashCan";
import { faUserXmark } from "@fortawesome/free-solid-svg-icons/faUserXmark";
import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Icon } from "../../components/Icon";
import { Banner, Spinner } from "../../components/ui";
import { isCode } from "../../lib/errors";
import { formatDateTime } from "../../lib/time";
import { adminApi } from "./api";
import { BlockedBadge } from "./AnnotatorsPage";
import { usePageTitle } from "./context";
import { adminErrorMessage } from "./errors";
import type { AdminGroup, AdminUser } from "./types";
import { ConfirmDelete } from "./ui/ConfirmDelete";
import { Dialog } from "../../components/Dialog";
import { ReasonDialog } from "./ui/ReasonDialog";
import { useToast } from "./ui/Toasts";

/** /admin/annotator/:id: profil + statistik, blokir/buka blokir, hapus data, hapus akun (spec-labels-admin §5.8). */
export function AnnotatorPage() {
  usePageTitle("Annotator");
  const { id = "" } = useParams();
  const toast = useToast();
  const navigate = useNavigate();
  const [u, setU] = useState<AdminUser>();
  const [error, setError] = useState<string>();
  const [dialog, setDialog] = useState<"block" | "unblock" | "delete" | "account" | null>(null);
  const [includeAccepted, setIncludeAccepted] = useState(false);
  const [busy, setBusy] = useState(false);
  const [groups, setGroups] = useState<AdminGroup[]>();
  const [groupId, setGroupId] = useState("");

  const load = useCallback(async () => {
    setError(undefined);
    try {
      setU(await adminApi.user(id));
    } catch (e) {
      setError(isCode(e, "NOT_FOUND") ? "Annotator tidak ditemukan." : adminErrorMessage(e));
    }
  }, [id]);

  useEffect(() => {
    void load();
  }, [load]);

  // pilihan kelompok untuk dipindah: hanya kelompok aktif
  useEffect(() => {
    let alive = true;
    adminApi.groups().then((g) => alive && setGroups(g.filter((x) => !x.archived)), () => alive && setGroups([]));
    return () => {
      alive = false;
    };
  }, []);

  if (error) return <div className="admin-page"><Banner kind="error">{error}</Banner><Link className="btn" to="/admin/annotator"><Icon icon={faArrowLeft} /> Daftar annotator</Link></div>;
  if (!u) return <div className="admin-page"><Spinner label="Memuat annotator" /></div>;

  const block = async (reason: string) => {
    await adminApi.block(u.id, reason);
    toast("ok", `${u.name} diblokir. Semua sesinya dicabut.`);
    setDialog(null);
    await load();
  };
  const unblock = async () => {
    setBusy(true);
    try {
      await adminApi.unblock(u.id);
      toast("ok", `Blokir ${u.name} dibuka. Ia bisa masuk lagi dengan password lamanya.`);
      setDialog(null);
      await load();
    } catch (e) {
      toast("error", adminErrorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const deleteData = async () => {
    const r = await adminApi.deleteUserData(u.id, includeAccepted);
    toast("warn", `${r.submissions} submission (${r.photos} foto, ${r.annotations} kotak) dihapus; file dibersihkan otomatis.`);
    setDialog(null);
    await load();
  };
  const deleteAccount = async () => {
    const r = await adminApi.deleteUser(u.id);
    toast(r.status === "deleted" ? "ok" : "warn", `Akun ${u.name} dihapus beserta ${r.submissions} submission (${r.photos} foto, ${r.annotations} kotak)${r.status === "deleted" ? "." : "; file dibersihkan otomatis."}`);
    navigate("/admin/annotator", { replace: true });
  };
  const moveGroup = async () => {
    const target = groups?.find((g) => String(g.id) === groupId);
    if (!target || busy) return;
    setBusy(true);
    try {
      await adminApi.setUserGroup(u.id, target.id);
      toast("ok", `${u.name} dipindah ke ${target.name}.`);
      setGroupId("");
      await load();
    } catch (e) {
      toast("error", adminErrorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const s = u.stats;
  const toDelete = includeAccepted ? s.submissions : s.submissions - s.accepted;

  return (
    <div className="admin-page">
      <div className="page-head"><Link className="btn" to="/admin/annotator"><Icon icon={faArrowLeft} /> Daftar</Link></div>
      <div className="detail-grid">
        <div className="stack">
          <div className="card stack">
            <h1 className="label-name profile-name">{u.name}</h1>
            <div className="row"><BlockedBadge blocked={u.blocked} /></div>
            {u.blocked && (
              <Banner kind="warn">Diblokir {u.blocked_at ? formatDateTime(u.blocked_at) : ""}{u.blocked_reason ? `. Alasan: ${u.blocked_reason}` : ""}</Banner>
            )}
            <a href={`mailto:${u.email}`} className="contact"><Icon icon={faEnvelope} /> {u.email}</a>
            <a href={`tel:+${u.phone}`} className="contact"><Icon icon={faPhone} /> {u.phone}</a>
            <dl className="meta">
              <dt>NIM</dt><dd>{u.nim ?? "-"}</dd>
              <dt>Kelompok</dt><dd>{u.group?.name ?? "-"}</dd>
              <dt>Terdaftar</dt><dd>{formatDateTime(u.created_at)}</dd>
              <dt>Login terakhir</dt><dd>{u.last_login_at ? formatDateTime(u.last_login_at) : "-"}</dd>
              <dt>Submission terakhir</dt><dd>{s.last_submission_at ? formatDateTime(s.last_submission_at) : "-"}</dd>
            </dl>
            <Link className="btn" to={`/admin/anotasi?user_id=${encodeURIComponent(u.id)}`}><Icon icon={faImages} /> Lihat submission-nya</Link>
          </div>
          <div className="stat-tiles">
            {([["Submission", s.submissions], ["Kotak", s.annotations], ["Diterima", s.accepted], ["Ditolak", s.rejected], ["Menunggu review", s.pending]] as const).map(([k, v]) => (
              <div key={k} className="card stat-tile"><span className="stat-value">{v}</span><span className="muted">{k}</span></div>
            ))}
          </div>
        </div>
        <aside className="detail-panel">
          <div className="card stack">
            <h2>Akses</h2>
            {u.blocked ? (
              <>
                <p className="muted">Membuka blokir membuat annotator bisa masuk lagi dengan password lamanya.</p>
                <button type="button" className="btn" onClick={() => setDialog("unblock")}><Icon icon={faLockOpen} /> Buka blokir</button>
              </>
            ) : (
              <>
                <p className="muted">Blokir mencabut semua sesinya dan menolak login berikutnya. Datanya tidak berubah.</p>
                <button type="button" className="btn btn-danger" onClick={() => setDialog("block")}><Icon icon={faBan} /> Blokir</button>
              </>
            )}
          </div>
          <div className="card stack">
            <h2>Kelompok</h2>
            <p className="muted">Pindahkan mahasiswa ke kelompok lain. Pendataan yang sudah ada tidak berubah.</p>
            <label className="field">
              Kelompok baru
              <select value={groupId} onChange={(e) => setGroupId(e.target.value)} disabled={!groups?.length}>
                <option value="">Pilih kelompok</option>
                {groups?.filter((g) => g.id !== u.group?.id).map((g) => <option key={g.id} value={String(g.id)}>{g.name}</option>)}
              </select>
            </label>
            <button type="button" className="btn" onClick={() => void moveGroup()} disabled={!groupId || busy}><Icon icon={faRightLeft} /> Pindahkan</button>
          </div>
          <div className="card stack">
            <h2>Hapus data</h2>
            <p className="muted">Hapus permanen submission annotator ini (akunnya tetap ada). Submission yang sudah diterima dipertahankan kecuali dipilih.</p>
            <button type="button" className="btn btn-danger" onClick={() => setDialog("delete")} disabled={s.submissions === 0}><Icon icon={faTrashCan} /> Hapus data annotator</button>
          </div>
          <div className="card stack">
            <h2>Hapus akun</h2>
            <p className="muted">Hapus akun beserta semua datanya, termasuk submission yang sudah diterima. Nomor HP-nya bisa dipakai mendaftar lagi; untuk mencegah itu, cukup blokir.</p>
            <button type="button" className="btn btn-danger" onClick={() => setDialog("account")}><Icon icon={faUserXmark} /> Hapus akun</button>
          </div>
        </aside>
      </div>

      {dialog === "block" && (
        <ReasonDialog title={`Blokir ${u.name}?`} label="Alasan blokir" danger confirmLabel={<><Icon icon={faBan} /> Blokir</>} onClose={() => setDialog(null)} onConfirm={block}>
          <p>Semua sesi aktifnya langsung dicabut. Login dengan password yang benar akan ditolak dengan pesan "Akun Anda diblokir"; mendaftar ulang dan lupa password dengan nomor ini juga tidak bisa.</p>
        </ReasonDialog>
      )}
      {dialog === "unblock" && (
        <Dialog title={`Buka blokir ${u.name}?`} onClose={() => setDialog(null)} busy={busy}
          footer={<>
            <button type="button" className="btn" onClick={() => setDialog(null)} disabled={busy}>Batal</button>
            <button type="button" className="btn btn-primary" onClick={() => void unblock()} disabled={busy}><Icon icon={faLockOpen} /> Buka blokir</button>
          </>}>
          <p>Annotator bisa masuk lagi dengan password lamanya. Sesi lama tetap dicabut.</p>
        </Dialog>
      )}
      {dialog === "delete" && (
        <ConfirmDelete title={`Hapus data ${u.name}?`} confirmLabel="Hapus data" onClose={() => setDialog(null)} onConfirm={deleteData}
          what={`${toDelete} submission milik ${u.name}${includeAccepted ? " (termasuk yang sudah diterima)" : ` (${s.accepted} yang sudah diterima dipertahankan)`}`}>
          <label className="check">
            <input type="checkbox" checked={includeAccepted} onChange={(e) => setIncludeAccepted(e.target.checked)} /> Ikut hapus submission yang sudah diterima
          </label>
        </ConfirmDelete>
      )}
      {dialog === "account" && (
        <ConfirmDelete title={`Hapus akun ${u.name}?`} confirmLabel="Hapus akun" onClose={() => setDialog(null)} onConfirm={deleteAccount}
          what={`akun ${u.name} (${u.phone}) dan ${s.submissions} submission miliknya, termasuk ${s.accepted} yang sudah diterima`}>
          <p>Semua sesinya langsung berakhir. Nomor ini bisa dipakai mendaftar akun baru.</p>
        </ConfirmDelete>
      )}
    </div>
  );
}
