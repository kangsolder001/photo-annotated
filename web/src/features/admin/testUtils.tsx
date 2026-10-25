import { render } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import AdminApp from "./AdminApp";
import { adminSession } from "./session";

export const ME_OTP = { name: "Super Admin", phone: "62812****7890", expires_at: "2099-01-01T00:00:00Z", auth_mode: "otp" as const };

export function loginAdmin(token = "adm2.tok") {
  adminSession.save(token, "2099-01-01T00:00:00Z", { name: "Super Admin", phone: "62812****7890" });
}

function Where() {
  const l = useLocation();
  return <output data-testid="where">{l.pathname + l.search}</output>;
}

/** Render dashboard di path tertentu (di bawah /admin), plus penunjuk lokasi saat ini. */
export function renderAdmin(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/admin/*" element={<AdminApp />} />
        <Route path="*" element={<p>halaman user</p>} />
      </Routes>
      <Where />
    </MemoryRouter>,
  );
}
