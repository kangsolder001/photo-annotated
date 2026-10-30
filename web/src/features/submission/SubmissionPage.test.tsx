import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { drafts } from "../../lib/draft";
import { history } from "../../lib/history";
import { apiError, fakeApi } from "../../test/fakeApi";
import { surveyForm } from "../../test/surveyFixture";
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
  expect(await screen.findByText(/Pendataan ini sudah dihapus/)).toBeTruthy();
  expect(history.list("anon")).toEqual([]);
  expect(localStorage.getItem("pa.draft.anon.s1")).toBeNull();
});

it("404 untuk laporan yang tidak ada di riwayat → pesan akses biasa", async () => {
  fakeApi({ "GET /api/v1/submissions/:id": apiError(404, "NOT_FOUND") });
  page("zz");
  expect(await screen.findByText(/tidak punya akses/)).toBeTruthy();
});

describe("pendataan (spec-pendataan §3.2, §5.3)", () => {
  const answers = { a_date: "2026-10-01", a_rw: "05", a_consent: "yes", b1: "yes", b7: "no", r3_container_types: ["bucket"] };
  const view = {
    id: "s1", version: 1, created_at: "2026-11-02T03:00:00Z", updated_at: "2026-11-02T03:00:00Z",
    editable_until: "2099-01-01T00:00:00Z", editable: true, form_version: 1, answers,
    photos: [{ photo_id: "p1", position: 0, content_type: "image/jpeg", size_bytes: 10, width: 800, height: 600,
      annotations: [{ label: { id: 1, name: "Organik", color: "#2e7d32", archived: false }, text: null, x: 0.1, y: 0.2, w: 0.3, h: 0.3 }] }],
    review: { status: "pending", reason: null },
  };
  const routes = () => fakeApi({
    "GET /api/v1/submissions/:id": { body: view },
    "GET /api/v1/survey/form": { body: surveyForm },
    "GET /api/v1/labels": { body: { labels: [{ id: 1, name: "Organik", color: "#2e7d32" }] } },
    "GET /api/v1/photos/:id/url": { body: { url: "blob:foto", expires_at: "2099-01-01T00:00:00Z" } },
    "PUT /api/v1/submissions/:id": { body: { submission_id: "s1", version: 2, updated_at: "2026-11-02T04:00:00Z" } },
  });

  it("tampilan baca: jawaban dengan teks opsi; riwayat diperbarui dengan ringkasan pendataan", async () => {
    history.add({ id: "s1", createdAt: "2026-11-02T03:00:00Z", photos: 1, boxes: 1, owner: "anon", editToken: "et" });
    routes();
    page("s1");
    const b = await screen.findByRole("region", { name: "1. Pengetahuan" });
    expect(within(b).getByText("Ya")).toBeTruthy();
    expect(within(b).getByText("Tidak")).toBeTruthy();
    expect(within(screen.getByRole("region", { name: "A. Identitas Pendataan" })).getByText("Bersedia")).toBeTruthy();
    expect(screen.getByText("Ember")).toBeTruthy();
    expect(history.get("s1", "anon")?.survey).toEqual({ date: "2026-10-01", rw: "05", rt: "", code: "", consent: true });
  });

  it("edit memakai wizard yang sama: PUT berisi version, form_version, answers, photos", async () => {
    history.add({ id: "s1", createdAt: "2026-11-02T03:00:00Z", photos: 1, boxes: 1, owner: "anon", editToken: "et" });
    const api = routes();
    page("s1");
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    expect(await screen.findByText("Bagian 1 dari 4")).toBeTruthy();
    fireEvent.change(screen.getByLabelText(/^RW/), { target: { value: "06" } });
    for (let i = 0; i < 4; i++) fireEvent.click(screen.getByRole("button", { name: "Lanjut" }));
    expect(screen.getByRole("heading", { name: "Ringkasan" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Simpan" }));
    await waitFor(() => expect(api.last("PUT", "/api/v1/submissions/s1")).toBeTruthy());
    const put = api.last("PUT", "/api/v1/submissions/s1")!;
    expect(put.headers["X-Edit-Token"]).toBe("et");
    expect(put.body).toEqual({
      version: 1, form_version: 1, answers: { ...answers, a_rw: "06" },
      photos: [{ photo_id: "p1", annotations: [{ label_id: 1, x: 0.1, y: 0.2, w: 0.3, h: 0.3 }] }],
    });
    expect(await screen.findByText("Perubahan tersimpan.")).toBeTruthy();
    expect(localStorage.getItem("pa.draft.anon.s1")).toBeNull();
  });
});
