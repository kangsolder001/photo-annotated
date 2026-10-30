import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { apiError, fakeApi } from "../../test/fakeApi";
import { sortGroups } from "./GroupsPage";
import { loginAdmin, ME_OTP, renderAdmin } from "./testUtils";
import type { AdminGroup, AdminUser } from "./types";

const G = (p: Partial<AdminGroup> & Pick<AdminGroup, "id" | "name">): AdminGroup => ({
  archived: false, user_count: 0, created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z", ...p,
});
const GROUPS = [
  G({ id: 4, name: "Kelompok 5", user_count: 0 }),
  G({ id: 3, name: "Kelompok 4 – CL-05", user_count: 12 }),
  G({ id: 1, name: "Kelompok Lama", archived: true, user_count: 2 }),
];

beforeEach(() => {
  sessionStorage.clear();
  loginAdmin();
});
afterEach(() => vi.unstubAllGlobals());

function setup(extra: Parameters<typeof fakeApi>[0] = {}) {
  return fakeApi({ "GET /api/v1/admin/me": { body: ME_OTP }, "GET /api/v1/admin/groups": { body: { groups: GROUPS } }, ...extra });
}
const rows = () => screen.getAllByRole("row").slice(1);
const rowOf = (name: string) => rows().find((r) => within(r).queryByText(name))!;

it("sortGroups: aktif urut nama, arsip di bawah", () => {
  expect(sortGroups(GROUPS).map((g) => g.name)).toEqual(["Kelompok 4 – CL-05", "Kelompok 5", "Kelompok Lama"]);
});

it("menu Kelompok; tabel: nama, jumlah mahasiswa, status; hapus hanya untuk kelompok belum dipakai", async () => {
  setup();
  renderAdmin("/admin/kelompok");
  await screen.findByText("Kelompok 5");
  expect(screen.getAllByRole("link", { name: /Kelompok/ }).some((a) => a.getAttribute("href") === "/admin/kelompok")).toBe(true);
  expect(within(rowOf("Kelompok 4 – CL-05")).getByText("12")).toBeTruthy();
  expect(within(rowOf("Kelompok 4 – CL-05")).queryByRole("button", { name: /Hapus/ })).toBeNull();
  expect(within(rowOf("Kelompok 5")).getByRole("button", { name: /Hapus/ })).toBeTruthy();
  expect(within(rowOf("Kelompok Lama")).getByText("Diarsipkan")).toBeTruthy();
  expect(within(rowOf("Kelompok Lama")).getByRole("button", { name: /Aktifkan/ })).toBeTruthy();
});

it("tambah: nama di-trim; nama kembar → pesan GROUP_NAME_TAKEN", async () => {
  let n = 0;
  const api = setup({
    "POST /api/v1/admin/groups": (c) => (n++ === 0 ? apiError(409, "GROUP_NAME_TAKEN") : { status: 201, body: G({ id: 9, name: (c.body as { name: string }).name }) }),
  });
  renderAdmin("/admin/kelompok");
  await screen.findByText("Kelompok 5");
  fireEvent.click(screen.getByRole("button", { name: /Tambah kelompok/ }));
  fireEvent.click(screen.getByRole("button", { name: "Tambah" }));
  expect(screen.getByText("Nama kelompok wajib diisi.")).toBeTruthy();
  fireEvent.change(screen.getByLabelText("Nama kelompok"), { target: { value: "  Kelompok 6  " } });
  fireEvent.click(screen.getByRole("button", { name: "Tambah" }));
  expect(await screen.findByText(/Nama kelompok sudah dipakai/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Tambah" }));
  await waitFor(() => expect(api.count("POST", "/api/v1/admin/groups")).toBe(2));
  expect(api.last("POST", "/api/v1/admin/groups")!.body).toEqual({ name: "Kelompok 6" });
});

it("ubah nama dan arsipkan → PUT {name, archived}", async () => {
  const api = setup({ "PUT /api/v1/admin/groups/:id": (c) => ({ body: G({ id: 4, ...(c.body as object) } as AdminGroup) }) });
  renderAdmin("/admin/kelompok");
  await screen.findByText("Kelompok 5");
  fireEvent.click(within(rowOf("Kelompok 5")).getByRole("button", { name: /Arsipkan/ }));
  await waitFor(() => expect(api.last("PUT", "/api/v1/admin/groups/4")?.body).toEqual({ name: "Kelompok 5", archived: true }));
  fireEvent.click(within(rowOf("Kelompok Lama")).getByRole("button", { name: /Ubah nama/ }));
  fireEvent.change(screen.getByLabelText("Nama kelompok"), { target: { value: "Kelompok 1" } });
  fireEvent.click(screen.getByRole("button", { name: "Simpan" }));
  await waitFor(() => expect(api.last("PUT", "/api/v1/admin/groups/1")?.body).toEqual({ name: "Kelompok 1", archived: true }));
});

it("hapus → DELETE; terpakai (409 GROUP_IN_USE) → pesan arsipkan saja", async () => {
  let inUse = false;
  const api = setup({ "DELETE /api/v1/admin/groups/:id": () => (inUse ? apiError(409, "GROUP_IN_USE") : { status: 204 }) });
  renderAdmin("/admin/kelompok");
  await screen.findByText("Kelompok 5");
  fireEvent.click(within(rowOf("Kelompok 5")).getByRole("button", { name: /Hapus/ }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: /Hapus kelompok/ }));
  await waitFor(() => expect(api.count("DELETE", "/api/v1/admin/groups/4")).toBe(1));
  inUse = true;
  fireEvent.click(within(rowOf("Kelompok 5")).getByRole("button", { name: /Hapus/ }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: /Hapus kelompok/ }));
  expect(await screen.findByText(/sudah dipakai akun mahasiswa/)).toBeTruthy();
});

it("profil annotator: NIM + kelompok; pindah kelompok → PUT /admin/users/{id}/group", async () => {
  let group = { id: 3, name: "Kelompok 4 – CL-05" };
  const user = (): AdminUser => ({
    id: "u1", name: "Siti", email: "s@x.id", phone: "6281234567890", created_at: "2026-09-01T00:00:00Z", last_login_at: null,
    blocked: false, blocked_at: null, blocked_reason: null, nim: "251FS01017", group,
    stats: { submissions: 0, annotations: 0, accepted: 0, rejected: 0, pending: 0, last_submission_at: null },
  });
  const api = setup({
    "GET /api/v1/admin/users/:id": () => ({ body: user() }),
    "PUT /api/v1/admin/users/:id/group": (c) => {
      group = GROUPS.find((g) => g.id === (c.body as { group_id: number }).group_id)!;
      return { body: user() };
    },
  });
  renderAdmin("/admin/annotator/u1");
  expect(await screen.findByText("251FS01017")).toBeTruthy();
  expect(screen.getByText("Kelompok 4 – CL-05")).toBeTruthy();
  const select = screen.getByLabelText("Kelompok baru");
  await within(select).findByRole("option", { name: "Kelompok 5" });
  // kelompok arsip dan kelompok saat ini tidak ditawarkan
  expect(within(select).queryByRole("option", { name: "Kelompok Lama" })).toBeNull();
  expect(within(select).queryByRole("option", { name: "Kelompok 4 – CL-05" })).toBeNull();
  fireEvent.change(select, { target: { value: "4" } });
  fireEvent.click(screen.getByRole("button", { name: /Pindahkan/ }));
  await waitFor(() => expect(api.last("PUT", "/api/v1/admin/users/u1/group")?.body).toEqual({ group_id: 4 }));
  expect(await screen.findByText("Siti dipindah ke Kelompok 5.")).toBeTruthy();
});
