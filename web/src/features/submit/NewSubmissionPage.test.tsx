import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { apiError, fakeApi } from "../../test/fakeApi";
import { NewSubmissionPage } from "./NewSubmissionPage";

afterEach(() => vi.unstubAllGlobals());

const page = () => render(<MemoryRouter><NewSubmissionPage /></MemoryRouter>);

it("memuat label saat layar anotasi dibuka", async () => {
  const api = fakeApi({ "GET /api/v1/labels": { body: { labels: [{ id: 3, name: "Retak", color: "#e4572e" }] } } });
  page();
  await vi.waitFor(() => expect(api.count("GET", "/api/v1/labels")).toBe(1));
  expect(screen.queryByText(/Belum ada label/)).toBeNull();
});

it("daftar label kosong → pesan hubungi admin", async () => {
  fakeApi({ "GET /api/v1/labels": { body: { labels: [] } } });
  page();
  expect(await screen.findByText("Belum ada label. Hubungi admin.")).toBeTruthy();
});

it("gagal memuat label → pesan + Coba lagi", async () => {
  fakeApi({ "GET /api/v1/labels": apiError(503, "X") });
  page();
  expect(await screen.findByText(/Daftar label gagal dimuat/)).toBeTruthy();
  expect(screen.getByRole("button", { name: /Coba lagi/ })).toBeTruthy();
});
