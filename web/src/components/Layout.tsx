import type { ReactNode } from "react";
import { Link, NavLink } from "react-router-dom";

export function Layout({ children, nav }: { children: ReactNode; nav?: ReactNode }) {
  return (
    <>
      <header className="app-header">
        <Link to="/" className="brand">📷 Photo Annotated</Link>
        <nav aria-label="Navigasi utama">
          <NavLink to="/" end>Baru</NavLink>
          <NavLink to="/riwayat">Riwayat</NavLink>
          {nav}
        </nav>
      </header>
      <main>{children}</main>
    </>
  );
}
