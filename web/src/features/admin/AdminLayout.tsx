import type { IconDefinition } from "@fortawesome/fontawesome-svg-core";
import { faAnglesLeft } from "@fortawesome/free-solid-svg-icons/faAnglesLeft";
import { faAnglesRight } from "@fortawesome/free-solid-svg-icons/faAnglesRight";
import { faBars } from "@fortawesome/free-solid-svg-icons/faBars";
import { faChartSimple } from "@fortawesome/free-solid-svg-icons/faChartSimple";
import { faGauge } from "@fortawesome/free-solid-svg-icons/faGauge";
import { faImages } from "@fortawesome/free-solid-svg-icons/faImages";
import { faRightFromBracket } from "@fortawesome/free-solid-svg-icons/faRightFromBracket";
import { faTags } from "@fortawesome/free-solid-svg-icons/faTags";
import { faUserGroup } from "@fortawesome/free-solid-svg-icons/faUserGroup";
import { faUsers } from "@fortawesome/free-solid-svg-icons/faUsers";
import { faXmark } from "@fortawesome/free-solid-svg-icons/faXmark";
import { useEffect, useMemo, useState } from "react";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import { Icon, IconButton } from "../../components/Icon";
import { ThemeSwitcher } from "../../components/ThemeSwitcher";
import { adminApi } from "./api";
import { AdminCtx, type AdminContext } from "./context";
import { adminSession } from "./session";
import type { AdminMe } from "./types";
import { useMediaQuery } from "./ui/useMediaQuery";
import { ToastProvider } from "./ui/Toasts";

interface NavItem { to: string; label: string; icon: IconDefinition; end?: boolean; otpOnly?: boolean }

const NAV: NavItem[] = [
  { to: "/admin", label: "Beranda", icon: faGauge, end: true },
  { to: "/admin/anotasi", label: "Anotasi", icon: faImages },
  { to: "/admin/label", label: "Label", icon: faTags },
  { to: "/admin/kelompok", label: "Kelompok", icon: faUserGroup },
  { to: "/admin/annotator", label: "Annotator", icon: faUsers, otpOnly: true },
  { to: "/admin/statistik", label: "Statistik", icon: faChartSimple },
];

/**
 * Kerangka dashboard (spec-labels-admin §7.3): sidebar kiri tetap di desktop (≥ 1024 px), bisa
 * diciutkan jadi ikon di tablet (768–1023 px, default ciut), bar atas + drawer di HP (< 768 px).
 */
export function AdminLayout() {
  const navigate = useNavigate();
  const loc = useLocation();
  const isPhone = useMediaQuery("(max-width: 767px)");
  const isTablet = useMediaQuery("(min-width: 768px) and (max-width: 1023px)");
  const [collapsed, setCollapsed] = useState(isTablet);
  const [drawer, setDrawer] = useState(false);
  const [me, setMe] = useState<AdminMe | null>(null);
  const [title, setTitle] = useState("Dashboard");
  const admin = adminSession.admin();

  useEffect(() => setCollapsed(isTablet), [isTablet]);
  useEffect(() => setDrawer(false), [loc.pathname]);

  useEffect(() => {
    let alive = true;
    adminApi.me().then((m) => alive && setMe(m), () => {
      // 401 sudah membuang token (RequireAdmin mengarahkan ke /admin/masuk); error lain: menu tetap tampil
    });
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (!drawer) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setDrawer(false);
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [drawer]);

  const ctx = useMemo<AdminContext>(() => ({ me, authMode: me?.auth_mode ?? null, setTitle }), [me]);
  const logout = () => {
    adminSession.logout();
    navigate("/admin/masuk", { replace: true });
  };
  const items = NAV.filter((n) => !n.otpOnly || me?.auth_mode === "otp");
  const mini = collapsed && !isPhone;

  return (
    <AdminCtx.Provider value={ctx}>
      <ToastProvider>
        <div className={`admin-shell${mini ? " is-collapsed" : ""}${drawer ? " drawer-open" : ""}`}>
          <a className="skip-link" href="#admin-main">Langsung ke isi</a>
          {isPhone && (
            <header className="admin-topbar">
              <IconButton icon={faBars} label="Buka menu" onClick={() => setDrawer(true)} aria-expanded={drawer} aria-controls="admin-sidebar" />
              <h1 className="admin-topbar-title">{title}</h1>
            </header>
          )}
          {isPhone && drawer && <div className="admin-drawer-backdrop" onClick={() => setDrawer(false)} />}
          <aside id="admin-sidebar" className="admin-sidebar" aria-label="Menu dashboard" hidden={isPhone && !drawer}>
            <div className="admin-sidebar-head">
              <span className="brand">{mini ? "PA" : "Photo Annotated"}</span>
              {isPhone ? (
                <IconButton icon={faXmark} label="Tutup menu" onClick={() => setDrawer(false)} />
              ) : (
                <IconButton icon={collapsed ? faAnglesRight : faAnglesLeft} label={collapsed ? "Lebarkan menu" : "Ciutkan menu"} onClick={() => setCollapsed((v) => !v)} />
              )}
            </div>
            <nav aria-label="Navigasi dashboard">
              {items.map((n) => (
                <NavLink key={n.to} to={n.to} end={n.end} title={mini ? n.label : undefined} aria-label={mini ? n.label : undefined}>
                  <Icon icon={n.icon} /> <span className="nav-text">{n.label}</span>
                </NavLink>
              ))}
            </nav>
            <div className="admin-sidebar-foot">
              {!mini && <ThemeSwitcher variant="inline" />}
              {mini && <ThemeSwitcher />}
              {!mini && <div className="admin-who" title={me?.phone ?? admin?.phone}>{me?.name ?? admin?.name ?? "Admin"}</div>}
              <button type="button" className="admin-logout" onClick={logout} title="Keluar" aria-label={mini ? "Keluar" : undefined}>
                <Icon icon={faRightFromBracket} /> <span className="nav-text">Keluar</span>
              </button>
            </div>
          </aside>
          <main id="admin-main" className="admin-main" tabIndex={-1}>
            <Outlet />
          </main>
        </div>
      </ToastProvider>
    </AdminCtx.Provider>
  );
}
