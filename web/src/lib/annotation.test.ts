import { boxProblems, isBigEnough, moveBox, normalizeText, rectFromPoints, resizeBox, toDTO, type Box } from "./annotation";

const box = (p: Partial<Box> = {}): Box => ({ id: "b", x: 0.1, y: 0.1, w: 0.2, h: 0.2, text: "retak", ...p });

describe("rectFromPoints", () => {
  it("menerima seret ke arah mana pun dan memotong ke dalam foto", () => {
    expect(rectFromPoints(0.5, 0.6, 0.2, 0.1)).toEqual({ x: 0.2, y: 0.1, w: 0.3, h: expect.closeTo(0.5) });
    const r = rectFromPoints(-0.2, 0.9, 0.3, 1.4);
    expect(r).toEqual({ x: 0, y: 0.9, w: 0.3, h: expect.closeTo(0.1) });
  });
});

describe("moveBox", () => {
  it("tidak keluar dari foto", () => {
    expect(moveBox(box(), 2, 2)).toMatchObject({ x: 0.8, y: 0.8 });
    expect(moveBox(box(), -2, -2)).toMatchObject({ x: 0, y: 0 });
  });
});

describe("resizeBox", () => {
  it("menarik sudut kanan bawah", () => {
    expect(resizeBox(box(), "se", 0.6, 0.5)).toMatchObject({ x: 0.1, y: 0.1, w: 0.5, h: expect.closeTo(0.4) });
  });
  it("tidak bisa membalik atau membuat kotak di bawah ukuran minimum", () => {
    const b = resizeBox(box(), "se", 0, 0);
    expect(b.x).toBeCloseTo(0.1);
    expect(isBigEnough(b)).toBe(true);
  });
  it("sudut kiri atas memindahkan titik awal, sudut kanan bawah tetap", () => {
    const b = resizeBox(box(), "nw", 0.05, 0.02);
    expect(b.x + b.w).toBeCloseTo(0.3);
    expect(b.y + b.h).toBeCloseTo(0.3);
  });
});

describe("validasi (sama dengan backend)", () => {
  it.each<[Partial<Box>, string[]]>([
    [{}, []],
    [{ w: 0.004 }, ["too-small"]],
    [{ text: "   \n " }, ["empty-text"]],
    [{ text: "a".repeat(1001) }, ["text-too-long"]],
    [{ text: "\u{1F600}".repeat(1000) }, []], // dihitung per karakter, bukan per unit UTF-16
    [{ text: "a‮b" }, ["bad-char"]],
    [{ text: "baris1\nbaris2\tkolom" }, []], // tab dinormalkan menjadi spasi
    [{ text: "a\u0007b" }, ["bad-char"]],
    [{ text: "a‎b" }, []], // LRM diizinkan backend
  ])("%o → %o", (p, want) => expect(boxProblems(box(p))).toEqual(want));
});

it("toDTO memangkas teks dan menjaga kotak di dalam foto setelah pembulatan", () => {
  const d = toDTO(box({ x: 0.3333333333, w: 0.6666666667, text: "  catatan \r\n dua " }));
  expect(d.text).toBe("catatan \n dua");
  expect(d.x + d.w).toBeLessThanOrEqual(1);
  expect(normalizeText("\r\nx\r")).toBe("x");
});
