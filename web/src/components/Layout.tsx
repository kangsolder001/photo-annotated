import type { ReactNode } from "react";
import { Link, NavLink, useNavigate } from "react-router-dom";
import { api } from "../api/endpoints";
import { config } from "../config";
import { useSessionUser } from "../features/auth/useSession";
import { session } from "../lib/session";

export function Layout({ children }: { children: ReactNode }) {
  const user = useSessionUser();
  const navigate = useNavigate();
  const logout = async () => {
    try {
      await api.logout();
    } catch {
      // token mungkin sudah tidak berlaku; sesi lokal tetap dihapus
    }
    session.expire();
    navigate("/masuk");
  };
  const showNav = config.authMode === "none" || user;
  return (
    <>
      <header className="app-header">
        <Link to="/" className="brand">📷 Photo Annotated</Link>
        {showNav && (
          <nav aria-label="Navigasi utama">
            <NavLink to="/" end>Baru</NavLink>
            <NavLink to="/riwayat">Riwayat</NavLink>
            {user && <button type="button" onClick={logout} title={user.name}>Keluar</button>}
          </nav>
        )}
      </header>
      <main>{children}</main>
    </>
  );
}
