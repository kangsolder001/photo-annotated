import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { drafts } from "../../lib/draft";
import { history } from "../../lib/history";
import { apiError, fakeApi } from "../../test/fakeApi";
import { SubmissionPage } from "./SubmissionPage";

vi.mock("../../config", async (orig) => ({ ...(await orig<typeof import("../../config")>()), config: { apiBase: "", authMode: "none", turnstileSiteKey: "" } }));

beforeEach(() => localStorage.clear());
afterEach(() => vi.unstubAllGlobals());

const page = (id: string) => render(<MemoryRouter initialEntries={[`/s/${id}`]}><Routes><Route path="/s/:id" element={<SubmissionPage />} /></Routes></MemoryRouter>);

it("404 untuk laporan di riwayat → 'sudah dihapus', entri riwayat dan draf dibuang", async () => {
  history.add({ id: "s1", createdAt: "2026-10-24T00:00:00Z", photos: 1, boxes: 1, owner: "anon", editToken: "et" });
  drafts.save("s1", "anon", { version: 1, savedAt: "x", photos: [] });
  fakeApi({ "GET /api/v1/submissions/:id": apiError(404, "NOT_FOUND") });
  page("s1");
  expect(await screen.findByText(/Laporan ini sudah dihapus/)).toBeTruthy();
  expect(history.list("anon")).toEqual([]);
  expect(localStorage.getItem("pa.draft.anon.s1")).toBeNull();
});

it("404 untuk laporan yang tidak ada di riwayat → pesan akses biasa", async () => {
  fakeApi({ "GET /api/v1/submissions/:id": apiError(404, "NOT_FOUND") });
  page("zz");
  expect(await screen.findByText(/tidak punya akses/)).toBeTruthy();
});
