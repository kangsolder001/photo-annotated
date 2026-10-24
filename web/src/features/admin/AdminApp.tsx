import { useSyncExternalStore, type ReactNode } from "react";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { AdminLayout } from "./AdminLayout";
import { HomePage } from "./HomePage";
import { LabelsPage } from "./LabelsPage";
import { AdminLoginPage } from "./LoginPage";
import { adminSession } from "./session";
import "./admin.css";

// Dashboard admin: dimuat terpisah (React.lazy di App.tsx) sehingga bundle user tidak membesar.

function useAdminToken() {
  return useSyncExternalStore(adminSession.subscribe, () => adminSession.token() ?? "", () => "");
}

function RequireAdmin({ children }: { children: ReactNode }) {
  const token = useAdminToken();
  const loc = useLocation();
  if (!token) {
    return <Navigate to={adminSession.loggedOut() ? "/admin/masuk" : `/admin/masuk?next=${encodeURIComponent(loc.pathname + loc.search)}`} replace />;
  }
  return <>{children}</>;
}

export default function AdminApp() {
  return (
    <Routes>
      <Route path="masuk" element={<AdminLoginPage />} />
      <Route element={<RequireAdmin><AdminLayout /></RequireAdmin>}>
        <Route index element={<HomePage />} />
        <Route path="label" element={<LabelsPage />} />
        <Route path="*" element={<Navigate to="/admin" replace />} />
      </Route>
    </Routes>
  );
}
