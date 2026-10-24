import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../../api/endpoints";
import { limits } from "../../config";
import { errorMessage, isCode } from "../../lib/errors";
import { processPhoto } from "../../lib/image";
import { uploadWithRetry } from "../../lib/upload";

// Mengelola foto yang dipilih pengguna: proses di browser → presign → upload (dengan progres).

export type PhotoStatus = "processing" | "uploading" | "uploaded" | "error";

export interface PhotoItem {
  /** Kunci lokal yang stabil (bukan photo_id; foto baru belum punya id saat diproses). */
  key: string;
  status: PhotoStatus;
  progress: number;
  error?: string;
  previewUrl?: string;
  width?: number;
  height?: number;
  photoId?: string;
}

/** Penyedia token Turnstile untuk presign (mode none); undefined bila tidak dipakai. */
export type TokenProvider = { token(): Promise<string | undefined>; reset(): void };

const MAX_PARALLEL = 2; // HP: dua upload bersamaan sudah memenuhi jaringan seluler

export function usePhotoUploads(turnstile?: TokenProvider) {
  const [items, setItems] = useState<PhotoItem[]>([]);
  const files = useRef(new Map<string, File>());
  const urls = useRef(new Set<string>());
  const active = useRef(0);
  const queue = useRef<string[]>([]);
  const alive = useRef(true);

  const patch = useCallback((key: string, p: Partial<PhotoItem>) => {
    if (!alive.current) return;
    setItems((xs) => xs.map((x) => (x.key === key ? { ...x, ...p } : x)));
  }, []);

  const presign = useCallback(async (meta: { content_type: string; size_bytes: number; width: number; height: number }) => {
    for (let attempt = 0; ; attempt++) {
      try {
        return await api.presign(meta, await turnstile?.token());
      } catch (e) {
        // token Turnstile basi/terpakai habis: minta yang baru sekali
        if (attempt === 0 && isCode(e, "TURNSTILE_FAILED") && turnstile) {
          turnstile.reset();
          continue;
        }
        throw e;
      }
    }
  }, [turnstile]);

  const run = useCallback(async (key: string) => {
    const file = files.current.get(key);
    if (!file) return;
    try {
      patch(key, { status: "processing", progress: 0, error: undefined });
      const photo = await processPhoto(file);
      const previewUrl = URL.createObjectURL(photo.blob);
      urls.current.add(previewUrl);
      patch(key, { previewUrl, width: photo.width, height: photo.height, status: "uploading" });

      const meta = { content_type: photo.contentType, size_bytes: photo.blob.size, width: photo.width, height: photo.height };
      for (let attempt = 0; ; attempt++) {
        const p = await presign(meta);
        try {
          await uploadWithRetry(p.upload.url, photo.blob, photo.contentType, (f) => patch(key, { progress: f }));
          patch(key, { status: "uploaded", progress: 1, photoId: p.photo_id });
          files.current.delete(key); // file asli tidak lagi diperlukan
          return;
        } catch (e) {
          if (attempt === 0 && isCode(e, "URL_EXPIRED")) continue; // minta URL baru sekali
          throw e;
        }
      }
    } catch (e) {
      patch(key, { status: "error", error: errorMessage(e) });
    }
  }, [patch, presign]);

  const pump = useCallback(() => {
    while (active.current < MAX_PARALLEL && queue.current.length > 0) {
      const key = queue.current.shift()!;
      active.current++;
      void run(key).finally(() => {
        active.current--;
        pump();
      });
    }
  }, [run]);

  const add = useCallback((list: FileList | File[], capacity: number) => {
    const picked = Array.from(list).slice(0, Math.max(0, capacity));
    const fresh = picked.map((f) => {
      const key = crypto.randomUUID();
      files.current.set(key, f);
      return { key, status: "processing" as const, progress: 0 };
    });
    setItems((xs) => [...xs, ...fresh]);
    queue.current.push(...fresh.map((x) => x.key));
    pump();
    return picked.length;
  }, [pump]);

  const retry = useCallback((key: string) => {
    if (!files.current.has(key)) return;
    queue.current.push(key);
    patch(key, { status: "processing", progress: 0, error: undefined });
    pump();
  }, [patch, pump]);

  const remove = useCallback((key: string) => {
    files.current.delete(key);
    queue.current = queue.current.filter((k) => k !== key);
    setItems((xs) => {
      const gone = xs.find((x) => x.key === key);
      if (gone?.previewUrl) {
        URL.revokeObjectURL(gone.previewUrl);
        urls.current.delete(gone.previewUrl);
      }
      return xs.filter((x) => x.key !== key);
    });
  }, []);

  useEffect(() => {
    alive.current = true;
    const owned = urls.current;
    return () => {
      alive.current = false;
      owned.forEach((u) => URL.revokeObjectURL(u));
    };
  }, []);

  return { items, add, retry, remove, maxPhotos: limits.maxPhotos };
}
