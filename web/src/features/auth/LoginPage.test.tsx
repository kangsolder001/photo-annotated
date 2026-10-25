import { fireEvent, render, screen } from "@testing-library/react";
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
