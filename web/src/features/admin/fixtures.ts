import type { AdminSubmission, SubmissionListItem } from "./types";

export function listItem(i: number, p: Partial<SubmissionListItem> = {}): SubmissionListItem {
  return {
    id: `s${i}`, created_at: "2026-10-22T03:15:00Z", updated_at: "2026-10-22T03:15:00Z", version: 2,
    review: { status: "pending", reason: null, reviewed_at: null },
    retention_deletes_at: "2099-01-01T00:00:00Z",
    annotator: { id: `u${i}`, name: `Budi ${i}`, email: `budi${i}@example.com`, phone: "6281234567890", blocked: false },
    photo_count: 3, annotation_count: 7,
    labels: [{ id: 3, name: "Retak", color: "#e4572e", count: 5 }],
    legacy_text_count: 0,
    cover: { photo_id: `ph${i}`, url: `https://x.test/photos/ph${i}`, width: 1600, height: 1200, annotations: [{ label_id: 3, x: 0.1, y: 0.1, w: 0.2, h: 0.2 }] },
    ...p,
  };
}

export function detail(p: Partial<AdminSubmission> = {}): AdminSubmission {
  return {
    id: "s1", version: 2, created_at: "2026-10-22T03:15:00Z", updated_at: "2026-10-22T04:00:00Z",
    annotator: { id: "u1", name: "Budi Santoso", email: "budi@example.com", phone: "6281234567890", blocked: false },
    review: { status: "pending", reason: null, reviewed_at: null }, retention_deletes_at: "2099-01-01T00:00:00Z",
    photos: [
      { photo_id: "ph1", position: 0, content_type: "image/webp", size_bytes: 1, width: 1600, height: 1200, url: "https://x.test/ph1", sha256: "ab",
        annotations: [
          { label: { id: 3, name: "Retak", color: "#e4572e", archived: false }, text: null, x: 0.1, y: 0.1, w: 0.3, h: 0.25 },
          { label: null, text: "Kerusakan <b>kiri</b>", x: 0.5, y: 0.5, w: 0.2, h: 0.2 },
        ] },
      { photo_id: "ph2", position: 1, content_type: "image/webp", size_bytes: 1, width: 1200, height: 1600, url: "https://x.test/ph2", sha256: "cd",
        annotations: [{ label: { id: 9, name: "Lama", color: "#808080", archived: true }, text: null, x: 0.2, y: 0.2, w: 0.5, h: 0.5 }] },
    ],
    ...p,
  };
}
