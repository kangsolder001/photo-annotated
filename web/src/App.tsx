import { Navigate, Route, Routes } from "react-router-dom";
import { Layout } from "./components/Layout";
import { HistoryPage } from "./features/history/HistoryPage";
import { NewSubmissionPage } from "./features/submit/NewSubmissionPage";

export function App() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<NewSubmissionPage />} />
        <Route path="/riwayat" element={<HistoryPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Layout>
  );
}
