import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { fakeApi } from "../../test/fakeApi";
import { surveyForm } from "../../test/surveyFixture";
import { resetSurveyFormCache } from "../survey/useSurveyForm";
import { dayAfter, dayStart, csvFilename, filtersToParams, parseFilters, toQuery } from "./filters";
import { detail, listItem } from "./fixtures";
import { loginAdmin, ME_OTP, renderAdmin } from "./testUtils";

// Admin: daftar/detail pendataan + export CSV (spec-pendataan §5.6, §5.7, §7.2).

const GROUPS = { groups: [{ id: 3, name: "Kelompok 4 – CL-05", archived: false, user_count: 2, created_at: "", updated_at: "" }] };
const student = { id: "u1", name: "Siti Aminah", email: "s@x.id", phone: "6281234567890", blocked: false, nim: "251FS01017", group: { id: 3, name: "Kelompok 4 – CL-05" } };

beforeEach(() => {
  sessionStorage.clear();
  resetSurveyFormCache();
  loginAdmin();
});
afterEach(() => vi.unstubAllGlobals());

function setup(extra: Parameters<typeof fakeApi>[0] = {}) {
  return fakeApi({
    "GET /api/v1/admin/me": { body: ME_OTP },
    "GET /api/v1/admin/labels": { body: { labels: [] } },
    "GET /api/v1/admin/groups": { body: GROUPS },
    "GET /api/v1/admin/submissions": {
      body: {
        items: [
          listItem(1, { annotator: student, a_date: "2026-10-02", a_rw: "05", a_rt: "03", a_household_code: "RT03-RW05-12", consent: true }),
          listItem(2, { a_date: "2026-10-03", a_rw: "05", a_rt: "04", a_household_code: "RT04-RW05-07", consent: false, photo_count: 0, cover: null }),
        ],
        next_cursor: null,
      },
    },
    ...extra,
  });
}

it("filter consent/rw/group_id: URL, filter, dan query API", () => {
  const f = parseFilters(new URLSearchParams("consent=no&rw=%2005%20&group_id=3&consent2=x"));
  expect(f).toEqual({ consent: "no", rw: "05", group_id: 3 });
  expect(parseFilters(new URLSearchParams("consent=maybe&group_id=abc")).consent).toBeUndefined();
  expect(filtersToParams(f).toString()).toBe("consent=no&rw=05&group_id=3");
  expect(toQuery(f)).toMatchObject({ consent: "no", rw: "05", group_id: 3 });
  expect(csvFilename({ from: "2026-10-01" })).toBe("pendataan-2026-10-01-sekarang.csv");
});

it("daftar: tanggal pendataan, RT/RW, kode rumah tangga, mahasiswa (nama + NIM), Bersedia/Menolak; filter ke query", async () => {
  const api = setup();
  renderAdmin("/admin/anotasi?rw=05&group_id=3");
  expect(await screen.findByText("RT 03 / RW 05 · RT03-RW05-12")).toBeTruthy();
  expect(screen.getByRole("heading", { name: "Pendataan" })).toBeTruthy();
  expect(screen.getByText("2 Okt 2026")).toBeTruthy();
  expect(screen.getByText("Siti Aminah")).toBeTruthy();
  expect(screen.getByText(/251FS01017/)).toBeTruthy();
  expect(screen.getByText("Bersedia", { selector: ".badge" })).toBeTruthy();
  expect(screen.getByText("Menolak", { selector: ".badge" })).toBeTruthy();
  let q = api.last("GET", "/api/v1/admin/submissions")!.query;
  expect(q.get("rw")).toBe("05");
  expect(q.get("group_id")).toBe("3");

  fireEvent.change(screen.getByLabelText("Persetujuan"), { target: { value: "no" } });
  await waitFor(() => expect(api.last("GET", "/api/v1/admin/submissions")!.query.get("consent")).toBe("no"));
  expect(screen.getByTestId("where").textContent).toContain("consent=no");

  await within(screen.getByLabelText("Kelompok")).findByRole("option", { name: "Kelompok 4 – CL-05" });
  fireEvent.change(screen.getByLabelText("Kelompok"), { target: { value: "" } });
  await waitFor(() => expect(api.last("GET", "/api/v1/admin/submissions")!.query.get("group_id")).toBeNull());

  fireEvent.change(screen.getByLabelText("RW"), { target: { value: "07" } });
  fireEvent.blur(screen.getByLabelText("RW"));
  await waitFor(() => expect(api.last("GET", "/api/v1/admin/submissions")!.query.get("rw")).toBe("07"));
  q = api.last("GET", "/api/v1/admin/submissions")!.query;
  expect(q.get("consent")).toBe("no");
});

it("Export CSV: fetch bertoken dengan filter aktif (tanggal, consent, review_status); nama berkas .csv", async () => {
  URL.createObjectURL = vi.fn(() => "blob:x");
  URL.revokeObjectURL = vi.fn();
  const clicks: string[] = [];
  HTMLAnchorElement.prototype.click = function () { clicks.push(this.download); };
  const api = setup({ "GET /api/v1/admin/export.csv": { text: "\uFEFFsubmission_id,created_at\r\ns1,2026\r\n", headers: { "Content-Type": "text/csv; charset=utf-8" } } });
  renderAdmin("/admin/anotasi?from=2026-10-01&to=2026-10-31&consent=yes&review_status=accepted");
  await screen.findByText("RT 03 / RW 05 · RT03-RW05-12");
  fireEvent.click(screen.getByRole("button", { name: /Export CSV/ }));
  expect(await screen.findByText(/Export CSV selesai/)).toBeTruthy();
  const c = api.last("GET", "/api/v1/admin/export.csv")!;
  expect(c.headers.Authorization).toBe("Bearer adm2.tok");
  expect(c.query.get("from")).toBe(dayStart("2026-10-01"));
  expect(c.query.get("to")).toBe(dayAfter("2026-10-31"));
  expect(c.query.get("consent")).toBe("yes");
  expect(c.query.get("review_status")).toBe("accepted");
  expect(clicks).toEqual(["pendataan-2026-10-01-2026-10-31.csv"]);
});

it("detail: jawaban per bagian dengan teks opsi (bukan id), NIM + kelompok mahasiswa", async () => {
  const answers = { a_date: "2026-10-02", a_rw: "05", a_consent: "yes", b1: "yes", b7: "no", r3_container_types: ["bucket", "other"], r3_container_types_other: "Galon" };
  setup({
    "GET /api/v1/admin/submissions/:id": { body: detail({ annotator: student, form_version: 1, answers }) },
    "GET /api/v1/survey/form": { body: surveyForm },
  });
  renderAdmin("/admin/anotasi/s1");
  const a = await screen.findByRole("region", { name: "A. Identitas Pendataan" });
  expect(within(a).getByText("Bersedia")).toBeTruthy();
  expect(within(a).queryByText("yes")).toBeNull();
  const b = screen.getByRole("region", { name: "1. Pengetahuan" });
  expect(within(b).getByText("Ya")).toBeTruthy();
  expect(within(b).getByText("Tidak")).toBeTruthy();
  expect(screen.getByText("Ember; Lainnya: Galon")).toBeTruthy();
  expect(screen.getByText("251FS01017")).toBeTruthy();
  expect(screen.getAllByText("Kelompok 4 – CL-05").length).toBeGreaterThan(0);
  // foto dengan overlay kotak tetap tampil
  expect(screen.getByRole("region", { name: "Foto 1" })).toBeTruthy();
});
