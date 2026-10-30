import { lazy, Suspense } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { Layout } from "./components/Layout";
import { Spinner } from "./components/ui";
import { config } from "./config";
import { ForgotPage } from "./features/auth/ForgotPage";
import { LoginPage } from "./features/auth/LoginPage";
import { OtpPage } from "./features/auth/OtpPage";
import { RequireAuth } from "./features/auth/RequireAuth";
import { HistoryPage } from "./features/history/HistoryPage";
import { SubmissionPage } from "./features/submission/SubmissionPage";
import { SurveyPage } from "./features/survey/SurveyPage";

// Dashboard admin dimuat terpisah (chunk sendiri): bundle user tidak memuat kode admin.
const AdminApp = lazy(() => import("./features/admin/AdminApp"));

function UserApp() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<RequireAuth><HistoryPage /></RequireAuth>} />
        <Route path="/pendataan/baru" element={<RequireAuth><SurveyPage /></RequireAuth>} />
        <Route path="/riwayat" element={<Navigate to="/" replace />} />
        <Route path="/s/:id" element={<RequireAuth><SubmissionPage /></RequireAuth>} />
        {config.authMode === "otp" && <Route path="/masuk" element={<LoginPage />} />}
        {config.authMode === "otp" && <Route path="/masuk/kode" element={<OtpPage />} />}
        {config.authMode === "otp" && <Route path="/masuk/lupa" element={<ForgotPage />} />}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Layout>
  );
}

export function App() {
  return (
    <Routes>
      <Route
        path="/admin/*"
        element={
          <Suspense fallback={<div className="page-loading"><Spinner label="Memuat dashboard" /></div>}>
            <AdminApp />
          </Suspense>
        }
      />
      <Route path="*" element={<UserApp />} />
    </Routes>
  );
}
