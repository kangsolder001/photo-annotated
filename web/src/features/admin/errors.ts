import { ApiError } from "../../api/client";
import { errorMessage } from "../../lib/errors";

// Pesan error khusus dashboard; kode lain memakai pesan umum aplikasi.
const MESSAGES: Record<string, string> = {
  UNAUTHORIZED: "Sesi admin berakhir. Silakan masuk lagi.",
  NOT_FOUND: "Data tidak ditemukan atau sudah dihapus.",
  VERSION_CONFLICT: "Submission ini baru saja diubah annotator. Muat ulang untuk melihat isi terbaru sebelum memutuskan.",
  INVALID_QUERY: "Filter tidak valid.",
  LABEL_NAME_TAKEN: "Nama label sudah dipakai (termasuk label yang diarsipkan). Buka arsip label itu bila ingin memakainya lagi.",
  LABEL_LIMIT: "Batas jumlah label aktif sudah tercapai. Arsipkan label yang tidak dipakai dulu.",
  LABEL_IN_USE: "Label ini sudah dipakai anotasi sehingga tidak bisa dihapus. Arsipkan saja.",
  LABEL_ORDER_STALE: "Daftar label sudah berubah di tempat lain. Daftar dimuat ulang; ulangi pengurutan.",
  INVALID_LABEL_NAME: "Nama label 1–50 karakter, tanpa karakter khusus.",
  INVALID_LABEL_COLOR: "Warna harus berbentuk #rrggbb.",
  INVALID_LABEL_DESCRIPTION: "Deskripsi maksimal 200 karakter.",
  INVALID_REASON: "Alasan wajib diisi (maksimal 500 karakter).",
  INVALID_REVIEW: "Status review tidak valid.",
  GROUP_NAME_TAKEN: "Nama kelompok sudah dipakai (termasuk kelompok yang diarsipkan).",
  GROUP_IN_USE: "Kelompok ini sudah dipakai akun mahasiswa sehingga tidak bisa dihapus. Arsipkan saja.",
  INVALID_GROUP_NAME: "Nama kelompok 1–100 karakter, tanpa karakter khusus.",
  INVALID_GROUP: "Kelompok tidak tersedia (mungkin sudah diarsipkan). Pilih kelompok lain.",
  ADMIN_READ_ONLY: "Admin tidak bisa mengubah anotasi user.",
};

export function adminErrorMessage(e: unknown): string {
  if (e instanceof ApiError && MESSAGES[e.code]) return MESSAGES[e.code]!;
  return errorMessage(e);
}
