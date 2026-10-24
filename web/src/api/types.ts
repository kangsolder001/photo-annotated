// Bentuk request/response API (lihat docs/spec.md §5). Nama field mengikuti JSON API apa adanya.

export interface AnnotationDTO {
  text: string;
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
  annotations: AnnotationDTO[];
}

export interface SubmissionView {
  id: string;
  version: number;
  created_at: string;
  updated_at: string;
  editable_until: string;
  editable: boolean;
  photos: PhotoView[];
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
}

export interface LoginResponse {
  access_token: string;
  expires_at: string;
  user: User;
}
