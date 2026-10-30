import { useState, type ReactNode } from "react";
import { faClipboardList } from "@fortawesome/free-solid-svg-icons/faClipboardList";
import { faRightFromBracket } from "@fortawesome/free-solid-svg-icons/faRightFromBracket";
import { Link, NavLink, useNavigate } from "react-router-dom";
import { api } from "../api/endpoints";
import { config } from "../config";
import { useSessionUser } from "../features/auth/useSession";
import { session } from "../lib/session";
import { Dialog } from "./Dialog";
import { Icon } from "./Icon";
import { ThemeSwitcher } from "./ThemeSwitcher";

export function Layout({ children }: { children: ReactNode }) {
  const user = useSessionUser();
  const navigate = useNavigate();
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const logout = async () => {
    setBusy(true);
    try {
      await api.logout();
    } catch {
      // token mungkin sudah tidak berlaku; sesi lokal tetap dihapus
    }
    session.expire();
    setBusy(false);
    setConfirming(false);
    navigate("/masuk");
  };
  const showNav = config.authMode === "none" || user;
  return (
    <>
      <header className="app-header">
        <Link to="/" className="brand"><Icon icon={faClipboardList} /> Pendataan Sampah KKN</Link>
        {showNav && (
          <nav aria-label="Navigasi utama">
            <NavLink to="/" end>Beranda</NavLink>
            {user && <button type="button" onClick={() => setConfirming(true)} title={user.name}>Keluar</button>}
          </nav>
        )}
        <ThemeSwitcher />
      </header>
      <main>{children}</main>
      {confirming && user && (
        <Dialog title="Keluar dari akun?" compact onClose={() => setConfirming(false)} busy={busy}
          footer={<>
            <button type="button" className="btn" onClick={() => setConfirming(false)} disabled={busy}>Batal</button>
            <button type="button" className="btn btn-primary" onClick={() => void logout()} disabled={busy}><Icon icon={faRightFromBracket} /> Keluar</button>
          </>}>
          <p>Anda masuk sebagai <strong>{user.name}</strong>. Setelah keluar, masuk lagi dengan nomor HP dan password.</p>
        </Dialog>
      )}
    </>
  );
}
