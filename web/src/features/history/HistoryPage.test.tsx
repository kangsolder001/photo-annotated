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
