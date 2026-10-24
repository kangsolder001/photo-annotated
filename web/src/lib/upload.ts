import { ApiError, parseRetryAfter } from "../api/client";

// Upload byte foto ke URL bertanda tangan (spec §5.1b) dengan progres dan retry.

export interface PutResult {
  status: number; // 0 = gagal jaringan
  code?: string;
  retryAfter?: number;
}

export type ProgressFn = (fraction: number) => void;

/** PUT lewat XHR karena fetch belum menyediakan progres upload di semua browser. */
export function putWithProgress(url: string, body: Blob, contentType: string, onProgress?: ProgressFn, signal?: AbortSignal): Promise<PutResult> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PUT", url);
    xhr.setRequestHeader("Content-Type", contentType);
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(e.loaded / e.total);
    };
    xhr.onload = () => {
      let code: string | undefined;
      try {
        code = (JSON.parse(xhr.responseText) as { error?: { code?: string } }).error?.code;
      } catch {
        // 204 tanpa badan
      }
      resolve({ status: xhr.status, code, retryAfter: parseRetryAfter(xhr.getResponseHeader("Retry-After")) });
    };
    xhr.onerror = () => resolve({ status: 0 });
    xhr.ontimeout = () => resolve({ status: 0 });
    xhr.onabort = () => reject(new DOMException("aborted", "AbortError"));
    signal?.addEventListener("abort", () => xhr.abort(), { once: true });
    xhr.send(body);
  });
}

/** Apakah hasil PUT layak diulang (spec §4.2): gangguan jaringan dan kondisi sementara. */
export function isRetryable(r: PutResult): boolean {
  if (r.status === 0 || r.status === 408) return true;
  if (r.status === 503) return r.code === "UPLOAD_BUSY";
  if (r.status === 409) return r.code === "UPLOAD_IN_PROGRESS";
  if (r.status === 422) return r.code === "SIZE_MISMATCH";
  return false;
}

/** Jeda sebelum percobaan ke-(attempt+1): Retry-After bila ada, selain itu 1 s, 2 s, 4 s (+ jitter). */
export function backoffMs(attempt: number, retryAfter?: number, random: () => number = Math.random): number {
  if (retryAfter !== undefined) return retryAfter * 1000;
  return 1000 * 2 ** attempt + Math.floor(random() * 300);
}

const MESSAGES: Record<string, string> = {
  UPLOAD_BUSY: "Server sedang sibuk. Coba lagi sebentar.",
  STORAGE_FULL: "Penyimpanan server penuh. Coba lagi nanti.",
  STORAGE_UNAVAILABLE: "Penyimpanan server sedang bermasalah. Coba lagi nanti.",
  INVALID_IMAGE: "File foto rusak atau tidak didukung.",
  PAYLOAD_TOO_LARGE: "Foto terlalu besar.",
  URL_EXPIRED: "Waktu upload habis.",
};

export function uploadErrorMessage(r: PutResult): string {
  if (r.status === 0) return "Koneksi terputus saat mengunggah.";
  return (r.code && MESSAGES[r.code]) ?? `Upload gagal (HTTP ${r.status}).`;
}

export const MAX_RETRIES = 3;

const sleep = (ms: number, signal?: AbortSignal) =>
  new Promise<void>((resolve, reject) => {
    const t = setTimeout(resolve, ms);
    signal?.addEventListener("abort", () => {
      clearTimeout(t);
      reject(new DOMException("aborted", "AbortError"));
    }, { once: true });
  });

/**
 * Mengunggah dengan retry. URL kedaluwarsa (410) dilempar sebagai ApiError URL_EXPIRED agar pemanggil
 * meminta presign baru. `put` dapat diganti di test.
 */
export async function uploadWithRetry(
  url: string,
  body: Blob,
  contentType: string,
  onProgress?: ProgressFn,
  signal?: AbortSignal,
  put: typeof putWithProgress = putWithProgress,
  wait: typeof sleep = sleep,
): Promise<void> {
  for (let attempt = 0; ; attempt++) {
    const r = await put(url, body, contentType, onProgress, signal);
    if (r.status === 204) return;
    if (attempt < MAX_RETRIES && isRetryable(r)) {
      onProgress?.(0);
      await wait(backoffMs(attempt, r.retryAfter), signal);
      continue;
    }
    throw new ApiError(r.status, r.code ?? (r.status === 0 ? "NETWORK_ERROR" : `HTTP_${r.status}`), uploadErrorMessage(r), r.retryAfter);
  }
}
