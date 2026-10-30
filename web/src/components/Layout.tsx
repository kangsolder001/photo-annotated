import { useEffect, useState, type ReactNode } from "react";
import { faBars } from "@fortawesome/free-solid-svg-icons/faBars";
import { faClipboardList } from "@fortawesome/free-solid-svg-icons/faClipboardList";
import { faRightFromBracket } from "@fortawesome/free-solid-svg-icons/faRightFromBracket";
import { faXmark } from "@fortawesome/free-solid-svg-icons/faXmark";
import { Link, NavLink, useNavigate } from "react-router-dom";
import { api } from "../api/endpoints";
import { config } from "../config";
import { useSessionUser } from "../features/auth/useSession";
import { session } from "../lib/session";
import { Dialog } from "./Dialog";
import { Icon, IconButton } from "./Icon";
import { ThemeSwitcher } from "./ThemeSwitcher";

export function Layout({ children }: { children: ReactNode }) {
  const user = useSessionUser();
  const navigate = useNavigate();
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  // HP: menu (navigasi + tema) dilipat ke tombol burger; di layar lebar tombolnya disembunyikan CSS
  const [menuOpen, setMenuOpen] = useState(false);
  useEffect(() => {
    if (!menuOpen) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setMenuOpen(false);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [menuOpen]);
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
          <>
            <IconButton icon={menuOpen ? faXmark : faBars} label={menuOpen ? "Tutup menu" : "Buka menu"} className="nav-toggle"
              onClick={() => setMenuOpen((v) => !v)} aria-expanded={menuOpen} aria-controls="app-menu" />
            <div id="app-menu" className={`nav-panel${menuOpen ? " open" : ""}`}>
              <nav aria-label="Navigasi utama" onClick={() => setMenuOpen(false)}>
                <NavLink to="/pendataan/baru">Pendataan baru</NavLink>
                <NavLink to="/" end>Riwayat</NavLink>
                {user && <button type="button" onClick={() => setConfirming(true)} title={user.name}>Keluar</button>}
              </nav>
              <div className="nav-theme"><ThemeSwitcher variant="inline" /></div>
            </div>
          </>
        )}
        <div className={showNav ? "header-theme" : undefined}><ThemeSwitcher /></div>
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
