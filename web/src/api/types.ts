// Bentuk request/response API (lihat docs/spec.md §5 dan docs/spec-labels-admin.md §5). Nama field
// mengikuti JSON API apa adanya.

/** Label aktif dari GET /labels (urut sort_order lalu nama). */
export interface Label {
  id: number;
  name: string;
  description?: string | null;
  color: string;
}

/** Label lengkap di dalam anotasi (GET /submissions/{id}); label arsip tetap tampil benar. */
export interface LabelRef {
  id: number;
  name: string;
  color: string;
  archived: boolean;
}

/** Kotak yang dikirim (POST/PUT /submissions): label wajib, teks bebas tidak diterima lagi. */
export interface AnnotationDTO {
  label_id: number;
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Kotak yang dibaca. Baris lama (sebelum label): `label: null` dan `text` berisi catatan bebas. */
export interface AnnotationView {
  label: LabelRef | null;
  text: string | null;
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface SubmitPhotoDTO {
  photo_id: string;
  annotations: AnnotationDTO[];
}

export interface PresignResponse {
  photo_id: string;
  upload: { method: "PUT"; url: string; headers: Record<string, string>; expires_at: string };
}

export interface SubmitResponse {
  submission_id: string;
  version: number;
  created_at: string;
  /** Hanya pada auth.mode = none. */
  edit_token?: string;
}

export interface EditResponse {
  submission_id: string;
  version: number;
  updated_at: string;
}

export interface PhotoView {
  photo_id: string;
  position: number;
  content_type: string;
  size_bytes: number;
  width: number;
  height: number;
  annotations: AnnotationView[];
}

export interface SubmissionView {
  id: string;
  version: number;
  created_at: string;
  updated_at: string;
  editable_until: string;
  editable: boolean;
  photos: PhotoView[];
  /** Status review admin (spec-labels-admin §5.7); tanpa reviewed_at untuk user. */
  review?: { status: "pending" | "accepted" | "rejected"; reason: string | null };
}

export interface PhotoURL {
  url: string;
  expires_at: string;
}

export interface OTPTicket {
  challenge_id: string;
  status: "queued";
  resend_after_seconds: number;
  estimated_wait_seconds: number;
}

export type OTPStatus = "queued" | "sent" | "failed" | "expired" | "consumed";

export interface OTPStatusView {
  status: OTPStatus;
  failure_reason: "WA_UNAVAILABLE" | "SEND_FAILED" | null;
  resend_after_seconds: number;
  expires_at: string | null;
}

export interface User {
  id: string;
  name: string;
  email: string;
  phone: string; // disamarkan oleh API
  role: "user";
  /** NIM + kelompok mahasiswa (spec-pendataan §5.4); null pada akun lama / mode none. */
  nim?: string | null;
  group?: GroupRef | null;
}

/** Kelompok mahasiswa (GET /groups, spec-pendataan §5.2). */
export interface GroupRef {
  id: number;
  name: string;
}

export interface LoginResponse {
  access_token: string;
  expires_at: string;
  user: User;
}
