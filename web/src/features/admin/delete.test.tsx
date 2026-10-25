import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { fakeApi } from "../../test/fakeApi";
import { detail, listItem } from "./fixtures";
import { loginAdmin, ME_OTP, renderAdmin } from "./testUtils";

beforeEach(() => {
  sessionStorage.clear();
  loginAdmin();
});
afterEach(() => vi.unstubAllGlobals());

function confirmWith(word: string) {
  const dlg = screen.getByRole("dialog");
  fireEvent.change(within(dlg).getByLabelText(/Ketik HAPUS/), { target: { value: word } });
  return dlg;
}

it("hapus submission: dialog menyebut yang hilang + permanen; tombol aktif hanya setelah ketik HAPUS", async () => {
  const api = fakeApi({
    "GET /api/v1/admin/me": { body: ME_OTP },
    "GET /api/v1/admin/labels": { body: { labels: [] } },
    "GET /api/v1/admin/submissions/:id": { body: detail() },
    "GET /api/v1/admin/submissions": { body: { items: [], next_cursor: null } },
    "DELETE /api/v1/admin/submissions/:id": { body: { status: "deleted", photos_deleted: 2, annotations_deleted: 3, submission_deleted: true } },
  });
  renderAdmin("/admin/anotasi/s1");
  fireEvent.click(await screen.findByRole("button", { name: /Hapus submission/ }));
  const dlg = screen.getByRole("dialog", { name: "Hapus submission?" });
  expect(within(dlg).getByText("2 foto, 3 kotak anotasi milik Budi Santoso")).toBeTruthy();
  expect(within(dlg).getByText(/permanen dan tidak bisa dibatalkan/)).toBeTruthy();
  const btn = within(dlg).getByRole("button", { name: /Hapus submission/ }) as HTMLButtonElement;
  expect(btn.disabled).toBe(true);
  confirmWith("hapus");
  expect(btn.disabled).toBe(true);
  confirmWith("HAPUS");
  expect(btn.disabled).toBe(false);
  fireEvent.click(btn);
  await waitFor(() => expect(api.count("DELETE", "/api/v1/admin/submissions/s1")).toBe(1));
  expect(await screen.findByText("Submission dihapus permanen.")).toBeTruthy();
  await waitFor(() => expect(screen.getByTestId("where").textContent).toBe("/admin/anotasi"));
});

it("hapus satu foto: cleanup_pending → pesan file dibersihkan otomatis; detail dimuat ulang", async () => {
  const api = fakeApi({
    "GET /api/v1/admin/me": { body: ME_OTP },
    "GET /api/v1/admin/submissions/:id": { body: detail() },
    "DELETE /api/v1/admin/photos/:id": { status: 202, body: { status: "cleanup_pending", photos_deleted: 1, annotations_deleted: 2, submission_deleted: false } },
  });
  renderAdmin("/admin/anotasi/s1");
  await screen.findByRole("heading", { name: "Foto 1 dari 2" });
  fireEvent.click(screen.getAllByRole("button", { name: /Hapus foto/ })[0]!);
  expect(within(screen.getByRole("dialog")).getByText(/foto 1 dan 2 kotak anotasinya/)).toBeTruthy();
  const dlg = confirmWith("HAPUS");
  fireEvent.click(within(dlg).getByRole("button", { name: /Hapus foto/ }));
  expect(await screen.findByText("Foto dihapus; sebagian file dibersihkan otomatis.")).toBeTruthy();
  expect(api.count("DELETE", "/api/v1/admin/photos/ph1")).toBe(1);
  expect(api.count("GET", "/api/v1/admin/submissions/s1")).toBe(2);
});

it("hapus terpilih dari daftar: POST /submissions/delete, item hilang, ringkasan hasil", async () => {
  const api = fakeApi({
    "GET /api/v1/admin/me": { body: ME_OTP },
    "GET /api/v1/admin/labels": { body: { labels: [] } },
    "GET /api/v1/admin/submissions": { body: { items: [listItem(1), listItem(2), listItem(3)], next_cursor: null } },
    "POST /api/v1/admin/submissions/delete": { body: { results: [{ id: "s1", status: "deleted" }, { id: "s3", status: "not_found" }] } },
  });
  renderAdmin("/admin/anotasi");
  await screen.findByText("Budi 1");
  fireEvent.click(screen.getByRole("checkbox", { name: /dari Budi 1/ }));
  fireEvent.click(screen.getByRole("checkbox", { name: /dari Budi 3/ }));
  expect(screen.getByText("2 dipilih")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: /Hapus terpilih/ }));
  const dlg = screen.getByRole("dialog", { name: "Hapus 2 submission?" });
  expect(within(dlg).getByText(/2 submission \(6 foto, 14 kotak anotasi\) dari Budi 1 dan Budi 3/)).toBeTruthy();
  confirmWith("HAPUS");
  fireEvent.click(within(dlg).getByRole("button", { name: /Hapus terpilih/ }));
  await waitFor(() => expect(api.last("POST", "/api/v1/admin/submissions/delete")?.body).toEqual({ ids: ["s1", "s3"] }));
  expect(await screen.findByText("1 submission dihapus permanen; 1 sudah tidak ada.")).toBeTruthy();
  expect(screen.queryByText("Budi 1")).toBeNull();
  expect(screen.getByText("Budi 2")).toBeTruthy();
});
