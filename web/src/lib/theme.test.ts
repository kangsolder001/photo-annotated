/// <reference types="node" />
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { applyTheme, readPref, resolveTheme, theme, THEME_COLOR, THEME_KEY } from "./theme";

const WEB = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const html = readFileSync(join(WEB, "index.html"), "utf8");
const inline = html.match(/<script>([\s\S]*?)<\/script>/)?.[1] ?? "";

function mockSystem(dark: boolean) {
  window.matchMedia = ((q: string) => ({
    matches: dark && q.includes("dark"), media: q, addEventListener() {}, removeEventListener() {},
  })) as unknown as typeof window.matchMedia;
}

function runInline() {
  document.head.innerHTML = '<meta name="theme-color" content="#000000">';
  document.documentElement.removeAttribute("data-theme");
  new Function(inline)();
  return [document.documentElement.getAttribute("data-theme"), document.querySelector('meta[name="theme-color"]')?.getAttribute("content")];
}

beforeEach(() => localStorage.clear());

describe("skrip anti-kedip di index.html", () => {
  it("dipasang sebelum CSS/JS aplikasi dimuat", () => {
    expect(inline).not.toBe("");
    expect(html.indexOf("<script>")).toBeLessThan(html.indexOf('<script type="module"'));
    expect(html.indexOf('name="theme-color"')).toBeLessThan(html.indexOf("<script>"));
  });
  it.each<[string | null, boolean, string]>([
    [null, false, "light"],
    [null, true, "dark"],
    ["system", true, "dark"],
    ["light", true, "light"],
    ["dark", false, "dark"],
    ["rusak", true, "dark"],
  ])("pa_theme=%s, perangkat gelap=%s → %s", (stored, dark, want) => {
    mockSystem(dark);
    if (stored) localStorage.setItem(THEME_KEY, stored);
    expect(runInline()).toEqual([want, THEME_COLOR[want as "light" | "dark"]]);
  });
  it("storage diblokir → ikut perangkat, tanpa error", () => {
    mockSystem(true);
    const spy = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("SecurityError");
    });
    expect(runInline()[0]).toBe("dark");
    expect(readPref()).toBe("system");
    spy.mockRestore();
  });
  it("hash CSP skrip ada di nginx (dan dihitung otomatis untuk _headers di vite.config.ts)", () => {
    const hash = `'sha256-${createHash("sha256").update(inline).digest("base64")}'`;
    expect(readFileSync(join(WEB, "nginx", "security-headers.conf"), "utf8")).toContain(hash);
    expect(readFileSync(join(WEB, "vite.config.ts"), "utf8")).toContain("inlineScriptHashes");
  });
});

describe("theme", () => {
  it("resolveTheme", () => {
    expect(resolveTheme("system", true)).toBe("dark");
    expect(resolveTheme("system", false)).toBe("light");
    expect(resolveTheme("light", true)).toBe("light");
  });
  it("set menyimpan pilihan, memasang data-theme dan theme-color, memberi tahu pendengar", () => {
    mockSystem(false);
    document.head.innerHTML = '<meta name="theme-color" content="#000000">';
    const l = vi.fn();
    const off = theme.subscribe(l);
    theme.set("dark");
    expect(localStorage.getItem(THEME_KEY)).toBe("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(document.querySelector("meta")?.getAttribute("content")).toBe(THEME_COLOR.dark);
    expect(theme.pref()).toBe("dark");
    expect(l).toHaveBeenCalledTimes(1);
    theme.set("system");
    expect(document.documentElement.dataset.theme).toBe("light");
    off();
  });
  it("storage penuh/diblokir: pilihan tetap berlaku", () => {
    const spy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("QuotaExceeded");
    });
    theme.set("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    spy.mockRestore();
  });
  it("applyTheme", () => {
    applyTheme("light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });
});
