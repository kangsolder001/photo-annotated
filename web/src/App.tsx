import { Navigate, Route, Routes } from "react-router-dom";
import { Layout } from "./components/Layout";
import { config } from "./config";
import { ForgotPage } from "./features/auth/ForgotPage";
import { LoginPage } from "./features/auth/LoginPage";
import { OtpPage } from "./features/auth/OtpPage";
import { RequireAuth } from "./features/auth/RequireAuth";
import { HistoryPage } from "./features/history/HistoryPage";
import { NewSubmissionPage } from "./features/submit/NewSubmissionPage";
import { SubmissionPage } from "./features/submission/SubmissionPage";

export function App() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<RequireAuth><NewSubmissionPage /></RequireAuth>} />
        <Route path="/riwayat" element={<RequireAuth><HistoryPage /></RequireAuth>} />
        <Route path="/s/:id" element={<RequireAuth><SubmissionPage /></RequireAuth>} />
        {config.authMode === "otp" && <Route path="/masuk" element={<LoginPage />} />}
        {config.authMode === "otp" && <Route path="/masuk/kode" element={<OtpPage />} />}
        {config.authMode === "otp" && <Route path="/masuk/lupa" element={<ForgotPage />} />}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Layout>
  );
}
