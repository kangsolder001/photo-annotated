import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { App } from "../../App";
import { session } from "../../lib/session";
import { apiError, fakeApi } from "../../test/fakeApi";
import { adminSession } from "./session";
import { loginAdmin, ME_OTP, renderAdmin } from "./testUtils";

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
});
afterEach(() => vi.unstubAllGlobals());

describe("sesi admin", () => {
  it("token di sessionStorage, terpisah dari sesi user di localStorage; kedaluwarsa = tidak ada", () => {
    adminSession.save("adm2.x", "2099-01-01T00:00:00Z", { name: "A", phone: "p" });
    expect(sessionStorage.getItem("pa.admin.session")).toContain("adm2.x");
    expect(localStorage.length).toBe(0);
    expect(session.token()).toBeNull();
    adminSession.save("adm2.y", "2000-01-01T00:00:00Z", { name: "A", phone: "p" });
    expect(adminSession.token()).toBeNull();
  });
});

describe("rute & layout", () => {
  it("dashboard adalah chunk lazy: /admin tanpa token → /admin/masuk?next=", async () => {
    fakeApi({ "GET /api/v1/admin/channel-status": { body: { otp_available: true } } });
    render(<MemoryRouter initialEntries={["/admin/label"]}><App /></MemoryRouter>);
    expect(await screen.findByRole("heading", { name: "Masuk admin" })).toBeTruthy();
  });

  it("token admin dikirim ke /admin/*; token user tidak ikut", async () => {
    session.save("user.tok", "2099-01-01T00:00:00Z", { id: "u", name: "U", email: "e", phone: "p", role: "user" });
    loginAdmin();
    const api = fakeApi({
      "GET /api/v1/admin/me": { body: ME_OTP },
      "GET /api/v1/admin/labels": { body: { labels: [] } },
    });
    renderAdmin("/admin/label");
    await screen.findByText(/Belum ada label/);
    expect(api.last("GET", "/api/v1/admin/labels")?.headers.Authorization).toBe("Bearer adm2.tok");
  });

  it("401 → token admin dibuang (sesi user tetap) dan kembali ke /admin/masuk", async () => {
    session.save("user.tok", "2099-01-01T00:00:00Z", { id: "u", name: "U", email: "e", phone: "p", role: "user" });
    loginAdmin();
    fakeApi({
      "GET /api/v1/admin/me": apiError(401, "UNAUTHORIZED"),
      "GET /api/v1/admin/labels": apiError(401, "UNAUTHORIZED"),
      "GET /api/v1/admin/channel-status": { body: { otp_available: true } },
    });
    renderAdmin("/admin/label");
    await waitFor(() => expect(screen.getByTestId("where").textContent).toBe("/admin/masuk?next=%2Fadmin%2Flabel"));
    expect(adminSession.token()).toBeNull();
    expect(session.token()).toBe("user.tok");
  });

  it("menu: Annotator hanya pada mode otp; Keluar membuang token", async () => {
    loginAdmin();
    fakeApi({ "GET /api/v1/admin/me": { body: { ...ME_OTP, auth_mode: "none" } }, "GET /api/v1/admin/channel-status": { body: { otp_available: true } } });
    renderAdmin("/admin");
    await screen.findByRole("link", { name: /Beranda/ });
    await waitFor(() => expect(screen.getByText("Super Admin")).toBeTruthy());
    expect(screen.getByRole("navigation", { name: "Navigasi dashboard" }).textContent).toContain("Label");
    expect(screen.queryByRole("link", { name: /Annotator/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Keluar/ }));
    expect(adminSession.token()).toBeNull();
    await waitFor(() => expect(screen.getByTestId("where").textContent).toBe("/admin/masuk"));
  });

  it("menu Annotator tampil pada mode otp", async () => {
    loginAdmin();
    fakeApi({ "GET /api/v1/admin/me": { body: ME_OTP } });
    renderAdmin("/admin");
    expect(await screen.findByRole("link", { name: /Annotator/ })).toBeTruthy();
  });
});

describe("login OTP admin", () => {
  it("nomor → kode terkirim → kode salah (sisa percobaan) → kode benar → token disimpan, ke ?next", async () => {
    let verifies = 0;
    const api = fakeApi({
      "GET /api/v1/admin/channel-status": { body: { otp_available: true } },
      "POST /api/v1/admin/otp/request": { status: 202, body: { challenge_id: "ch1", status: "queued", resend_after_seconds: 60, estimated_wait_seconds: 0 } },
      "GET /api/v1/admin/otp/:id": { body: { status: "sent", failure_reason: null, resend_after_seconds: 50, expires_at: "2099-01-01T00:00:00Z" } },
      "POST /api/v1/admin/otp/verify": () => (++verifies === 1
        ? apiError(422, "OTP_INVALID", { attempts_left: 4 })
        : { body: { access_token: "adm2.new", expires_at: "2099-01-01T00:00:00Z", admin: { name: "Super Admin", phone: "62812****7890" } } }),
      "GET /api/v1/admin/me": { body: ME_OTP },
      "GET /api/v1/admin/labels": { body: { labels: [] } },
    });
    renderAdmin("/admin/masuk?next=%2Fadmin%2Flabel");
    fireEvent.change(screen.getByLabelText(/Nomor WhatsApp superadmin/), { target: { value: "0812-3456-7890" } });
    fireEvent.click(screen.getByRole("button", { name: /Kirim kode WhatsApp/ }));
    expect(await screen.findByText("Kode sudah dikirim ke WhatsApp Anda.")).toBeTruthy();
    expect(api.last("POST", "/api/v1/admin/otp/request")?.body).toEqual({ phone: "0812-3456-7890" });
    expect(api.last("GET", "/api/v1/admin/otp/ch1")?.headers.Authorization).toBeUndefined();
    expect(screen.getByRole("button", { name: /Kirim ulang dalam/ })).toBeTruthy();

    fireEvent.change(screen.getByLabelText(/Kode 6 digit/), { target: { value: "000000" } });
    expect(await screen.findByText("Kode salah. Sisa 4 percobaan.")).toBeTruthy();
    fireEvent.change(screen.getByLabelText(/Kode 6 digit/), { target: { value: "123456" } });
    await waitFor(() => expect(screen.getByTestId("where").textContent).toBe("/admin/label"));
    expect(api.last("POST", "/api/v1/admin/otp/verify")?.body).toEqual({ challenge_id: "ch1", code: "123456" });
    expect(adminSession.token()).toBe("adm2.new");
    expect(sessionStorage.getItem("pa.admin.otp")).toBeNull();
  });

  it("kanal WhatsApp tidak tersedia → banner; kode kedaluwarsa → wajib minta ulang", async () => {
    fakeApi({
      "GET /api/v1/admin/channel-status": { body: { otp_available: false } },
      "POST /api/v1/admin/otp/request": { status: 202, body: { challenge_id: "ch1", status: "queued", resend_after_seconds: 60, estimated_wait_seconds: 0 } },
      "GET /api/v1/admin/otp/:id": { body: { status: "failed", failure_reason: "WA_UNAVAILABLE", resend_after_seconds: 0, expires_at: null } },
    });
    renderAdmin("/admin/masuk");
    expect(await screen.findByText(/Pengiriman kode WhatsApp sedang bermasalah/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText(/Nomor WhatsApp superadmin/), { target: { value: "08123" } });
    fireEvent.click(screen.getByRole("button", { name: /Kirim kode WhatsApp/ }));
    expect(await screen.findByText(/Layanan WhatsApp sedang bermasalah/)).toBeTruthy();
    expect((screen.getByRole("button", { name: /Kirim ulang kode/ }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: /Ganti nomor/ }));
    expect(screen.getByLabelText(/Nomor WhatsApp superadmin/)).toBeTruthy();
  });
});
