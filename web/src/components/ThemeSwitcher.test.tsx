import { fireEvent, render, screen } from "@testing-library/react";
import { theme } from "../lib/theme";
import { ThemeSwitcher } from "./ThemeSwitcher";

beforeEach(() => {
  localStorage.clear();
  theme.set("system");
});

it("menu: tombol ikon berlabel membuka pilihan Sistem/Terang/Gelap", () => {
  render(<ThemeSwitcher />);
  fireEvent.click(screen.getByRole("button", { name: "Tema: Sistem" }));
  fireEvent.click(screen.getByRole("menuitemradio", { name: "Gelap" }));
  expect(document.documentElement.dataset.theme).toBe("dark");
  expect(localStorage.getItem("pa_theme")).toBe("dark");
  expect(screen.queryByRole("menu")).toBeNull();
  expect(screen.getByRole("button", { name: "Tema: Gelap" })).toBeTruthy();
});

it("menu tertutup dengan Escape", () => {
  render(<ThemeSwitcher />);
  fireEvent.click(screen.getByRole("button", { name: /Tema/ }));
  expect(screen.getByRole("menu")).toBeTruthy();
  fireEvent.keyDown(document, { key: "Escape" });
  expect(screen.queryByRole("menu")).toBeNull();
});

it("inline: radio group", () => {
  render(<ThemeSwitcher variant="inline" />);
  fireEvent.click(screen.getByRole("radio", { name: /Terang/ }));
  expect(screen.getByRole("radio", { name: /Terang/ }).getAttribute("aria-checked")).toBe("true");
  expect(document.documentElement.dataset.theme).toBe("light");
});
