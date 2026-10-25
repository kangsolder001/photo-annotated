import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { fakeApi } from "../../test/fakeApi";
import { loginAdmin, ME_OTP, renderAdmin } from "./testUtils";
import type { AdminUser } from "./types";

const user = (i: number, p: Partial<AdminUser> = {}): AdminUser => ({
  id: `u${i}`, name: `Budi ${i}`, email: `b${i}@example.com`, phone: `628123456789${i}`, created_at: "2026-09-01T00:00:00Z",
  last_login_at: "2026-10-23T08:00:00Z", blocked: false, blocked_at: null, blocked_reason: null,
  stats: { submissions: 12, annotations: 48, accepted: 9, rejected: 1, pending: 2, last_submission_at: "2026-10-23T08:00:00Z" }, ...p,
});

beforeEach(() => {
  sessionStorage.clear();
  loginAdmin();
});
afterEach(() => vi.unstubAllGlobals());

it("daftar: kolom statistik + status; cari & filter status ke query; mode none → tanpa request", async () => {
  const api = fakeApi({
    "GET /api/v1/admin/me": { body: ME_OTP },
    "GET /api/v1/admin/users": { body: { items: [user(1), user(2, { blocked: true })], next_cursor: null } },
  });
  renderAdmin("/admin/annotator");
  expect(await screen.findByRole("link", { name: "Budi 1" })).toBeTruthy();
  expect(screen.getByText("6281234567891")).toBeTruthy();
  expect(screen.getAllByText("Diblokir", { selector: ".badge" }).length).toBe(1);
  fireEvent.change(screen.getByLabelText("Cari nama atau nomor"), { target: { value: "0812" } });
  fireEvent.click(screen.getByRole("button", { name: "Cari" }));
  await waitFor(() => expect(api.last("GET", "/api/v1/admin/users")?.query.get("q")).toBe("0812"));
  fireEvent.change(screen.getByLabelText("Status"), { target: { value: "blocked" } });
  await waitFor(() => expect(api.last("GET", "/api/v1/admin/users")?.query.get("status")).toBe("blocked"));
});

it("mode none: halaman annotator tidak meminta data", async () => {
  const api = fakeApi({ "GET /api/v1/admin/me": { body: { ...ME_OTP, auth_mode: "none" } } });
  renderAdmin("/admin/annotator");
  expect(await screen.findByText(/tanpa akun annotator/)).toBeTruthy();
  expect(api.count("GET", "/api/v1/admin/users")).toBe(0);
});

it("profil: blokir (alasan wajib) → buka blokir; tautan submission-nya", async () => {
  let blocked = false;
  const api = fakeApi({
    "GET /api/v1/admin/me": { body: ME_OTP },
    "GET /api/v1/admin/users/:id": () => ({ body: user(1, blocked ? { blocked: true, blocked_at: "2026-10-24T00:00:00Z", blocked_reason: "Asal-asalan" } : {}) }),
    "POST /api/v1/admin/users/:id/block": () => { blocked = true; return { body: {} }; },
    "POST /api/v1/admin/users/:id/unblock": () => { blocked = false; return { body: {} }; },
  });
  renderAdmin("/admin/annotator/u1");
  expect(await screen.findByRole("heading", { name: "Budi 1" })).toBeTruthy();
  expect(screen.getByRole("link", { name: /Lihat submission-nya/ }).getAttribute("href")).toBe("/admin/anotasi?user_id=u1");
  fireEvent.click(screen.getByRole("button", { name: /^Blokir$/ }));
  const dlg = screen.getByRole("dialog");
  fireEvent.click(within(dlg).getByRole("button", { name: /Blokir/ }));
  expect(within(dlg).getByText("Alasan wajib diisi.")).toBeTruthy();
  fireEvent.change(within(dlg).getByLabelText("Alasan blokir"), { target: { value: "Asal-asalan" } });
  fireEvent.click(within(dlg).getByRole("button", { name: /Blokir/ }));
  await waitFor(() => expect(api.last("POST", "/api/v1/admin/users/u1/block")?.body).toEqual({ reason: "Asal-asalan" }));
  expect(await screen.findByText(/Alasan: Asal-asalan/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: /Buka blokir/ }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: /Buka blokir/ }));
  await waitFor(() => expect(api.count("POST", "/api/v1/admin/users/u1/unblock")).toBe(1));
});

it("hapus data: default mempertahankan yang diterima; opsi ikut hapus; konfirmasi HAPUS", async () => {
  const api = fakeApi({
    "GET /api/v1/admin/me": { body: ME_OTP },
    "GET /api/v1/admin/users/:id": { body: user(1) },
    "POST /api/v1/admin/users/:id/delete-data": { status: 202, body: { submissions: 12, photos: 30, annotations: 48, status: "cleanup_pending" } },
  });
  renderAdmin("/admin/annotator/u1");
  fireEvent.click(await screen.findByRole("button", { name: /Hapus data annotator/ }));
  const dlg = screen.getByRole("dialog");
  expect(within(dlg).getByText("3 submission milik Budi 1 (9 yang sudah diterima dipertahankan)")).toBeTruthy();
  fireEvent.click(within(dlg).getByLabelText(/Ikut hapus submission yang sudah diterima/));
  expect(within(dlg).getByText("12 submission milik Budi 1 (termasuk yang sudah diterima)")).toBeTruthy();
  const btn = within(dlg).getByRole("button", { name: /Hapus data/ }) as HTMLButtonElement;
  expect(btn.disabled).toBe(true);
  fireEvent.change(within(dlg).getByLabelText(/Ketik HAPUS/), { target: { value: "HAPUS" } });
  fireEvent.click(btn);
  await waitFor(() => expect(api.last("POST", "/api/v1/admin/users/u1/delete-data")?.body).toEqual({ include_accepted: true }));
  expect(await screen.findByText(/12 submission \(30 foto, 48 kotak\) dihapus; file dibersihkan otomatis/)).toBeTruthy();
});

it("hapus akun: konfirmasi HAPUS → DELETE, kembali ke daftar dengan notifikasi", async () => {
  const api = fakeApi({
    "GET /api/v1/admin/me": { body: ME_OTP },
    "GET /api/v1/admin/users/:id": { body: user(1) },
    "GET /api/v1/admin/users": { body: { items: [], next_cursor: null } },
    "DELETE /api/v1/admin/users/:id": { status: 202, body: { account_deleted: true, submissions: 12, photos: 30, annotations: 48, status: "cleanup_pending" } },
  });
  renderAdmin("/admin/annotator/u1");
  fireEvent.click(await screen.findByRole("button", { name: /Hapus akun/ }));
  const dlg = screen.getByRole("dialog");
  expect(within(dlg).getByText(/akun Budi 1 \(.+\) dan 12 submission miliknya, termasuk 9 yang sudah diterima/)).toBeTruthy();
  const btn = within(dlg).getByRole("button", { name: /Hapus akun/ }) as HTMLButtonElement;
  expect(btn.disabled).toBe(true);
  fireEvent.change(within(dlg).getByLabelText(/Ketik HAPUS/), { target: { value: "HAPUS" } });
  fireEvent.click(btn);
  await waitFor(() => expect(api.count("DELETE", "/api/v1/admin/users/u1")).toBe(1));
  expect(await screen.findByText(/Akun Budi 1 dihapus beserta 12 submission \(30 foto, 48 kotak\); file dibersihkan otomatis/)).toBeTruthy();
  await waitFor(() => expect(api.count("GET", "/api/v1/admin/users")).toBeGreaterThan(0));
});
