import { boxProblems, clearUnavailable, fromDraft, fromView, isBigEnough, moveBox, rectFromPoints, resizeBox, toDraft, toDTO, type Box } from "./annotation";

const box = (p: Partial<Box> = {}): Box => ({ id: "b", x: 0.1, y: 0.1, w: 0.2, h: 0.2, labelId: 3, ...p });

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
    [{ labelId: null }, ["no-label"]],
    [{ labelId: null, flagged: true }, ["label-unavailable"]],
    [{ labelId: null, legacyText: "catatan lama" }, ["no-label"]], // teks lama tidak menggantikan label
    [{ w: 0.001, labelId: null }, ["too-small", "no-label"]],
  ])("%o → %o", (p, want) => expect(boxProblems(box(p))).toEqual(want));
});

it("toDTO mengirim label_id (tanpa teks) dan menjaga kotak di dalam foto setelah pembulatan", () => {
  const d = toDTO(box({ x: 0.3333333333, w: 0.6666666667, legacyText: "lama" }));
  expect(d).toEqual({ label_id: 3, x: 0.333333, y: 0.1, w: 0.666667, h: 0.2 });
  expect(d.x + d.w).toBeLessThanOrEqual(1);
  expect(() => toDTO(box({ labelId: null }))).toThrow();
});

it("fromView: label → labelId; baris lama → teks sebagai catatan lama", () => {
  const base = { x: 0.1, y: 0.2, w: 0.3, h: 0.4 };
  expect(fromView({ ...base, label: { id: 3, name: "Retak", color: "#e4572e", archived: false }, text: null }))
    .toMatchObject({ ...base, labelId: 3, legacyText: null });
  expect(fromView({ ...base, label: null, text: "Kerusakan di sisi kiri" }))
    .toMatchObject({ labelId: null, legacyText: "Kerusakan di sisi kiri" });
});

it("toDraft/fromDraft mempertahankan label kosong dan catatan lama", () => {
  const d = toDraft(box({ labelId: null, legacyText: "lama" }));
  expect(d).toEqual({ label_id: null, x: 0.1, y: 0.1, w: 0.2, h: 0.2, legacy_text: "lama" });
  expect(fromDraft(d)).toMatchObject({ labelId: null, legacyText: "lama" });
});

it("clearUnavailable mengosongkan label yang tidak tersedia dan menandai kotaknya", () => {
  const boxes = [box({ id: "a", labelId: 3 }), box({ id: "b", labelId: 9 }), box({ id: "c", labelId: null })];
  const out = clearUnavailable(boxes, (id) => id === 3);
  expect(out.map((b) => [b.labelId, !!b.flagged])).toEqual([[3, false], [null, true], [null, false]]);
  expect(boxProblems(out[1]!)).toEqual(["label-unavailable"]);
  expect(clearUnavailable(boxes, () => true)).toBe(boxes); // tanpa perubahan: array yang sama
});
