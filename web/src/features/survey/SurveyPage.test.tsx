import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { history } from "../../lib/history";
import { session } from "../../lib/session";
import { apiError, fakeApi } from "../../test/fakeApi";
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

it("menolak di bagian A melewati bagian lain: tombol Kirim langsung tampil", async () => {
  page();
  await screen.findByText("Bagian 1 dari 4");
  fireEvent.click(within(group(/Persetujuan pendataan/)).getByRole("radio", { name: "Tidak bersedia" }));
  expect(screen.getByText("Bagian 1 dari 1")).toBeTruthy();
  expect(screen.getByText(/hanya bagian A yang disimpan/)).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Lanjut" })).toBeNull();
  expect(screen.getByRole("button", { name: "Kirim" })).toBeTruthy();
  // berubah pikiran: bagian lain kembali berlaku
  fireEvent.click(within(group(/Persetujuan pendataan/)).getByRole("radio", { name: "Bersedia" }));
  expect(screen.getByText("Bagian 1 dari 4")).toBeTruthy();
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

describe("kirim (T68)", () => {
  const routes = () =>
    render(
      <MemoryRouter initialEntries={["/pendataan/baru"]}>
        <Routes>
          <Route path="/pendataan/baru" element={<SurveyPage />} />
          <Route path="/s/:id" element={<p>Halaman pendataan</p>} />
        </Routes>
      </MemoryRouter>,
    );
  const fillA = (consent: "Bersedia" | "Tidak bersedia") => {
    fireEvent.change(screen.getByLabelText(/^RW/), { target: { value: " 05 " } });
    fireEvent.click(within(group(/Persetujuan pendataan/)).getByRole("radio", { name: consent }));
  };

  it("menolak: tombol Kirim langsung di bagian A; body hanya bagian A tanpa foto; draf dibuang; riwayat dicatat", async () => {
    const api = fakeApi({
      "GET /api/v1/survey/form": { body: surveyForm },
      "GET /api/v1/labels": { body: { labels: [] } },
      "POST /api/v1/submissions": { status: 201, body: { submission_id: "s9", version: 1, created_at: "2026-11-02T03:00:00Z" } },
    });
    routes();
    await screen.findByText("Bagian 1 dari 4");
    fillA("Tidak bersedia");
    expect(screen.queryByRole("button", { name: "Lanjut" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Kirim" }));
    expect(await screen.findByText("Halaman pendataan")).toBeTruthy();
    const body = api.last("POST", "/api/v1/submissions")!.body as Record<string, unknown>;
    expect(body).toMatchObject({ form_version: 1, photos: [], answers: { a_rw: "05", a_consent: "no" } });
    expect(Object.keys(body.answers as object).sort()).toEqual(["a_consent", "a_date", "a_rw"]);
    expect(typeof body.client_request_id).toBe("string");
    expect(localStorage.getItem("pa.draft.u1.baru")).toBeNull();
    expect(history.get("s9", "u1")?.survey).toMatchObject({ rw: "05", consent: false });
  });

  it("menolak dengan bagian A belum lengkap → tidak dikirim, error tampil", async () => {
    const api = fakeApi({ "GET /api/v1/survey/form": { body: surveyForm }, "GET /api/v1/labels": { body: { labels: [] } } });
    routes();
    await screen.findByText("Bagian 1 dari 4");
    fireEvent.click(within(group(/Persetujuan pendataan/)).getByRole("radio", { name: "Tidak bersedia" }));
    fireEvent.click(screen.getByRole("button", { name: "Kirim" }));
    expect(await screen.findByText("Wajib diisi.")).toBeTruthy();
    expect(screen.getByText("Bagian A belum lengkap.")).toBeTruthy();
    expect(api.count("POST", "/api/v1/submissions")).toBe(0);
  });

  it("bersedia: wajib kurang atau 0 foto → tidak bisa dikirim; ringkasan menandai bagiannya", async () => {
    const api = fakeApi({ "GET /api/v1/survey/form": { body: surveyForm }, "GET /api/v1/labels": { body: { labels: [] } } });
    routes();
    await screen.findByText("Bagian 1 dari 4");
    fillA("Bersedia");
    next();
    fireEvent.click(within(group(/pentingnya memilah/)).getByRole("radio", { name: "Ya" }));
    fireEvent.click(within(group(/memisahkan sampah residu/)).getByRole("radio", { name: "Tidak" }));
    next(); next();
    // langkah foto R3: protokol di atas tombol kamera, maksimal 3 foto
    expect(screen.getByRole("button", { name: /Ambil foto/ })).toBeTruthy();
    expect(screen.getByText("Bisa menambah 3 foto lagi.")).toBeTruthy();
    next();
    expect(screen.getByRole("heading", { name: "Ringkasan" })).toBeTruthy();
    const items = within(screen.getByRole("list")).getAllByRole("button");
    expect(items.map((b) => b.textContent!.includes("Belum lengkap"))).toEqual([false, false, false, true]);
    fireEvent.click(screen.getByRole("button", { name: "Kirim" }));
    expect(screen.getByText("1 bagian belum lengkap.")).toBeTruthy();
    expect(api.count("POST", "/api/v1/submissions")).toBe(0);
    // bagian yang kurang bisa diklik: kembali ke langkah foto dengan pesannya
    fireEvent.click(items[3]!);
    expect(screen.getByText("Tambahkan minimal 1 foto.")).toBeTruthy();
  });

  it("422 INVALID_ANSWER dari server → pesan + bagian pertanyaannya dibuka", async () => {
    fakeApi({
      "GET /api/v1/survey/form": { body: surveyForm },
      "GET /api/v1/labels": { body: { labels: [] } },
      "POST /api/v1/submissions": { status: 422, body: { error: { code: "INVALID_ANSWER", message: "answers.a_rw: wajib diisi" } } },
    });
    routes();
    await screen.findByText("Bagian 1 dari 4");
    fillA("Tidak bersedia");
    fireEvent.click(screen.getByRole("button", { name: "Kirim" }));
    expect(await screen.findByText("answers.a_rw: wajib diisi")).toBeTruthy();
    expect(screen.getByText("Bagian 1 dari 1")).toBeTruthy();
  });

  it("error jaringan tidak membuang draf", async () => {
    fakeApi({
      "GET /api/v1/survey/form": { body: surveyForm },
      "GET /api/v1/labels": { body: { labels: [] } },
      "POST /api/v1/submissions": apiError(503, "STORAGE_UNAVAILABLE"),
    });
    routes();
    await screen.findByText("Bagian 1 dari 4");
    fillA("Tidak bersedia");
    fireEvent.click(screen.getByRole("button", { name: "Kirim" }));
    expect(await screen.findByText(/Penyimpanan server sedang bermasalah/)).toBeTruthy();
    await waitFor(() => expect(JSON.parse(localStorage.getItem("pa.draft.u1.baru")!).answers.a_consent).toBe("no"));
  });
});
