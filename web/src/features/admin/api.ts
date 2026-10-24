// Klien API admin (spec-labels-admin §5). Token admin dari sessionStorage; 401 → token dibuang dan
// dashboard kembali ke /admin/masuk.
import { ApiError, apiURL, parseRetryAfter, request } from "../../api/client";
import type { OTPStatusView } from "../../api/types";
import { adminSession } from "./session";
import type {
  AdminLabel,
  AdminMe,
  AdminOTPTicket,
  AdminSubmission,
  AdminUser,
  AdminVerifyResponse,
  BulkDeleteResult,
  BulkReviewResult,
  DeleteDataResult,
  DeleteResult,
  LabelInput,
  LabelReplace,
  LabelStats,
  Page,
  ReviewInput,
  ReviewResponse,
  ReviewStatus,
  SubmissionListItem,
  SubmissionQuery,
  SystemStatus,
  UserQuery,
} from "./types";

const enc = encodeURIComponent;
const BASE = "/api/v1/admin";

type Opts = { body?: unknown; signal?: AbortSignal };

function adm<T>(method: string, path: string, opts: Opts = {}): Promise<T> {
  return request<T>(method, BASE + path, { ...opts, token: adminSession.token(), onUnauthorized: adminSession.expire });
}

/** Tanpa token (alur login). */
function pub<T>(method: string, path: string, body?: unknown): Promise<T> {
  return request<T>(method, BASE + path, { body, token: null });
}

/** Query string dari objek; nilai kosong/undefined dilewati. */
export function qs(q: Record<string, string | number | boolean | undefined | null>): string {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(q)) if (v !== undefined && v !== null && v !== "") p.set(k, String(v));
  const s = p.toString();
  return s ? `?${s}` : "";
}

export const adminApi = {
  // ---- login OTP (§5.2)
  requestOTP: (body: { phone: string; turnstile_token?: string }) => pub<AdminOTPTicket>("POST", "/otp/request", body),
  otpStatus: (id: string) => pub<OTPStatusView>("GET", `/otp/${enc(id)}`),
  resendOTP: (body: { challenge_id: string; turnstile_token?: string }) => pub<AdminOTPTicket>("POST", "/otp/resend", body),
  verifyOTP: (body: { challenge_id: string; code: string }) => pub<AdminVerifyResponse>("POST", "/otp/verify", body),
  channelStatus: () => pub<{ otp_available: boolean }>("GET", "/channel-status"),
  me: () => adm<AdminMe>("GET", "/me"),

  // ---- label (§5.3)
  labels: () => adm<{ labels: AdminLabel[] }>("GET", "/labels").then((r) => r.labels ?? []),
  createLabel: (body: LabelInput) => adm<AdminLabel>("POST", "/labels", { body }),
  updateLabel: (id: number, body: LabelReplace) => adm<AdminLabel>("PUT", `/labels/${id}`, { body }),
  reorderLabels: (ids: number[]) => adm<unknown>("PUT", "/labels/order", { body: { ids } }),
  deleteLabel: (id: number) => adm<void>("DELETE", `/labels/${id}`),

  // ---- submission (§5.4)
  submissions: (q: SubmissionQuery, signal?: AbortSignal) =>
    adm<Page<SubmissionListItem>>("GET", `/submissions${qs({ ...q })}`, { signal }),
  submission: (id: string) => adm<AdminSubmission>("GET", `/submissions/${enc(id)}`),

  // ---- hapus (§5.5)
  deleteSubmission: (id: string) => adm<DeleteResult>("DELETE", `/submissions/${enc(id)}`),
  deletePhoto: (id: string) => adm<DeleteResult>("DELETE", `/photos/${enc(id)}`),
  deleteSubmissions: (ids: string[]) => adm<BulkDeleteResult>("POST", "/submissions/delete", { body: { ids } }),

  // ---- review (§5.7)
  review: (id: string, body: ReviewInput) => adm<ReviewResponse>("PUT", `/submissions/${enc(id)}/review`, { body }),
  reviewMany: (items: { id: string; version: number }[], status: ReviewStatus, reason: string | null) =>
    adm<BulkReviewResult>("POST", "/submissions/review", { body: { items, status, reason } }),

  // ---- annotator (§5.8)
  users: (q: UserQuery) => adm<Page<AdminUser>>("GET", `/users${qs({ ...q })}`),
  user: (id: string) => adm<AdminUser>("GET", `/users/${enc(id)}`),
  block: (id: string, reason: string) => adm<unknown>("POST", `/users/${enc(id)}/block`, { body: { reason } }),
  unblock: (id: string) => adm<unknown>("POST", `/users/${enc(id)}/unblock`),
  deleteUserData: (id: string, includeAccepted: boolean) =>
    adm<DeleteDataResult>("POST", `/users/${enc(id)}/delete-data`, { body: { include_accepted: includeAccepted } }),

  // ---- statistik & status (§5.9, §5.10)
  labelStats: (q: { from?: string; to?: string; review_status?: ReviewStatus }) => adm<LabelStats>("GET", `/stats/labels${qs(q)}`),
  system: () => adm<SystemStatus>("GET", "/system"),
};

export interface ExportResult {
  blob: Blob;
  rows: number;
  /** false = export terpotong (baris terakhir rusak, atau server melaporkan tidak lengkap). */
  complete: boolean;
}

/**
 * GET /admin/export (JSONL) lewat fetch dengan header Authorization, lalu dicek kelengkapannya.
 * Trailer X-Export-Complete tidak bisa dibaca fetch; dipakai header bila ada, dan baris terakhir
 * harus JSON utuh diakhiri baris baru.
 */
export async function exportJSONL(q: { from?: string; to?: string; review_status?: ReviewStatus; label_id?: number }): Promise<ExportResult> {
  const token = adminSession.token();
  let res: Response;
  try {
    res = await fetch(apiURL(`${BASE}/export${qs({ format: "jsonl", ...q })}`), {
      headers: { Accept: "application/x-ndjson", ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    });
  } catch {
    throw new ApiError(0, "NETWORK_ERROR", "Tidak bisa terhubung ke server.");
  }
  if (!res.ok) {
    let err: { code?: string; message?: string } | undefined;
    try {
      err = ((await res.json()) as { error?: { code?: string; message?: string } }).error;
    } catch {
      // bukan JSON
    }
    if (res.status === 401) adminSession.expire();
    throw new ApiError(res.status, err?.code ?? `HTTP_${res.status}`, err?.message ?? res.statusText, parseRetryAfter(res.headers.get("Retry-After")));
  }
  const text = await res.text();
  return { blob: new Blob([text], { type: "application/x-ndjson" }), ...checkJSONL(text, res.headers.get("X-Export-Complete")) };
}

export function checkJSONL(text: string, completeHeader: string | null): { rows: number; complete: boolean } {
  const lines = text.split("\n");
  const endsWithNewline = text === "" || text.endsWith("\n");
  const body = lines.filter((l) => l !== "");
  let lastOk = true;
  if (body.length) {
    try {
      JSON.parse(body[body.length - 1]!);
    } catch {
      lastOk = false;
    }
  }
  return { rows: body.length, complete: endsWithNewline && lastOk && completeHeader !== "false" };
}

/** Simpan Blob sebagai berkas unduhan. */
export function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
