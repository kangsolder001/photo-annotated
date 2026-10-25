import { limits } from "../config";

// Pemrosesan foto di browser sebelum upload (spec §6.2): orientasi EXIF diterapkan, sisi terpanjang
// ≤ 1920 px, encode ulang ke WebP (fallback JPEG). Encode ulang lewat canvas membuang seluruh
// metadata EXIF, termasuk lokasi GPS.

export interface ProcessedPhoto {
  blob: Blob;
  contentType: "image/webp" | "image/jpeg";
  width: number;
  height: number;
}

/** Ukuran hasil resize: sisi terpanjang ≤ maxSide, rasio dipertahankan, tidak pernah diperbesar. */
export function fitWithin(width: number, height: number, maxSide: number): { width: number; height: number } {
  const scale = Math.min(1, maxSide / Math.max(width, height));
  return { width: Math.max(1, Math.round(width * scale)), height: Math.max(1, Math.round(height * scale)) };
}

/** Urutan kualitas yang dicoba; bila semua masih > batas, dimensi dikecilkan lalu diulang. */
export const QUALITY_STEPS = [0.85, 0.75, 0.65, 0.55] as const;

export class ImageProcessingError extends Error {}

export interface Encoder {
  /** Menggambar ulang ke ukuran tertentu dan encode; mengembalikan null bila tipe tidak didukung. */
  encode(width: number, height: number, type: string, quality: number): Promise<Blob | null>;
}

/**
 * Mencari hasil terbesar (kualitas/dimensi) yang ≤ maxBytes. Dipisahkan dari canvas agar bisa diuji.
 */
export async function compress(
  enc: Encoder,
  srcWidth: number,
  srcHeight: number,
  opts: { maxSide?: number; maxBytes?: number } = {},
): Promise<ProcessedPhoto> {
  const maxBytes = opts.maxBytes ?? limits.maxUploadBytes;
  let { width, height } = fitWithin(srcWidth, srcHeight, opts.maxSide ?? limits.maxSide);
  let type: ProcessedPhoto["contentType"] = "image/webp";
  for (let round = 0; round < 6; round++) {
    for (const q of QUALITY_STEPS) {
      let blob = await enc.encode(width, height, type, q);
      if (blob == null && type === "image/webp") {
        type = "image/jpeg"; // browser tanpa encoder WebP (Safari lama)
        blob = await enc.encode(width, height, type, q);
      }
      if (blob == null) throw new ImageProcessingError("Browser tidak bisa mengolah foto ini.");
      if (blob.size <= maxBytes) return { blob, contentType: type, width, height };
    }
    ({ width, height } = fitWithin(width, height, Math.floor(Math.max(width, height) * 0.8)));
  }
  throw new ImageProcessingError("Foto terlalu besar dan tidak bisa diperkecil di bawah 2 MB.");
}

/** Encoder berbasis canvas untuk satu bitmap sumber. */
function canvasEncoder(bitmap: ImageBitmap): Encoder {
  return {
    async encode(width, height, type, quality) {
      const canvas = document.createElement("canvas");
      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext("2d");
      if (!ctx) return null;
      ctx.imageSmoothingQuality = "high";
      ctx.drawImage(bitmap, 0, 0, width, height);
      const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, type, quality));
      // browser yang tidak mendukung tipe itu diam-diam mengembalikan PNG
      return blob && blob.type === type ? blob : null;
    },
  };
}

/** Memproses file dari input kamera/galeri menjadi foto siap upload. */
export async function processPhoto(file: File): Promise<ProcessedPhoto> {
  if (!file.type.startsWith("image/")) throw new ImageProcessingError("File ini bukan gambar.");
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file, { imageOrientation: "from-image" });
  } catch {
    throw new ImageProcessingError("Format foto tidak didukung browser ini (coba JPG atau ambil ulang foto).");
  }
  try {
    return await compress(canvasEncoder(bitmap), bitmap.width, bitmap.height);
  } finally {
    bitmap.close();
  }
}
