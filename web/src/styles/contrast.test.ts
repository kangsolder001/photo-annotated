/// <reference types="node" />
// @vitest-environment node
// Kontras WCAG AA atas token CSS kedua tema (spec-labels-admin §7.4): teks ≥ 4.5:1, tepi kontrol
// dan indikator fokus ≥ 3:1. --border (pemisah dekoratif) sengaja tidak dicek.
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { contrast, textOn } from "../lib/color";
import { THEME_COLOR } from "../lib/theme";

const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "global.css"), "utf8");

function block(selector: string): Record<string, string> {
  const start = css.indexOf(`${selector} {`);
  if (start < 0) throw new Error(`blok ${selector} tidak ada`);
  const body = css.slice(start, css.indexOf("\n}", start));
  return Object.fromEntries([...body.matchAll(/--([\w-]+):\s*(#[0-9a-f]{6})\b/gi)].map((m) => [m[1]!, m[2]!.toLowerCase()]));
}

const light = block(":root");
const dark = { ...light, ...block(':root[data-theme="dark"]') };

// [teks, latar, minimum]
const PAIRS: [string, string, number][] = [
  ["text", "bg", 4.5], ["text", "surface", 4.5], ["text", "primary-soft", 4.5],
  ["muted", "bg", 4.5], ["muted", "surface", 4.5], ["muted", "primary-soft", 4.5],
  ["primary", "bg", 4.5], ["primary", "surface", 4.5], ["primary", "primary-soft", 4.5],
  ["primary-text", "primary", 4.5],
  ["danger", "surface", 4.5], ["danger", "bg", 4.5], ["danger", "danger-soft", 4.5],
  ["warn", "surface", 4.5], ["warn", "bg", 4.5], ["warn", "warn-soft", 4.5],
  ["ok", "surface", 4.5], ["ok", "bg", 4.5], ["ok", "primary-soft", 4.5],
  ["border-strong", "surface", 3], ["border-strong", "bg", 3],
  ["focus", "surface", 3], ["focus", "bg", 3],
];

describe.each([["terang hangat", light], ["gelap", dark]])("tema %s", (_n, t) => {
  it.each(PAIRS)("--%s di atas --%s ≥ %s:1", (fg, bg, min) => {
    expect(t[fg], `--${fg}`).toBeDefined();
    expect(t[bg], `--${bg}`).toBeDefined();
    expect(contrast(t[fg]!, t[bg]!)).toBeGreaterThanOrEqual(min);
  });
});

it("tema gelap = tema gelap lama (latar, aksen toska)", () => {
  expect(dark).toMatchObject({ bg: "#111416", surface: "#1b1f22", text: "#e8eaed", primary: "#2bb3a3" });
});

it("tema terang hangat: latar krem, teks cokelat tua", () => {
  expect(light).toMatchObject({ bg: "#f5ecdd", surface: "#fcf6ec", text: "#2e2419" });
});

it("theme-color = --surface tiap tema", () => {
  expect(THEME_COLOR).toEqual({ light: light.surface, dark: dark.surface });
});

it("contrast() sesuai WCAG; textOn memilih teks yang terbaca di atas warna label", () => {
  expect(contrast("#000000", "#ffffff")).toBeCloseTo(21);
  expect(contrast("#777777", "#ffffff")).toBeCloseTo(4.48, 1);
  expect(textOn("#f59e0b")).toBe("#111111");
  expect(textOn("#1f3a5f")).toBe("#ffffff");
  for (const c of ["#e4572e", "#2e86ab", "#a23b72", "#f18f01", "#3b1f2b", "#44af69", "#808080"]) {
    expect(contrast(textOn(c), c)).toBeGreaterThanOrEqual(4.5);
  }
});
