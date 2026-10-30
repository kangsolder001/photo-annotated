import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { session } from "../../lib/session";
import { fakeApi } from "../../test/fakeApi";
import { surveyForm } from "../../test/surveyFixture";
import { SurveyPage } from "./SurveyPage";
import { resetSurveyFormCache } from "./useSurveyForm";

const user = { id: "u1", name: "Siti Aminah", email: "s@x.id", phone: "0812****789", role: "user" as const, nim: "251FS01017", group: { id: 3, name: "Kelompok 4 – CL-05" } };

beforeEach(() => {
  localStorage.clear();
  resetSurveyFormCache();
  session.save("tok", "2099-01-01T00:00:00Z", user);
  fakeApi({ "GET /api/v1/survey/form": { body: surveyForm }, "GET /api/v1/labels": { body: { labels: [] } } });
});
afterEach(() => vi.unstubAllGlobals());

const page = () => render(<MemoryRouter><SurveyPage /></MemoryRouter>);
const group = (name: string | RegExp) => screen.getByRole("group", { name });
const next = () => fireEvent.click(screen.getByRole("button", { name: "Lanjut" }));

it("bagian A: identitas dari akun, teks persetujuan, tanggal hari ini; Lanjut/Kembali dengan progres", async () => {
  page();
  expect(await screen.findByText("Bagian 1 dari 4")).toBeTruthy();
  expect(screen.getByText("Siti Aminah")).toBeTruthy();
  expect(screen.getByText("251FS01017")).toBeTruthy();
  expect(screen.getByText("Kelompok 4 – CL-05")).toBeTruthy();
  expect(screen.getByText(surveyForm.consent_text)).toBeTruthy();
  expect((screen.getByLabelText(/Tanggal pendataan/) as HTMLInputElement).value).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  expect((screen.getByRole("button", { name: "Kembali" }) as HTMLButtonElement).disabled).toBe(true);

  next();
  expect(screen.getByText("Bagian 2 dari 4")).toBeTruthy();
  expect(screen.getByRole("heading", { name: "1. Pengetahuan" })).toBeTruthy();
  // pilihan Ya/Tidak berupa tombol besar (radio)
  fireEvent.click(within(group(/memisahkan sampah residu/)).getByRole("radio", { name: "Ya" }));
  expect((within(group(/memisahkan sampah residu/)).getByRole("radio", { name: "Ya" }) as HTMLInputElement).checked).toBe(true);

  fireEvent.click(screen.getByRole("button", { name: "Kembali" }));
  expect(screen.getByText("Bagian 1 dari 4")).toBeTruthy();
  // bagian A yang ditinggalkan dengan isian kosong menampilkan error setelah kembali
  expect(screen.getAllByText("Wajib diisi.").length).toBeGreaterThan(0);
});

it("menolak di bagian A melewati bagian lain; ringkasan hanya bagian A", async () => {
  page();
  await screen.findByText("Bagian 1 dari 4");
  fireEvent.click(within(group(/Persetujuan pendataan/)).getByRole("radio", { name: "Tidak bersedia" }));
  expect(screen.getByText("Bagian 1 dari 1")).toBeTruthy();
  expect(screen.getByText(/hanya bagian A yang disimpan/)).toBeTruthy();
  next();
  expect(screen.getByRole("heading", { name: "Ringkasan" })).toBeTruthy();
  const items = within(screen.getByRole("list")).getAllByRole("button");
  expect(items).toHaveLength(1);
  expect(items[0]!.textContent).toContain("A. Identitas Pendataan");
  expect(items[0]!.textContent).toContain("Belum lengkap"); // RW masih kosong
  fireEvent.click(items[0]!);
  expect(screen.getByText("Bagian 1 dari 1")).toBeTruthy();
});

it("draf dipulihkan setelah mount ulang (versi form sama); Mulai dari awal membuang draf", async () => {
  const first = page();
  await screen.findByText("Bagian 1 dari 4");
  fireEvent.change(screen.getByLabelText(/^RW/), { target: { value: "05" } });
  next();
  first.unmount();

  page();
  expect(await screen.findByText("Isian yang belum dikirim dipulihkan.", { exact: false })).toBeTruthy();
  expect(screen.getByText("Bagian 2 dari 4")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Kembali" }));
  expect((screen.getByLabelText(/^RW/) as HTMLInputElement).value).toBe("05");

  fireEvent.click(screen.getByRole("button", { name: "Mulai dari awal" }));
  expect((screen.getByLabelText(/^RW/) as HTMLInputElement).value).toBe("");
});

it("draf versi form lain diabaikan", async () => {
  localStorage.setItem("pa.draft.u1.baru", JSON.stringify({ version: 0, savedAt: "", photos: [], answers: { a_rw: "09" }, form_version: 99, step: 2 }));
  page();
  await screen.findByText("Bagian 1 dari 4");
  expect((screen.getByLabelText(/^RW/) as HTMLInputElement).value).toBe("");
  expect(screen.queryByText(/dipulihkan/)).toBeNull();
});

it("multi dengan opsi Lainnya meminta teks", async () => {
  page();
  await screen.findByText("Bagian 1 dari 4");
  next(); next(); next();
  expect(screen.getByText("Bagian 4 dari 4")).toBeTruthy();
  fireEvent.click(within(group(/Jenis wadah/)).getByRole("checkbox", { name: "Lainnya" }));
  fireEvent.change(screen.getByLabelText("Sebutkan lainnya"), { target: { value: "Galon" } });
  expect((screen.getByLabelText("Sebutkan lainnya") as HTMLInputElement).value).toBe("Galon");
});
