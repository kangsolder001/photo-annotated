import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { history } from "../../lib/history";
import { apiError, fakeApi } from "../../test/fakeApi";
import { HistoryPage } from "./HistoryPage";

vi.mock("../../config", async (orig) => ({ ...(await orig<typeof import("../../config")>()), config: { apiBase: "", authMode: "none", turnstileSiteKey: "" } }));

beforeEach(() => localStorage.clear());
afterEach(() => vi.unstubAllGlobals());

it("lencana review + alasan; laporan yang dihapus admin hilang dari riwayat", async () => {
  for (const id of ["a", "b", "c"]) history.add({ id, createdAt: "2026-10-24T00:00:00Z", photos: 1, boxes: 2, owner: "anon", editToken: "t" });
  const api = fakeApi({
    "GET /api/v1/submissions/:id": (_c, p) => (p.id === "c" ? apiError(404, "NOT_FOUND")
      : { body: { id: p.id, version: 1, created_at: "", updated_at: "", editable_until: "", editable: false, photos: [],
          review: p.id === "a" ? { status: "rejected", reason: "Kotak meleset" } : { status: "accepted", reason: null } } }),
  });
  render(<MemoryRouter><HistoryPage /></MemoryRouter>);
  expect(await screen.findByText("Ditolak")).toBeTruthy();
  expect(screen.getByText("Diterima")).toBeTruthy();
  expect(screen.getByText("Alasan: Kotak meleset")).toBeTruthy();
  await waitFor(() => expect(screen.getAllByRole("listitem")).toHaveLength(2));
  expect(api.last("GET", "/api/v1/submissions/a")?.headers["X-Edit-Token"]).toBe("t");
});

it("beranda: tombol besar Pendataan baru; entri pendataan menampilkan tanggal, RT/RW, kode, Bersedia/Menolak", async () => {
  history.add({ id: "a", createdAt: "2026-10-24T00:00:00Z", photos: 1, boxes: 2, owner: "anon", editToken: "t",
    survey: { date: "2026-10-02", rw: "05", rt: "03", code: "RT03-RW05-12", consent: true } });
  history.add({ id: "b", createdAt: "2026-10-24T00:00:00Z", photos: 0, boxes: 0, owner: "anon", editToken: "t",
    survey: { date: "2026-10-03", rw: "05", rt: "04", code: "RT04-RW05-07", consent: false } });
  fakeApi({ "GET /api/v1/submissions/:id": { body: { id: "x", version: 1, created_at: "", updated_at: "", editable_until: "", editable: false, photos: [] } } });
  render(<MemoryRouter><HistoryPage /></MemoryRouter>);
  expect(screen.getByRole("link", { name: "Pendataan baru" }).getAttribute("href")).toBe("/pendataan/baru");
  expect(screen.getByText("RT 03 / RW 05 · RT03-RW05-12")).toBeTruthy();
  expect(screen.getByText("Bersedia")).toBeTruthy();
  expect(screen.getByText("Menolak")).toBeTruthy();
  expect(screen.getByText("2 Okt 2026")).toBeTruthy();
  await waitFor(() => expect(screen.getAllByRole("listitem")).toHaveLength(2));
});
