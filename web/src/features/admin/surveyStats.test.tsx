import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { fakeApi } from "../../test/fakeApi";
import { loginAdmin, ME_OTP, renderAdmin } from "./testUtils";
import type { SurveyStats } from "./types";

// Statistik pendataan (spec-pendataan §5.8): fixture mengikuti contoh JSON spec.
const STATS: SurveyStats = {
  totals: { households: 120, consented: 108, declined: 12 },
  by_rw: [{ rw: "05", households: 60, consented: 55, declined: 5 }, { rw: "07", households: 60, consented: 53, declined: 7 }],
  by_student: [{ user_id: "u1", name: "Siti Aminah", nim: "251FS01017", group: "Kelompok 4 – CL-05", households: 14, declined: 1 }],
  b: [
    { id: "b1", label: "Apakah keluarga mengetahui pentingnya memilah sampah dari rumah?", yes: 80, no: 28, yes_share: 0.741 },
    { id: "b2", label: "Apakah keluarga mengetahui perbedaan sampah organik dan anorganik?", yes: 54, no: 54, yes_share: 0.5 },
  ],
  c: [{ id: "c1", label: "Terdapat wadah sampah organik", yes: 40, no: 50, na: 18 }],
  r3_sorting: { sorted: 20, partial: 40, mixed: 45, unknown: 3 },
};

beforeEach(() => {
  sessionStorage.clear();
  loginAdmin();
});
afterEach(() => vi.unstubAllGlobals());

const section = (name: RegExp) => screen.getByRole("heading", { name }).closest("section")!;

it("render dari fixture: kartu total, per RW, per mahasiswa, % Ya per butir B, distribusi C, kondisi pemilahan", async () => {
  fakeApi({ "GET /api/v1/admin/me": { body: ME_OTP }, "GET /api/v1/admin/stats/survey": { body: STATS } });
  renderAdmin("/admin/statistik");
  expect(await screen.findByText("rumah tangga")).toBeTruthy();
  expect(screen.getByText("120")).toBeTruthy();
  expect(screen.getByText("108")).toBeTruthy();
  expect(screen.getByText("menolak (10%)")).toBeTruthy();

  const rw = within(section(/Per RW/)).getAllByRole("row").slice(1);
  expect(rw.map((r) => within(r).getByRole("rowheader").textContent)).toEqual(["05", "07"]);
  expect(within(rw[1]!).getByText("53")).toBeTruthy();

  const st = within(section(/Per mahasiswa/));
  expect(st.getByText("Siti Aminah")).toBeTruthy();
  expect(st.getByText("251FS01017")).toBeTruthy();
  expect(st.getByText("Kelompok 4 – CL-05")).toBeTruthy();

  const b = within(section(/Checklist B/)).getAllByRole("row").slice(1);
  expect(within(b[0]!).getByText("74,1%")).toBeTruthy();
  expect(within(b[0]!).getByRole("rowheader").textContent).toContain("B1");
  expect((b[0]!.querySelector(".bar") as HTMLElement).style.width).toBe("74.1%");
  expect((b[1]!.querySelector(".bar") as HTMLElement).style.width).toBe("50%");

  const c = within(section(/Observasi C/)).getAllByRole("row");
  expect(c[0]!.textContent).toContain("Tidak teramati");
  expect(within(c[1]!).getByText("18")).toBeTruthy();

  const sorting = within(section(/Kondisi pemilahan/)).getAllByRole("row").slice(1);
  expect(sorting.map((r) => within(r).getByRole("rowheader").textContent)).toEqual(["Terpilah", "Sebagian", "Tercampur", "Tidak bisa dinilai"]);
  expect(within(sorting[2]!).getByText("45")).toBeTruthy();
  expect(within(sorting[2]!).getByText("41,7%")).toBeTruthy(); // 45 / 108
  expect(screen.getByRole("link", { name: "Statistik label kotak" }).getAttribute("href")).toBe("/admin/statistik/label");
});

it("filter tanggal (YYYY-MM-DD) dan RW ke query; kosong = semua", async () => {
  const api = fakeApi({ "GET /api/v1/admin/me": { body: ME_OTP }, "GET /api/v1/admin/stats/survey": { body: STATS } });
  renderAdmin("/admin/statistik");
  await screen.findByText("rumah tangga");
  expect([...api.last("GET", "/api/v1/admin/stats/survey")!.query.keys()]).toEqual([]);
  fireEvent.change(screen.getByLabelText("Dari"), { target: { value: "2026-10-01" } });
  await waitFor(() => expect(api.last("GET", "/api/v1/admin/stats/survey")!.query.get("from")).toBe("2026-10-01"));
  fireEvent.change(screen.getByLabelText("RW"), { target: { value: " 05 " } });
  fireEvent.blur(screen.getByLabelText("RW"));
  await waitFor(() => expect(api.last("GET", "/api/v1/admin/stats/survey")!.query.get("rw")).toBe("05"));
  expect(screen.getByTestId("where").textContent).toContain("rw=05");
});
