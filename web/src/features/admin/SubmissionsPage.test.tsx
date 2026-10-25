import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { apiError, fakeApi } from "../../test/fakeApi";
import { checkJSONL } from "./api";
import { dayAfter, dayStart } from "./filters";
import { detail, listItem } from "./fixtures";
import { loginAdmin, ME_OTP, renderAdmin } from "./testUtils";

const LABELS = { labels: [{ id: 3, name: "Retak", color: "#e4572e", description: null, sort_order: 1, archived: false, usage_count: 5, created_at: "", updated_at: "" }] };

beforeEach(() => {
  sessionStorage.clear();
  loginAdmin();
});
afterEach(() => vi.unstubAllGlobals());

function setup(extra: Parameters<typeof fakeApi>[0] = {}) {
  return fakeApi({
    "GET /api/v1/admin/me": { body: ME_OTP },
    "GET /api/v1/admin/labels": { body: LABELS },
    "GET /api/v1/admin/submissions": (c) => (c.query.get("cursor") === "c2"
      ? { body: { items: [listItem(9)], next_cursor: null } }
      : { body: { items: [listItem(1), listItem(2, { annotator: null, review: { status: "rejected", reason: "Kotak meleset", reviewed_at: "x" }, legacy_text_count: 2 })], next_cursor: "c2" } }),
    ...extra,
  });
}

it("kartu: sampul beroverlay, annotator (nama + nomor utuh), label + jumlah, status review; Anonim", async () => {
  setup();
  renderAdmin("/admin/anotasi");
  const cards = await screen.findAllByRole("listitem", { name: undefined });
  expect(cards.length).toBeGreaterThan(0);
  expect(screen.getByText("Budi 1")).toBeTruthy();
  expect(screen.getAllByText("6281234567890").length).toBeGreaterThan(0);
  expect(screen.getByText("Anonim")).toBeTruthy();
  expect(screen.getAllByText("Menunggu review", { selector: ".badge" }).length).toBe(1);
  expect(screen.getAllByText("Ditolak", { selector: ".badge" }).length).toBe(1);
  expect(screen.getByText("Alasan: Kotak meleset")).toBeTruthy();
  expect(screen.getByText("2 catatan lama")).toBeTruthy();
  expect(document.querySelectorAll(".sub-cover svg g[data-color='#e4572e']").length).toBe(2);
  expect(screen.getAllByRole("link", { name: /Buka submission/ })[0]!.getAttribute("href")).toBe("/admin/anotasi/s1");
});

it("filter ke query API (tanggal → RFC 3339, to eksklusif) dan URL; Muat lagi pakai cursor", async () => {
  const api = setup();
  renderAdmin("/admin/anotasi?from=2026-10-01&to=2026-10-31&label_id=3");
  await screen.findByText("Budi 1");
  const q = api.last("GET", "/api/v1/admin/submissions")!.query;
  expect(q.get("from")).toBe(dayStart("2026-10-01"));
  expect(q.get("to")).toBe(dayAfter("2026-10-31"));
  expect(q.get("label_id")).toBe("3");
  expect(q.get("limit")).toBe("24");

  fireEvent.change(screen.getByLabelText("Status review"), { target: { value: "accepted" } });
  await waitFor(() => expect(api.last("GET", "/api/v1/admin/submissions")!.query.get("review_status")).toBe("accepted"));
  expect(screen.getByTestId("where").textContent).toContain("review_status=accepted");

  fireEvent.change(screen.getByLabelText("Annotator"), { target: { value: "0812" } });
  fireEvent.click(screen.getByRole("button", { name: "Cari annotator" }));
  await waitFor(() => expect(api.last("GET", "/api/v1/admin/submissions")!.query.get("q")).toBe("0812"));
  fireEvent.click(screen.getByLabelText("Masih ada catatan lama"));
  await waitFor(() => expect(api.last("GET", "/api/v1/admin/submissions")!.query.get("unlabeled")).toBe("true"));

  // chip filter aktif bisa dihapus
  fireEvent.click(screen.getByRole("button", { name: "Hapus filter Label: Retak" }));
  await waitFor(() => expect(api.last("GET", "/api/v1/admin/submissions")!.query.get("label_id")).toBeNull());

  await screen.findByText("Budi 1");
  fireEvent.click(screen.getByRole("button", { name: "Muat lagi" }));
  expect(await screen.findByText("Budi 9")).toBeTruthy();
  expect(api.last("GET", "/api/v1/admin/submissions")!.query.get("cursor")).toBe("c2");
  expect(screen.queryByRole("button", { name: "Muat lagi" })).toBeNull();
});

it("pintasan Antrean review: pending + terlama dulu", async () => {
  const api = setup();
  renderAdmin("/admin/anotasi");
  await screen.findByText("Budi 1");
  fireEvent.click(screen.getByRole("button", { name: /Antrean review/ }));
  await waitFor(() => {
    const q = api.last("GET", "/api/v1/admin/submissions")!.query;
    expect([q.get("review_status"), q.get("sort")]).toEqual(["pending", "oldest"]);
  });
});

it("query salah → pesan + Coba lagi", async () => {
  setup({ "GET /api/v1/admin/submissions": apiError(422, "INVALID_QUERY") });
  renderAdmin("/admin/anotasi");
  expect(await screen.findByText(/Filter tidak valid/)).toBeTruthy();
});

describe("export", () => {
  beforeEach(() => {
    URL.createObjectURL = vi.fn(() => "blob:x");
    URL.revokeObjectURL = vi.fn();
  });

  it("fetch dengan Authorization, default review_status=accepted + rentang tanggal; nama berkas", async () => {
    const clicks: string[] = [];
    HTMLAnchorElement.prototype.click = function () { clicks.push(this.download); };
    const api = setup({ "GET /api/v1/admin/export": { text: '{"a":1}\n{"a":2}\n', headers: { "Content-Type": "application/x-ndjson" } } });
    renderAdmin("/admin/anotasi?from=2026-10-01&to=2026-10-31");
    await screen.findByText("Budi 1");
    fireEvent.click(screen.getByRole("button", { name: /Export JSONL/ }));
    const dlg = screen.getByRole("dialog", { name: "Export JSONL" });
    fireEvent.click(within(dlg).getByRole("button", { name: /Unduh/ }));
    expect(await screen.findByText("Export selesai: 2 baris (satu baris per foto).")).toBeTruthy();
    const c = api.last("GET", "/api/v1/admin/export")!;
    expect(c.headers.Authorization).toBe("Bearer adm2.tok");
    expect(c.query.get("review_status")).toBe("accepted");
    expect(c.query.get("from")).toBe(dayStart("2026-10-01"));
    expect(c.query.get("to")).toBe(dayAfter("2026-10-31"));
    expect(clicks).toEqual(["photo-annotated-2026-10-01-2026-10-31.jsonl"]);
  });

  it("export terpotong → peringatan", async () => {
    HTMLAnchorElement.prototype.click = () => {};
    setup({ "GET /api/v1/admin/export": { text: '{"a":1}\n{"a":', headers: { "Content-Type": "application/x-ndjson" } } });
    renderAdmin("/admin/anotasi");
    await screen.findByText("Budi 1");
    fireEvent.click(screen.getByRole("button", { name: /Export JSONL/ }));
    fireEvent.change(screen.getByLabelText("Status review", { selector: ".dialog select" }), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: /Unduh/ }));
    expect(await screen.findByText(/Export tidak lengkap/)).toBeTruthy();
  });

  it("checkJSONL", () => {
    expect(checkJSONL("", null)).toEqual({ rows: 0, complete: true });
    expect(checkJSONL('{"a":1}\n', "true")).toEqual({ rows: 1, complete: true });
    expect(checkJSONL('{"a":1}\n', "false").complete).toBe(false);
    expect(checkJSONL('{"a":1}', null).complete).toBe(false);
  });
});

describe("detail", () => {
  it("semua foto + kotak bernomor, label (arsip ditandai), ukuran px, catatan lama sebagai teks, annotator", async () => {
    setup({ "GET /api/v1/admin/submissions/:id": { body: detail() } });
    renderAdmin("/admin/anotasi/s1");
    expect(await screen.findByRole("heading", { name: "Foto 1 dari 2" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Foto 2 dari 2" })).toBeTruthy();
    expect(screen.getByText("Retak")).toBeTruthy();
    expect(screen.getByText("480 × 300 px")).toBeTruthy();
    expect(screen.getByText("Kerusakan <b>kiri</b>")).toBeTruthy();
    expect(screen.getByText("diarsipkan")).toBeTruthy();
    expect(screen.getByText("budi@example.com")).toBeTruthy();
    expect(screen.getByText("6281234567890")).toBeTruthy();
    expect(screen.getByRole("link", { name: /Semua dari annotator ini/ }).getAttribute("href")).toBe("/admin/anotasi?user_id=u1");
    expect(document.querySelectorAll(".photo-card svg g[data-color]").length).toBe(3);
  });

  it("404 → sudah dihapus", async () => {
    setup({ "GET /api/v1/admin/submissions/:id": apiError(404, "NOT_FOUND") });
    renderAdmin("/admin/anotasi/zz");
    expect(await screen.findByText("Submission ini tidak ditemukan atau sudah dihapus.")).toBeTruthy();
  });
});
