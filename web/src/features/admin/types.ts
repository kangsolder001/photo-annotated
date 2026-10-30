// Kontrak API admin (docs/spec-labels-admin.md §5). Nama field mengikuti JSON API apa adanya.
import type { AnnotationView, OTPTicket, PhotoView } from "../../api/types";

export type AuthMode = "none" | "otp";

export interface AdminInfo {
  name: string;
  /** Tersamar, mis. "62812****7890". */
  phone: string;
}

export interface AdminVerifyResponse {
  access_token: string;
  expires_at: string;
  admin: AdminInfo;
}

export interface AdminMe extends AdminInfo {
  expires_at: string;
  auth_mode: AuthMode;
}

export type AdminOTPTicket = OTPTicket;

// ---- label (§5.3)
export interface AdminLabel {
  id: number;
  name: string;
  description: string | null;
  color: string;
  sort_order: number;
  archived: boolean;
  usage_count: number;
  created_at: string;
  updated_at: string;
}

export interface LabelInput {
  name: string;
  description?: string | null;
  color?: string;
}

export interface LabelReplace {
  name: string;
  description: string | null;
  color: string;
  archived: boolean;
}

// ---- kelompok (spec-pendataan §5.5)
export interface AdminGroup {
  id: number;
  name: string;
  archived: boolean;
  /** Jumlah mahasiswa di kelompok ini. */
  user_count: number;
  created_at: string;
  updated_at: string;
}

// ---- submission (§5.4, §5.7)
export type ReviewStatus = "pending" | "accepted" | "rejected";

export interface Review {
  status: ReviewStatus;
  reason: string | null;
  reviewed_at: string | null;
}

export interface Annotator {
  id: string;
  name: string;
  email: string;
  /** Nomor utuh (admin perlu bisa menghubungi). */
  phone: string;
  blocked: boolean;
}

export interface CoverAnnotation {
  label_id: number | null;
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface SubmissionListItem {
  id: string;
  created_at: string;
  updated_at: string;
  version: number;
  review: Review;
  retention_deletes_at: string | null;
  annotator: Annotator | null;
  photo_count: number;
  annotation_count: number;
  labels: { id: number; name: string; color: string; count: number }[];
  legacy_text_count: number;
  cover: { photo_id: string; url: string; width: number; height: number; annotations: CoverAnnotation[] } | null;
}

export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}

export interface SubmissionQuery {
  from?: string;
  to?: string;
  label_id?: number;
  user_id?: string;
  q?: string;
  unlabeled?: boolean;
  review_status?: ReviewStatus;
  sort?: "newest" | "oldest";
  limit?: number;
  cursor?: string;
}

export interface AdminPhoto extends PhotoView {
  url: string;
  sha256: string;
  annotations: AnnotationView[];
}

export interface AdminSubmission {
  id: string;
  version: number;
  created_at: string;
  updated_at: string;
  editable_until?: string;
  editable?: boolean;
  photos: AdminPhoto[];
  annotator: Annotator | null;
  review: Review;
  retention_deletes_at: string | null;
}

export interface ReviewInput {
  status: ReviewStatus;
  reason: string | null;
  version: number;
}

export interface ReviewResponse {
  review: Review;
  retention_deletes_at: string | null;
}

export interface BulkReviewResult {
  results: { id: string; result: "ok" | "version_conflict" | "not_found" }[];
}

// ---- hapus (§5.5)
export interface DeleteResult {
  status: "deleted" | "cleanup_pending";
  photos_deleted?: number;
  annotations_deleted?: number;
  submission_deleted?: boolean;
}

export interface BulkDeleteResult {
  results: { id: string; status: "deleted" | "cleanup_pending" | "not_found" }[];
}

// ---- annotator (§5.8)
export interface AnnotatorStats {
  submissions: number;
  annotations: number;
  accepted: number;
  rejected: number;
  pending: number;
  last_submission_at: string | null;
}

export interface AdminUser {
  id: string;
  name: string;
  email: string;
  phone: string;
  created_at: string;
  last_login_at: string | null;
  blocked: boolean;
  blocked_at: string | null;
  blocked_reason: string | null;
  /** NIM + kelompok (spec-pendataan §5.4); null pada akun lama. */
  nim?: string | null;
  group?: { id: number; name: string } | null;
  stats: AnnotatorStats;
}

export interface UserQuery {
  q?: string;
  status?: "active" | "blocked";
  limit?: number;
  cursor?: string;
}

export interface DeleteDataResult {
  submissions: number;
  photos: number;
  annotations: number;
  status: "cleanup_pending";
}

/** DELETE /admin/users/{id}: akun + semua datanya; 200 "deleted" bila tidak ada foto sama sekali. */
export interface DeleteAccountResult {
  account_deleted: true;
  submissions: number;
  photos: number;
  annotations: number;
  status: "deleted" | "cleanup_pending";
}

// ---- statistik (§5.9)
export interface LabelStat {
  id: number;
  name: string;
  color: string;
  archived: boolean;
  annotations: number;
  photos: number;
  submissions: number;
  share: number;
}

export interface LabelStats {
  totals: { submissions: number; photos: number; annotations: number; legacy_text_annotations: number };
  labels: LabelStat[];
}

// ---- status sistem (§5.10); tiap bagian bisa berisi { error } bila gagal dihitung
export type Part<T> = T | { error: string };

export interface SystemStatus {
  checked_at: string;
  api: Part<{ started_at: string; version: string; auth_mode: AuthMode }>;
  database: Part<{ ok: boolean; latency_ms: number }>;
  storage: Part<{ available: boolean; total_bytes: number; free_bytes: number; used_pct: number; min_free_bytes: number; photos_pending: number }>;
  uploads: Part<{ active: number; max: number }>;
  whatsapp: Part<{ otp_available: boolean; last_probe_at: string | null; queued: number; max_queue: number; sent_24h: number; failed_24h: number; daily_cap: number }>;
  cleanup: Part<{ last_run_at: string | null; last_ok: boolean; files_pending_delete: number }>;
  data: Part<{ submissions: number; review_pending: number; accepted: number; rejected: number; retention_due_7d: number; users_active: number; users_blocked: number }>;
  host: Part<{ cpu_temp_c: number | null }>;
}

export function partError<T extends object>(p: Part<T> | undefined): string | null {
  if (!p) return "tidak ada data";
  return "error" in p && typeof p.error === "string" ? p.error : null;
}
