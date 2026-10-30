import { useSyncExternalStore, type ReactNode } from "react";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { AdminLayout } from "./AdminLayout";
import { AnnotatorPage } from "./AnnotatorPage";
import { AnnotatorsPage } from "./AnnotatorsPage";
import { GroupsPage } from "./GroupsPage";
import { HomePage } from "./HomePage";
import { LabelsPage } from "./LabelsPage";
import { SubmissionDetailPage } from "./SubmissionDetailPage";
import { StatsPage } from "./StatsPage";
import { SubmissionsPage } from "./SubmissionsPage";
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
        <Route path="anotasi" element={<SubmissionsPage />} />
        <Route path="anotasi/:id" element={<SubmissionDetailPage />} />
        <Route path="kelompok" element={<GroupsPage />} />
        <Route path="annotator" element={<AnnotatorsPage />} />
        <Route path="annotator/:id" element={<AnnotatorPage />} />
        <Route path="statistik" element={<StatsPage />} />
        <Route path="*" element={<Navigate to="/admin" replace />} />
      </Route>
    </Routes>
  );
}
