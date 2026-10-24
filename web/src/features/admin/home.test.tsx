import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { fakeApi } from "../../test/fakeApi";
import { assess, formatBytes } from "./systemStatus";
import { loginAdmin, ME_OTP, renderAdmin } from "./testUtils";
import type { SystemStatus } from "./types";

const base = (): SystemStatus => ({
  checked_at: "2026-10-24T10:00:00Z",
  api: { started_at: "2026-10-24T01:00:00Z", version: "3206339", auth_mode: "otp" },
  database: { ok: true, latency_ms: 3 },
  storage: { available: true, total_bytes: 250e9, free_bytes: 212e9, used_pct: 15.2, min_free_bytes: 5368709120, photos_pending: 4 },
  uploads: { active: 2, max: 200 },
  whatsapp: { otp_available: true, last_probe_at: null, queued: 0, max_queue: 200, sent_24h: 31, failed_24h: 1, daily_cap: 500 },
  cleanup: { last_run_at: "2026-10-24T09:00:00Z", last_ok: true, files_pending_delete: 0 },
  data: { submissions: 420, review_pending: 37, accepted: 300, rejected: 83, retention_due_7d: 5, users_active: 58, users_blocked: 2 },
  host: { cpu_temp_c: 52.1 },
});
const level = (s: SystemStatus, k: string) => assess(s).find((c) => c.key === k)!.level;

describe("ambang status (sama dengan alert Grafana)", () => {
  it("semua normal → ok", () => expect(assess(base()).every((c) => c.level === "ok")).toBe(true));
  it("SSD > 80% waspada; sisa < min_free bahaya; tidak tersedia bahaya", () => {
    expect(level({ ...base(), storage: { ...(base().storage as object), used_pct: 81, free_bytes: 47e9 } as SystemStatus["storage"] }, "storage")).toBe("warn");
    expect(level({ ...base(), storage: { ...(base().storage as object), used_pct: 99, free_bytes: 1e9 } as SystemStatus["storage"] }, "storage")).toBe("danger");
    expect(level({ ...base(), storage: { ...(base().storage as object), available: false } as SystemStatus["storage"] }, "storage")).toBe("danger");
  });
  it("WhatsApp: kanal mati bahaya; antrean > 80% / OTP harian > 80% waspada", () => {
    const wa = base().whatsapp as Exclude<SystemStatus["whatsapp"], { error: string }>;
    expect(level({ ...base(), whatsapp: { ...wa, otp_available: false } }, "whatsapp")).toBe("danger");
    expect(level({ ...base(), whatsapp: { ...wa, queued: 161 } }, "whatsapp")).toBe("warn");
    expect(level({ ...base(), whatsapp: { ...wa, sent_24h: 401 } }, "whatsapp")).toBe("warn");
    expect(level({ ...base(), whatsapp: { ...wa, sent_24h: 400 } }, "whatsapp")).toBe("ok");
  });
  it("suhu > 75 °C waspada; DB gagal bahaya; bagian error → bahaya tanpa menjatuhkan kartu lain", () => {
    expect(level({ ...base(), host: { cpu_temp_c: 76 } }, "host")).toBe("warn");
    expect(level({ ...base(), database: { ok: false, latency_ms: 0 } }, "database")).toBe("danger");
    const s = { ...base(), database: { error: "timeout" } };
    expect(level(s, "database")).toBe("danger");
    expect(assess(s).find((c) => c.key === "database")!.lines).toEqual(["timeout"]);
    expect(level(s, "storage")).toBe("ok");
  });
  it("formatBytes", () => {
    expect(formatBytes(250e9)).toBe("250.0 GB");
    expect(formatBytes(5368709120)).toBe("5.4 GB");
  });
});

beforeEach(() => {
  sessionStorage.clear();
  loginAdmin();
});
afterEach(() => vi.unstubAllGlobals());

it("beranda: kartu dengan ikon + teks status; ringkasan pekerjaan menaut ke antrean review", async () => {
  const s = { ...base(), whatsapp: { ...(base().whatsapp as object), otp_available: false } as SystemStatus["whatsapp"], host: { error: "tidak terbaca" } };
  fakeApi({ "GET /api/v1/admin/me": { body: ME_OTP }, "GET /api/v1/admin/system": { body: s } });
  renderAdmin("/admin");
  const wa = (await screen.findByText("WhatsApp / OTP")).closest("li")!;
  expect(within(wa).getByText("Bahaya")).toBeTruthy();
  expect(within(wa).getByText("Kanal tidak tersedia")).toBeTruthy();
  expect(within(screen.getByText("Database").closest("li")!).getByText("OK")).toBeTruthy();
  expect(screen.getByRole("meter", { name: "Pemakaian SSD" }).getAttribute("aria-valuenow")).toBe("15");
  const q = screen.getByRole("link", { name: /37\s*menunggu review/ });
  expect(q.getAttribute("href")).toBe("/admin/anotasi?review_status=pending&sort=oldest");
  expect(screen.getByRole("link", { name: /5\s*akan terhapus retensi dalam 7 hari/ })).toBeTruthy();
});

it("beranda dimuat ulang tiap 30 detik selama tab terlihat", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  try {
    const api = fakeApi({ "GET /api/v1/admin/me": { body: ME_OTP }, "GET /api/v1/admin/system": { body: base() } });
    renderAdmin("/admin");
    await screen.findByText("Database");
    expect(api.count("GET", "/api/v1/admin/system")).toBe(1);
    await vi.advanceTimersByTimeAsync(30_000);
    await waitFor(() => expect(api.count("GET", "/api/v1/admin/system")).toBe(2));
    fireEvent.click(screen.getByRole("button", { name: /Muat ulang/ }));
    await waitFor(() => expect(api.count("GET", "/api/v1/admin/system")).toBe(3));
  } finally {
    vi.useRealTimers();
  }
});

it("statistik: default diterima; urut terbanyak; label 0 contoh di bawah; filter tanggal", async () => {
  const api = fakeApi({
    "GET /api/v1/admin/me": { body: ME_OTP },
    "GET /api/v1/admin/stats/labels": { body: {
      totals: { submissions: 420, photos: 1130, annotations: 3870, legacy_text_annotations: 12 },
      labels: [
        { id: 5, name: "Penyok", color: "#2e86ab", archived: false, annotations: 300, photos: 100, submissions: 50, share: 0.165 },
        { id: 8, name: "Basah", color: "#1d3557", archived: false, annotations: 0, photos: 0, submissions: 0, share: 0 },
        { id: 3, name: "Retak", color: "#e4572e", archived: false, annotations: 1520, photos: 610, submissions: 300, share: 0.835 },
      ] } },
  });
  renderAdmin("/admin/statistik");
  await screen.findByText("Kotak per label");
  expect(api.last("GET", "/api/v1/admin/stats/labels")?.query.get("review_status")).toBe("accepted");
  const rows = within(screen.getByRole("table")).getAllByRole("row").slice(1);
  expect(rows.map((r) => within(r).getByRole("rowheader").textContent)).toEqual(["Retak", "Penyok"]);
  expect(within(rows[0]!).getByText("83,5%")).toBeTruthy();
  expect(within(rows[0]!).getByText("1.520")).toBeTruthy();
  expect(within(screen.getByRole("heading", { name: /Belum ada contoh/ }).closest("section")!).getByText("Basah")).toBeTruthy();
  fireEvent.change(screen.getByLabelText("Status review"), { target: { value: "" } });
  await waitFor(() => expect(api.last("GET", "/api/v1/admin/stats/labels")?.query.has("review_status")).toBe(false));
  fireEvent.change(screen.getByLabelText("Dari"), { target: { value: "2026-10-01" } });
  await waitFor(() => expect(api.last("GET", "/api/v1/admin/stats/labels")?.query.get("from")).toBeTruthy());
});
