import { normalizeText, textProblem } from "./text";

describe("textProblem (sama dengan backend)", () => {
  it.each<[string, number, ReturnType<typeof textProblem>]>([
    ["Retak", 50, null],
    ["   \n ", 50, "empty"],
    ["a".repeat(51), 50, "too-long"],
    ["\u{1F600}".repeat(50), 50, null], // dihitung per karakter, bukan per unit UTF-16
    ["a‮b", 50, "bad-char"],
    ["baris1\nbaris2\tkolom", 500, null], // tab dinormalkan menjadi spasi
    ["a\u0007b", 500, "bad-char"],
    ["a‎b", 500, null], // LRM diizinkan backend
  ])("%j (maks %i) → %s", (t, max, want) => expect(textProblem(t, max)).toBe(want));

  it("opsional: kosong boleh", () => {
    expect(textProblem("  ", 200, { optional: true })).toBeNull();
    expect(textProblem("x".repeat(201), 200, { optional: true })).toBe("too-long");
  });

  it("normalizeText", () => {
    expect(normalizeText("  catatan \r\n dua ")).toBe("catatan \n dua");
    expect(normalizeText("\r\nx\r")).toBe("x");
  });
});
