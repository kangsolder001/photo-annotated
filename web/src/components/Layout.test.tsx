import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { session } from "../lib/session";
import { fakeApi } from "../test/fakeApi";
import { Layout } from "./Layout";

vi.mock("../config", async (orig) => ({ ...(await orig<typeof import("../config")>()), config: { apiBase: "", authMode: "otp", turnstileSiteKey: "" } }));

beforeEach(() => {
  localStorage.clear();
  session.save("tok", "2099-01-01T00:00:00Z", { id: "u1", name: "Budi", email: "b@example.com", phone: "6281200000001" } as never);
});
afterEach(() => vi.unstubAllGlobals());

function renderApp() {
  render(
    <MemoryRouter initialEntries={["/riwayat"]}>
      <Routes>
        <Route path="/masuk" element={<p>Halaman masuk</p>} />
        <Route path="*" element={<Layout><p>Isi</p></Layout>} />
      </Routes>
    </MemoryRouter>,
  );
}

it("Keluar meminta konfirmasi; Batal tetap masuk", async () => {
  const api = fakeApi({ "POST /api/v1/auth/logout": { status: 204 } });
  renderApp();
  fireEvent.click(screen.getByRole("button", { name: "Keluar" }));
  const dlg = screen.getByRole("dialog", { name: "Keluar dari akun?" });
  fireEvent.click(within(dlg).getByRole("button", { name: "Batal" }));
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(api.count("POST", "/api/v1/auth/logout")).toBe(0);
  expect(screen.getByText("Isi")).toBeTruthy();
});

it("Keluar lalu konfirmasi → logout dan ke halaman masuk", async () => {
  const api = fakeApi({ "POST /api/v1/auth/logout": { status: 204 } });
  renderApp();
  fireEvent.click(screen.getByRole("button", { name: "Keluar" }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Keluar" }));
  expect(await screen.findByText("Halaman masuk")).toBeTruthy();
  await waitFor(() => expect(api.count("POST", "/api/v1/auth/logout")).toBe(1));
});

it("HP: burger membuka panel menu (navigasi + tema), memilih menu menutupnya, Escape juga", () => {
  renderApp();
  const toggle = screen.getByRole("button", { name: "Buka menu" });
  expect(toggle.getAttribute("aria-expanded")).toBe("false");
  fireEvent.click(toggle);
  expect(screen.getByRole("button", { name: "Tutup menu" }).getAttribute("aria-expanded")).toBe("true");
  expect(document.getElementById("app-menu")!.className).toContain("open");
  expect(within(document.getElementById("app-menu")!).getByRole("radiogroup", { name: "Tema" })).toBeTruthy();
  fireEvent.click(screen.getByRole("link", { name: "Riwayat" }));
  expect(screen.getByRole("button", { name: "Buka menu" })).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Buka menu" }));
  fireEvent.keyDown(window, { key: "Escape" });
  expect(document.getElementById("app-menu")!.className).not.toContain("open");
});
