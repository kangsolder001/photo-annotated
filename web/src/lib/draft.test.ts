import { drafts, sameAsSaved, type Draft } from "./draft";
import { formatRemaining } from "./time";

const ann = { text: "a", x: 0.1, y: 0.1, w: 0.2, h: 0.2 };
const d: Draft = { version: 2, savedAt: "2026-10-24T00:00:00Z", photos: [{ photoId: "p1", annotations: [ann] }] };

beforeEach(() => localStorage.clear());

it("draf pulih setelah reload untuk versi yang sama", () => {
  drafts.save("s1", "anon", d);
  expect(drafts.load("s1", "anon", 2)).toEqual(d);
});

it("draf versi lama dibuang (laporan sudah diubah di tempat lain)", () => {
  drafts.save("s1", "anon", d);
  expect(drafts.load("s1", "anon", 3)).toBeNull();
  expect(drafts.load("s1", "anon", 2)).toBeNull(); // sudah dihapus
});

it("draf terpisah per pemilik", () => {
  drafts.save("s1", "u1", d);
  expect(drafts.load("s1", "u2", 2)).toBeNull();
});

it("sameAsSaved membandingkan foto dan anotasi", () => {
  expect(sameAsSaved(d, [{ photo_id: "p1", annotations: [ann] }])).toBe(true);
  expect(sameAsSaved(d, [{ photo_id: "p1", annotations: [{ ...ann, text: "b" }] }])).toBe(false);
});

it.each([
  [0, "habis"],
  [30_000, "kurang dari 1 menit"],
  [45 * 60_000, "45 menit"],
  [3 * 3600_000, "3 jam"],
  [(3 * 60 + 12) * 60_000 + 59_000, "3 jam 12 menit"],
])("formatRemaining(%i) = %s", (ms, want) => expect(formatRemaining(ms)).toBe(want));
