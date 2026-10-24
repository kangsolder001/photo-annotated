import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { apiError, fakeApi } from "../../test/fakeApi";
import { detail, listItem } from "./fixtures";
import { listContext } from "./listContext";
import { unacceptWouldDelete } from "./review";
import { loginAdmin, ME_OTP, renderAdmin } from "./testUtils";

beforeEach(() => {
  sessionStorage.clear();
  loginAdmin();
});
afterEach(() => vi.unstubAllGlobals());

const reviewed = (status: string, reason: string | null = null) => ({ body: { review: { status, reason, reviewed_at: "2026-10-24T00:00:00Z" }, retention_deletes_at: status === "accepted" ? null : "2099-01-01T00:00:00Z" } });

function setup(extra: Parameters<typeof fakeApi>[0] = {}, d = detail()) {
  return fakeApi({ "GET /api/v1/admin/me": { body: ME_OTP }, "GET /api/v1/admin/submissions/:id": (_c, p) => ({ body: { ...d, id: p.id! } }), ...extra });
}

it("Terima mengirim versi yang dilihat; status + retensi diperbarui", async () => {
  const api = setup({ "PUT /api/v1/admin/submissions/:id/review": reviewed("accepted") });
  renderAdmin("/admin/anotasi/s1");
  fireEvent.click(await screen.findByRole("button", { name: /^Terima$/ }));
  await waitFor(() => expect(api.last("PUT", "/api/v1/admin/submissions/s1/review")?.body).toEqual({ status: "accepted", reason: null, version: 2 }));
  expect(await screen.findByText("Disimpan tanpa batas (diterima)")).toBeTruthy();
  expect(screen.getByRole("button", { name: /Batalkan review/ })).toBeTruthy();
});

it("Tolak butuh alasan; alasan dikirim; VERSION_CONFLICT → muat ulang", async () => {
  let conflict = false;
  const api = setup({ "PUT /api/v1/admin/submissions/:id/review": (c) => (conflict ? apiError(409, "VERSION_CONFLICT") : reviewed("rejected", (c.body as { reason: string }).reason)) });
  renderAdmin("/admin/anotasi/s1");
  fireEvent.click(await screen.findByRole("button", { name: /^Tolak$/ }));
  const dlg = screen.getByRole("dialog", { name: "Tolak submission" });
  fireEvent.click(within(dlg).getByRole("button", { name: /Tolak/ }));
  expect(within(dlg).getByText("Alasan wajib diisi.")).toBeTruthy();
  fireEvent.change(within(dlg).getByLabelText(/Alasan penolakan/), { target: { value: " Kotak tidak menutupi kerusakan " } });
  fireEvent.click(within(dlg).getByRole("button", { name: /Tolak/ }));
  await waitFor(() => expect(api.last("PUT", "/api/v1/admin/submissions/s1/review")?.body).toEqual({ status: "rejected", reason: "Kotak tidak menutupi kerusakan", version: 2 }));
  expect(await screen.findByText("Alasan: Kotak tidak menutupi kerusakan")).toBeTruthy();

  conflict = true;
  fireEvent.click(screen.getByRole("button", { name: /Batalkan review/ }));
  expect(await screen.findByText(/baru saja diubah annotator/)).toBeTruthy();
  const before = api.count("GET", "/api/v1/admin/submissions/s1");
  fireEvent.click(screen.getByRole("button", { name: "Muat ulang" }));
  await waitFor(() => expect(api.count("GET", "/api/v1/admin/submissions/s1")).toBe(before + 1));
});

it("pintasan: A terima, J berikutnya; tidak aktif saat mengetik", async () => {
  listContext.save("review_status=pending&sort=oldest", ["s0", "s1", "s2"], null);
  const api = setup({ "PUT /api/v1/admin/submissions/:id/review": reviewed("accepted") });
  renderAdmin("/admin/anotasi/s1");
  await screen.findByRole("button", { name: /^Terima$/ });
  expect(screen.getByRole("button", { name: /^Terima$/ }).getAttribute("title")).toBe("Terima (A)");
  const input = document.createElement("input");
  document.body.appendChild(input);
  fireEvent.keyDown(input, { key: "a" });
  expect(api.count("PUT", /review$/)).toBe(0);
  input.remove();
  fireEvent.keyDown(document.body, { key: "a" });
  await waitFor(() => expect(api.count("PUT", "/api/v1/admin/submissions/s1/review")).toBe(1));
  fireEvent.keyDown(document.body, { key: "j" });
  await waitFor(() => expect(screen.getByTestId("where").textContent).toBe("/admin/anotasi/s2"));
  await screen.findByRole("button", { name: /^Terima$/ });
  fireEvent.keyDown(document.body, { key: "k" });
  await waitFor(() => expect(screen.getByTestId("where").textContent).toBe("/admin/anotasi/s1"));
});

it("Terima lalu berikutnya membuka submission berikutnya di antrean", async () => {
  listContext.save("review_status=pending&sort=oldest", ["s1", "s2"], null);
  setup({ "PUT /api/v1/admin/submissions/:id/review": reviewed("accepted") });
  renderAdmin("/admin/anotasi/s1");
  fireEvent.click(await screen.findByRole("button", { name: /Terima lalu berikutnya/ }));
  await waitFor(() => expect(screen.getByTestId("where").textContent).toBe("/admin/anotasi/s2"));
});

it("membatalkan penerimaan submission > 90 hari → peringatan terhapus cleanup", async () => {
  expect(unacceptWouldDelete("accepted", "pending", "2026-01-01T00:00:00Z", Date.parse("2026-10-24T00:00:00Z"))).toBe(true);
  expect(unacceptWouldDelete("accepted", "pending", "2026-10-01T00:00:00Z", Date.parse("2026-10-24T00:00:00Z"))).toBe(false);
  expect(unacceptWouldDelete("rejected", "pending", "2020-01-01T00:00:00Z")).toBe(false);
  const api = setup({ "PUT /api/v1/admin/submissions/:id/review": reviewed("pending") },
    detail({ created_at: "2020-01-01T00:00:00Z", review: { status: "accepted", reason: null, reviewed_at: "2020-01-02T00:00:00Z" }, retention_deletes_at: null }));
  renderAdmin("/admin/anotasi/s1");
  fireEvent.click(await screen.findByRole("button", { name: /Batalkan review/ }));
  const dlg = screen.getByRole("dialog", { name: "Batalkan penerimaan?" });
  expect(within(dlg).getByText(/terhapus permanen/)).toBeTruthy();
  expect(api.count("PUT", /review$/)).toBe(0);
  fireEvent.click(within(dlg).getByRole("button", { name: "Lanjutkan" }));
  await waitFor(() => expect(api.last("PUT", "/api/v1/admin/submissions/s1/review")?.body).toMatchObject({ status: "pending" }));
});

it("review massal dari daftar: hasil per item; yang keluar dari antrean hilang", async () => {
  const api = fakeApi({
    "GET /api/v1/admin/me": { body: ME_OTP },
    "GET /api/v1/admin/labels": { body: { labels: [] } },
    "GET /api/v1/admin/submissions": { body: { items: [listItem(1), listItem(2, { version: 5 }), listItem(3)], next_cursor: null } },
    "POST /api/v1/admin/submissions/review": { body: { results: [{ id: "s1", result: "ok" }, { id: "s2", result: "version_conflict" }] } },
  });
  renderAdmin("/admin/anotasi?review_status=pending&sort=oldest");
  await screen.findByText("Budi 1");
  fireEvent.click(screen.getByRole("checkbox", { name: /dari Budi 1/ }));
  fireEvent.click(screen.getByRole("checkbox", { name: /dari Budi 2/ }));
  fireEvent.click(screen.getByRole("button", { name: /Tolak terpilih/ }));
  const dlg = screen.getByRole("dialog");
  fireEvent.change(within(dlg).getByLabelText(/Alasan penolakan/), { target: { value: "Buram" } });
  fireEvent.click(within(dlg).getByRole("button", { name: /Tolak terpilih/ }));
  await waitFor(() => expect(api.last("POST", "/api/v1/admin/submissions/review")?.body).toEqual({
    items: [{ id: "s1", version: 2 }, { id: "s2", version: 5 }], status: "rejected", reason: "Buram",
  }));
  expect(await screen.findByText(/1 submission ditolak; 1 baru diubah annotator/)).toBeTruthy();
  expect(screen.queryByText("Budi 1")).toBeNull();
  expect(screen.getByText("Budi 2")).toBeTruthy();
});
