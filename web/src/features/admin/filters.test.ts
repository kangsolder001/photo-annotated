import { dayAfter, dayStart, daysUntil, exportFilename, filtersToParams, parseFilters, toQuery } from "./filters";

it("parse/serialize filter URL; nilai tidak valid diabaikan", () => {
  const f = parseFilters(new URLSearchParams("from=2026-10-01&to=bad&label_id=3&review_status=pending&sort=oldest&unlabeled=true&q=%20budi%20&user_id=u1&x=1"));
  expect(f).toEqual({ from: "2026-10-01", label_id: 3, review_status: "pending", sort: "oldest", unlabeled: true, q: "budi", user_id: "u1" });
  expect(parseFilters(new URLSearchParams("label_id=abc&review_status=semua")).label_id).toBeUndefined();
  const p = filtersToParams({ ...f, sort: "newest", unlabeled: false });
  p.sort();
  expect(p.toString()).toBe("from=2026-10-01&label_id=3&q=budi&review_status=pending&user_id=u1");
});

it("tanggal lokal → RFC 3339; to inklusif dikirim sebagai awal hari berikutnya", () => {
  expect(new Date(dayStart("2026-10-01")).getDate()).toBe(1);
  expect(new Date(dayAfter("2026-10-31")).getDate()).toBe(1);
  expect(new Date(dayAfter("2026-10-31")).getMonth()).toBe(10);
  const q = toQuery({ from: "2026-10-01", to: "2026-10-02", unlabeled: false });
  expect(q.from).toBe(dayStart("2026-10-01"));
  expect(q.to).toBe(dayStart("2026-10-03"));
  expect(q.unlabeled).toBeUndefined();
});

it("nama berkas export dan sisa hari", () => {
  expect(exportFilename({ from: "2026-10-01", to: "2026-10-31" })).toBe("photo-annotated-2026-10-01-2026-10-31.jsonl");
  expect(exportFilename({})).toBe("photo-annotated-awal-sekarang.jsonl");
  const now = Date.parse("2026-10-24T00:00:00Z");
  expect(daysUntil("2026-10-26T12:00:00Z", now)).toBe(3);
  expect(daysUntil("2026-10-01T00:00:00Z", now)).toBe(0);
});
