import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { apiError, fakeApi } from "../../test/fakeApi";
import { sortLabels } from "./LabelsPage";
import { loginAdmin, ME_OTP, renderAdmin } from "./testUtils";
import type { AdminLabel } from "./types";

const L = (p: Partial<AdminLabel> & Pick<AdminLabel, "id" | "name">): AdminLabel => ({
  description: null, color: "#e4572e", sort_order: p.id, archived: false, usage_count: 0,
  created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z", ...p,
});
const LABELS = [
  L({ id: 5, name: "Penyok", sort_order: 2, usage_count: 12, color: "#2e86ab" }),
  L({ id: 3, name: "Retak", sort_order: 1, usage_count: 128, description: "Retakan pada dinding paket" }),
  L({ id: 9, name: "Basah", sort_order: 3 }),
  L({ id: 7, name: "Lama", archived: true, usage_count: 4 }),
];

beforeEach(() => {
  sessionStorage.clear();
  loginAdmin();
});
afterEach(() => vi.unstubAllGlobals());

function setup(extra: Parameters<typeof fakeApi>[0] = {}) {
  return fakeApi({ "GET /api/v1/admin/me": { body: ME_OTP }, "GET /api/v1/admin/labels": { body: { labels: LABELS } }, ...extra });
}

const rows = () => screen.getAllByRole("row").slice(1);
const rowOf = (name: string) => rows().find((r) => within(r).queryByText(name))!;

it("sortLabels: aktif urut sort_order, arsip di bawah", () => {
  expect(sortLabels(LABELS).map((l) => l.name)).toEqual(["Retak", "Penyok", "Basah", "Lama"]);
});

it("tabel: warna, nama, deskripsi, pemakaian, status; hapus hanya untuk label belum dipakai", async () => {
  setup();
  renderAdmin("/admin/label");
  await screen.findByText("Retak");
  expect(rows().map((r) => within(r).getAllByRole("cell")[2]!.textContent)).toEqual(["Retak", "Penyok", "Basah", "Lama"]);
  expect(within(rowOf("Retak")).getByText("128")).toBeTruthy();
  expect(within(rowOf("Retak")).getByText("Retakan pada dinding paket")).toBeTruthy();
  expect(within(rowOf("Retak")).queryByRole("button", { name: /Hapus/ })).toBeNull();
  expect(within(rowOf("Retak")).getByRole("button", { name: /Arsipkan/ })).toBeTruthy();
  expect(within(rowOf("Basah")).getByRole("button", { name: /Hapus/ })).toBeTruthy();
  expect(within(rowOf("Lama")).getByText("Diarsipkan")).toBeTruthy();
  expect(within(rowOf("Lama")).getByRole("button", { name: /Buka arsip/ })).toBeTruthy();
  // tombol urutan hanya untuk label aktif; ujung daftar nonaktif
  expect((screen.getByRole("button", { name: "Naikkan Retak" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("button", { name: "Turunkan Basah" }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.queryByRole("button", { name: "Naikkan Lama" })).toBeNull();
});

it("tambah label: warna otomatis (tanpa color) atau warna sendiri; validasi nama", async () => {
  const api = setup({ "POST /api/v1/admin/labels": (c) => ({ status: 201, body: L({ id: 10, name: (c.body as { name: string }).name }) }) });
  renderAdmin("/admin/label");
  await screen.findByText("Retak");
  fireEvent.click(screen.getByRole("button", { name: /Tambah label/ }));
  fireEvent.click(screen.getByRole("button", { name: "Tambah" }));
  expect(screen.getByText("Nama wajib diisi.")).toBeTruthy();
  fireEvent.change(screen.getByLabelText("Nama"), { target: { value: "  Sobek  " } });
  fireEvent.change(screen.getByLabelText(/Deskripsi/), { target: { value: "Kemasan sobek" } });
  fireEvent.click(screen.getByRole("button", { name: "Tambah" }));
  await waitFor(() => expect(api.last("POST", "/api/v1/admin/labels")?.body).toEqual({ name: "Sobek", description: "Kemasan sobek" }));
  expect(await screen.findByText('Label "Sobek" ditambahkan.')).toBeTruthy();

  fireEvent.click(screen.getByRole("button", { name: /Tambah label/ }));
  fireEvent.change(screen.getByLabelText("Nama"), { target: { value: "Basah2" } });
  fireEvent.click(screen.getByLabelText(/Pilih otomatis/));
  fireEvent.change(screen.getByLabelText("Kode warna"), { target: { value: "#ABCDEF" } });
  fireEvent.click(screen.getByRole("button", { name: "Tambah" }));
  await waitFor(() => expect(api.last("POST", "/api/v1/admin/labels")?.body).toEqual({ name: "Basah2", color: "#abcdef" }));
});

it("nama sudah dipakai → pesan dari server", async () => {
  setup({ "POST /api/v1/admin/labels": apiError(409, "LABEL_NAME_TAKEN") });
  renderAdmin("/admin/label");
  await screen.findByText("Retak");
  fireEvent.click(screen.getByRole("button", { name: /Tambah label/ }));
  fireEvent.change(screen.getByLabelText("Nama"), { target: { value: "retak" } });
  fireEvent.click(screen.getByRole("button", { name: "Tambah" }));
  expect(await screen.findByText(/Nama label sudah dipakai/)).toBeTruthy();
});

it("ubah nama label terpakai: peringatan 'dipakai di N kotak', PUT replace lengkap", async () => {
  const api = setup({ "PUT /api/v1/admin/labels/:id": (c) => ({ body: c.body }) });
  renderAdmin("/admin/label");
  await screen.findByText("Retak");
  fireEvent.click(within(rowOf("Retak")).getByRole("button", { name: /Ubah/ }));
  expect(screen.queryByText(/dipakai di 128 kotak/)).toBeNull();
  fireEvent.change(screen.getByLabelText("Nama"), { target: { value: "Retak halus" } });
  expect(screen.getByText(/Label ini dipakai di 128 kotak/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Simpan" }));
  await waitFor(() => expect(api.last("PUT", "/api/v1/admin/labels/3")?.body).toEqual({
    name: "Retak halus", description: "Retakan pada dinding paket", color: "#e4572e", archived: false,
  }));
});

it("urutan: turunkan → PUT /labels/order dengan semua label aktif; LABEL_ORDER_STALE → muat ulang", async () => {
  let stale = false;
  const api = setup({ "PUT /api/v1/admin/labels/order": () => (stale ? apiError(409, "LABEL_ORDER_STALE") : { status: 204 }) });
  renderAdmin("/admin/label");
  await screen.findByText("Retak");
  fireEvent.click(screen.getByRole("button", { name: "Turunkan Retak" }));
  await waitFor(() => expect(api.last("PUT", "/api/v1/admin/labels/order")?.body).toEqual({ ids: [5, 3, 9] }));
  await waitFor(() => expect(rows().map((r) => within(r).getAllByRole("cell")[2]!.textContent).slice(0, 2)).toEqual(["Penyok", "Retak"]));
  stale = true;
  const before = api.count("GET", "/api/v1/admin/labels");
  fireEvent.click(screen.getByRole("button", { name: "Turunkan Penyok" }));
  expect(await screen.findByText(/Daftar label sudah berubah/)).toBeTruthy();
  await waitFor(() => expect(api.count("GET", "/api/v1/admin/labels")).toBe(before + 1));
});

it("arsipkan dan buka arsip lewat PUT replace", async () => {
  const api = setup({ "PUT /api/v1/admin/labels/:id": (c) => ({ body: c.body }) });
  renderAdmin("/admin/label");
  await screen.findByText("Retak");
  fireEvent.click(within(rowOf("Penyok")).getByRole("button", { name: /Arsipkan/ }));
  await waitFor(() => expect(api.last("PUT", "/api/v1/admin/labels/5")?.body).toMatchObject({ name: "Penyok", archived: true, color: "#2e86ab" }));
  fireEvent.click(within(rowOf("Lama")).getByRole("button", { name: /Buka arsip/ }));
  await waitFor(() => expect(api.last("PUT", "/api/v1/admin/labels/7")?.body).toMatchObject({ archived: false }));
});

it("hapus: dialog konfirmasi → DELETE; LABEL_IN_USE → pesan tawarkan arsip", async () => {
  let inUse = false;
  const api = setup({ "DELETE /api/v1/admin/labels/:id": () => (inUse ? apiError(409, "LABEL_IN_USE", { usage_count: 2 }) : { status: 204 }) });
  renderAdmin("/admin/label");
  await screen.findByText("Retak");
  fireEvent.click(within(rowOf("Basah")).getByRole("button", { name: /Hapus/ }));
  const dlg = screen.getByRole("dialog", { name: "Hapus label?" });
  fireEvent.click(within(dlg).getByRole("button", { name: /Hapus label/ }));
  await waitFor(() => expect(api.count("DELETE", "/api/v1/admin/labels/9")).toBe(1));
  expect(await screen.findByText('Label "Basah" dihapus.')).toBeTruthy();
  inUse = true;
  fireEvent.click(within(rowOf("Basah")).getByRole("button", { name: /Hapus/ }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: /Hapus label/ }));
  expect(await screen.findByText(/sudah dipakai anotasi sehingga tidak bisa dihapus/)).toBeTruthy();
});

it("HP: daftar kartu dengan menu titik tiga", async () => {
  const mm = window.matchMedia;
  window.matchMedia = ((q: string) => ({ matches: q === "(max-width: 767px)", media: q, addEventListener() {}, removeEventListener() {} })) as unknown as typeof window.matchMedia;
  try {
    const api = setup({ "PUT /api/v1/admin/labels/:id": (c) => ({ body: c.body }) });
    renderAdmin("/admin/label");
    await screen.findByText("Retak");
    expect(screen.queryByRole("table")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Aksi label Penyok" }));
    expect(screen.queryByRole("menuitem", { name: /Hapus/ })).toBeNull(); // terpakai: tidak bisa dihapus
    fireEvent.click(screen.getByRole("menuitem", { name: /Arsipkan/ }));
    await waitFor(() => expect(api.last("PUT", "/api/v1/admin/labels/5")?.body).toMatchObject({ archived: true }));
    expect(screen.getByRole("button", { name: "Buka menu" })).toBeTruthy();
  } finally {
    window.matchMedia = mm;
  }
});
