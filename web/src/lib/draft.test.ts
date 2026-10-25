import type { AnnotationView } from "../api/types";
import { drafts, migrateAnnotation, sameAsSaved, type Draft } from "./draft";
import { writeJSON } from "./storage";
import { formatRemaining } from "./time";

const ann = { label_id: 3, x: 0.1, y: 0.1, w: 0.2, h: 0.2 };
const view: AnnotationView = { label: { id: 3, name: "Retak", color: "#e4572e", archived: false }, text: null, x: 0.1, y: 0.1, w: 0.2, h: 0.2 };
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

it("draf lama berisi teks: kotak dipertahankan, label kosong, teks jadi catatan lama", () => {
  writeJSON("pa.draft.anon.s1", {
    version: 2, savedAt: "x",
    photos: [{ photoId: "p1", annotations: [{ text: "Penyok", x: 0.1, y: 0.2, w: 0.3, h: 0.4 }, { text: "  ", x: 0.5, y: 0.5, w: 0.1, h: 0.1 }, { bogus: 1 }] }],
  });
  expect(drafts.load("s1", "anon", 2)?.photos[0]?.annotations).toEqual([
    { label_id: null, x: 0.1, y: 0.2, w: 0.3, h: 0.4, legacy_text: "Penyok" },
    { label_id: null, x: 0.5, y: 0.5, w: 0.1, h: 0.1 },
  ]);
});

it("migrateAnnotation menolak kotak rusak dan label bukan bilangan bulat", () => {
  expect(migrateAnnotation(null)).toBeNull();
  expect(migrateAnnotation({ label_id: 3, x: "1", y: 0, w: 0, h: 0 })).toBeNull();
  expect(migrateAnnotation({ label_id: 3.5, x: 0, y: 0, w: 0.1, h: 0.1 })?.label_id).toBeNull();
});

it("sameAsSaved membandingkan foto dan label kotak", () => {
  expect(sameAsSaved(d, [{ photo_id: "p1", annotations: [view] }])).toBe(true);
  expect(sameAsSaved(d, [{ photo_id: "p1", annotations: [{ ...view, label: { ...view.label!, id: 4 } }] }])).toBe(false);
  // laporan lama (teks bebas) vs draf yang belum memilih label: sama
  const legacy: AnnotationView = { ...view, label: null, text: "lama" };
  const dl: Draft = { ...d, photos: [{ photoId: "p1", annotations: [{ ...ann, label_id: null, legacy_text: "lama" }] }] };
  expect(sameAsSaved(dl, [{ photo_id: "p1", annotations: [legacy] }])).toBe(true);
});

it.each([
  [0, "habis"],
  [30_000, "kurang dari 1 menit"],
  [45 * 60_000, "45 menit"],
  [3 * 3600_000, "3 jam"],
  [(3 * 60 + 12) * 60_000 + 59_000, "3 jam 12 menit"],
])("formatRemaining(%i) = %s", (ms, want) => expect(formatRemaining(ms)).toBe(want));
