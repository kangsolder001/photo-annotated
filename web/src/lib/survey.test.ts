import type { Answers, SurveyQuestion } from "../api/types";
import { surveyForm as form } from "../test/surveyFixture";
import {
  answerText,
  cleanAnswers,
  incompleteSections,
  isConsented,
  missingRequired,
  otherKey,
  sectionErrors,
  todayJakarta,
  validateAnswer,
  visibleSections,
} from "./survey";

const q = (id: string): SurveyQuestion => form.sections.flatMap((s) => s.questions).find((x) => x.id === id)!;
const today = "2026-11-02";
const base: Answers = { a_date: "2026-11-02", a_rw: "05" };

describe("visibleSections / isConsented", () => {
  it("menolak → hanya bagian A", () => {
    expect(visibleSections(form, { ...base, a_consent: "no" }).map((s) => s.id)).toEqual(["A"]);
    expect(isConsented({ a_consent: "no" })).toBe(false);
  });
  it("bersedia atau belum dijawab → semua bagian", () => {
    expect(visibleSections(form, { ...base, a_consent: "yes" })).toHaveLength(4);
    expect(visibleSections(form, {})).toHaveLength(4);
    expect(isConsented({ a_consent: "yes" })).toBe(true);
  });
});

describe("missingRequired", () => {
  it("menolak hanya butuh bagian A", () => {
    expect(missingRequired(form, { ...base, a_consent: "no" })).toEqual([]);
  });
  it("bersedia dengan b7 kosong → b7", () => {
    expect(missingRequired(form, { ...base, a_consent: "yes", b1: "yes" })).toEqual(["b7"]);
  });
  it("A kosong → semua wajib A dulu (urut definisi)", () => {
    expect(missingRequired(form, {}).slice(0, 3)).toEqual(["a_date", "a_rw", "a_consent"]);
  });
});

describe("validateAnswer (aturan §4.1, sama dengan backend)", () => {
  const cases: [string, unknown, string | null, Answers?][] = [
    ["a_date", "2026-11-02", null],
    ["a_date", "2026-11-03", "Tanggal tidak boleh di masa depan."],
    ["a_date", "2026-02-30", "Tanggal tidak valid."],
    ["a_date", "02-11-2026", "Tanggal tidak valid."],
    ["a_date", "", "Wajib diisi."],
    ["a_rw", "  05 ", null],
    ["a_rw", "12345678901", "Maksimal 10 karakter."],
    ["a_rw", "0\u00075", "Mengandung karakter yang tidak diizinkan."],
    ["a_rw", "0\n5", "Tidak boleh berisi baris baru."],
    ["a_consent", "maybe", "Pilihan tidak valid."],
    ["b1", "yes", null],
    ["b_note_1", "", null],
    ["b_note_1", "B3 – belum bisa\nmembedakan", null],
    ["b_note_1", "x".repeat(1001), "Maksimal 1000 karakter."],
    ["r2_occupants", 0, "Minimal 1."],
    ["r2_occupants", 51, "Maksimal 50."],
    ["r2_occupants", 2.5, "Isi dengan bilangan bulat."],
    ["r2_occupants", 4, null],
    ["r3_container_types", ["lidded", "bucket"], null],
    ["r3_container_types", ["lidded", "lidded"], "Pilihan ganda tidak boleh berulang."],
    ["r3_container_types", ["box"], "Pilihan tidak valid."],
    ["r3_container_types", ["other"], "Tuliskan jenis lainnya."],
    ["r3_container_types", ["other"], null, { r3_container_types_other: "Galon bekas" }],
    ["r3_container_types", ["other"], "Keterangan lainnya maksimal 100 karakter.", { r3_container_types_other: "x".repeat(101) }],
  ];
  it.each(cases)("%s = %j → %s", (id, v, want, answers) => {
    expect(validateAnswer(q(id), v as never, answers ?? {}, today)).toBe(want);
  });
});

it("sectionErrors dan incompleteSections memakai aturan yang sama", () => {
  const a: Answers = { ...base, a_consent: "yes", b1: "yes", r2_occupants: 99 };
  expect(sectionErrors(form.sections[1]!, a, today)).toEqual({ b7: "Wajib diisi." });
  expect(incompleteSections(form, a, today).map((s) => s.id)).toEqual(["B1", "R2"]);
  expect(incompleteSections(form, { ...base, a_consent: "no", b1: "x" }, today)).toEqual([]);
});

describe("cleanAnswers", () => {
  it("menolak → jawaban bagian lain dibuang (§4.3)", () => {
    expect(cleanAnswers(form, { ...base, a_consent: "no", b1: "yes", r2_occupants: 3 })).toEqual({ ...base, a_consent: "no" });
  });
  it("kosong dibuang, teks di-trim, teks lainnya hanya bila opsi lainnya dipilih", () => {
    const a: Answers = { ...base, a_rw: " 05 ", a_consent: "yes", b_note_1: "  ", r3_container_types: ["bucket"], [otherKey("r3_container_types")]: "x" };
    expect(cleanAnswers(form, a)).toEqual({ ...base, a_consent: "yes", r3_container_types: ["bucket"] });
    const b: Answers = { ...a, r3_container_types: ["other"], r3_container_types_other: " Galon " };
    expect(cleanAnswers(form, b)).toMatchObject({ r3_container_types: ["other"], r3_container_types_other: "Galon" });
  });
});

it("answerText menampilkan teks opsi, bukan id", () => {
  expect(answerText(q("b1"), "no")).toBe("Tidak");
  expect(answerText(q("r3_container_types"), ["lidded", "other"], { r3_container_types_other: "Galon" })).toBe("Tempat sampah bertutup; Lainnya: Galon");
  expect(answerText(q("r2_occupants"), 4)).toBe("4");
  expect(answerText(q("b_note_1"), undefined)).toBe("");
});

it("todayJakarta memakai zona Asia/Jakarta (UTC+7)", () => {
  expect(todayJakarta(new Date("2026-11-01T17:30:00Z"))).toBe("2026-11-02");
  expect(todayJakarta(new Date("2026-11-01T16:30:00Z"))).toBe("2026-11-01");
});
