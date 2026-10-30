import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { apiError, fakeApi } from "../../test/fakeApi";
import { LoginPage } from "./LoginPage";

afterEach(() => vi.unstubAllGlobals());

it("akun diblokir (403 ACCOUNT_BLOCKED) → pesan khusus", async () => {
  fakeApi({ "POST /api/v1/auth/login": apiError(403, "ACCOUNT_BLOCKED") });
  render(<MemoryRouter><LoginPage /></MemoryRouter>);
  fireEvent.change(screen.getByLabelText(/Nomor WhatsApp/), { target: { value: "08123456789" } });
  fireEvent.change(screen.getByLabelText("Password", { exact: true }), { target: { value: "rahasia123" } });
  fireEvent.click(screen.getByRole("button", { name: "Masuk" }));
  expect(await screen.findByText("Akun Anda diblokir. Hubungi admin.")).toBeTruthy();
});

describe("daftar dengan NIM + kelompok (spec-pendataan §5.4)", () => {
  const groups = { groups: [{ id: 3, name: "Kelompok 4 – CL-05" }, { id: 5, name: "Kelompok 5" }] };
  const ticket = { challenge_id: "ch1", status: "queued", resend_after_seconds: 60, estimated_wait_seconds: 5 };

  function fill(nim = "251fs01017") {
    render(<MemoryRouter initialEntries={["/masuk?daftar=1"]}><LoginPage /></MemoryRouter>);
    fireEvent.change(screen.getByLabelText("Nama"), { target: { value: "Siti" } });
    fireEvent.change(screen.getByLabelText("Email"), { target: { value: "siti@contoh.id" } });
    fireEvent.change(screen.getByLabelText("NIM"), { target: { value: nim } });
    fireEvent.change(screen.getByLabelText(/Nomor WhatsApp/), { target: { value: "08123456789" } });
    fireEvent.change(screen.getByLabelText("Password", { exact: true }), { target: { value: "rahasia123" } });
    fireEvent.change(screen.getByLabelText("Ulangi password"), { target: { value: "rahasia123" } });
  }
  const submit = () => fireEvent.click(screen.getByRole("button", { name: /Daftar & kirim kode/ }));
  const pickGroup = async (id: string) => {
    await screen.findByRole("option", { name: "Kelompok 5" });
    fireEvent.change(screen.getByLabelText("Kelompok"), { target: { value: id } });
  };

  it("dropdown dari GET /groups; body berisi nim (huruf besar) + group_id", async () => {
    const f = fakeApi({ "GET /api/v1/groups": { body: groups }, "POST /api/v1/auth/register": { body: ticket } });
    fill();
    expect((screen.getByLabelText("NIM") as HTMLInputElement).value).toBe("251FS01017");
    expect(screen.getByLabelText("NIM").getAttribute("autocapitalize")).toBe("characters");
    await pickGroup("3");
    submit();
    await waitFor(() => expect(f.last("POST", "/api/v1/auth/register")).toBeTruthy());
    expect(f.last("POST", "/api/v1/auth/register")!.body).toMatchObject({ nim: "251FS01017", group_id: 3, name: "Siti" });
    expect(f.last("GET", "/api/v1/groups")!.headers.Authorization).toBeUndefined();
  });

  it("NIM tidak valid → pesan di field NIM, tanpa request", async () => {
    const f = fakeApi({ "GET /api/v1/groups": { body: groups } });
    fill("12-34");
    await pickGroup("5");
    submit();
    expect(await screen.findByText("NIM hanya huruf dan angka, 5–20 karakter.")).toBeTruthy();
    expect(screen.getByLabelText("NIM").getAttribute("aria-invalid")).toBe("true");
    expect(f.count("POST", "/api/v1/auth/register")).toBe(0);
  });

  it("409 NIM_TAKEN → pesan di field NIM", async () => {
    fakeApi({ "GET /api/v1/groups": { body: groups }, "POST /api/v1/auth/register": apiError(409, "NIM_TAKEN") });
    fill();
    await pickGroup("5");
    submit();
    const msg = await screen.findByText(/NIM sudah terdaftar/);
    expect(msg.id).toBe(screen.getByLabelText("NIM").getAttribute("aria-describedby"));
  });

  it("422 INVALID_GROUP → pesan di field kelompok, daftar dimuat ulang", async () => {
    const f = fakeApi({ "GET /api/v1/groups": { body: groups }, "POST /api/v1/auth/register": apiError(422, "INVALID_GROUP") });
    fill();
    await pickGroup("5");
    submit();
    const msg = await screen.findByText("Kelompok tidak tersedia. Pilih kelompok lain.");
    expect(msg.id).toBe(screen.getByLabelText("Kelompok").getAttribute("aria-describedby"));
    await waitFor(() => expect(f.count("GET", "/api/v1/groups")).toBe(2));
  });

  it("tanpa kelompok aktif → teks 'Belum ada kelompok' dan tombol daftar mati", async () => {
    fakeApi({ "GET /api/v1/groups": { body: { groups: [] } } });
    fill();
    expect(await screen.findByText("Belum ada kelompok. Hubungi admin.")).toBeTruthy();
    expect((screen.getByRole("button", { name: /Daftar & kirim kode/ }) as HTMLButtonElement).disabled).toBe(true);
  });
});
