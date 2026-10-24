import { config } from "../config";
import { session } from "../lib/session";

/** Error dari API (format `{error:{code,message,details}}`) atau dari jaringan (status 0). */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  /** Detik menunggu dari header Retry-After, bila ada. */
  readonly retryAfter?: number;
  readonly details?: Record<string, unknown>;

  constructor(status: number, code: string, message: string, retryAfter?: number, details?: Record<string, unknown>) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.retryAfter = retryAfter;
    this.details = details;
  }
}

export function parseRetryAfter(v: string | null): number | undefined {
  if (!v) return undefined;
  const n = Number(v);
  return Number.isFinite(n) && n >= 0 ? n : undefined;
}

export function apiURL(path: string): string {
  return config.apiBase + path;
}

interface RequestOptions {
  body?: unknown;
  headers?: Record<string, string>;
  /** Kirim token sesi (mode otp) bila ada. Default true. */
  auth?: boolean;
  signal?: AbortSignal;
}

/** Memanggil API JSON. Respons 401 pada request bertoken menghapus sesi (FE kembali ke login). */
export async function request<T>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json", ...opts.headers };
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  const token = opts.auth === false ? null : session.token();
  if (token) headers.Authorization = `Bearer ${token}`;

  let res: Response;
  try {
    res = await fetch(apiURL(path), {
      method,
      headers,
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
      signal: opts.signal,
    });
  } catch (e) {
    if (e instanceof DOMException && e.name === "AbortError") throw e;
    throw new ApiError(0, "NETWORK_ERROR", "Tidak bisa terhubung ke server. Periksa koneksi internet Anda.");
  }

  if (res.status === 204) return undefined as T;
  let data: unknown = null;
  try {
    data = await res.json();
  } catch {
    // badan kosong / bukan JSON
  }
  if (res.ok) return data as T;

  const err = (data as { error?: { code?: string; message?: string; details?: Record<string, unknown> } } | null)?.error;
  const apiErr = new ApiError(
    res.status,
    err?.code ?? `HTTP_${res.status}`,
    err?.message ?? res.statusText,
    parseRetryAfter(res.headers.get("Retry-After")),
    err?.details,
  );
  if (res.status === 401 && token) session.expire();
  throw apiErr;
}
