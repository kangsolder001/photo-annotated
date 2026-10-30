import { ApiError } from "../api/client";

// Pesan untuk pengguna berdasarkan kode error API. Pesan teknis dari server tidak ditampilkan.
const MESSAGES: Record<string, string> = {
  NETWORK_ERROR: "Tidak bisa terhubung ke server. Periksa koneksi internet Anda.",
  RATE_LIMITED: "Terlalu banyak percobaan. Tunggu sebentar lalu coba lagi.",
  TURNSTILE_FAILED: "Verifikasi keamanan gagal. Coba lagi.",
  TURNSTILE_UNAVAILABLE: "Verifikasi keamanan sedang bermasalah. Coba lagi sebentar lagi.",
  STORAGE_FULL: "Penyimpanan server penuh. Coba lagi nanti.",
  STORAGE_UNAVAILABLE: "Penyimpanan server sedang bermasalah. Coba lagi nanti.",
  UPLOAD_BUSY: "Server sedang sibuk. Coba lagi sebentar.",
  PHOTO_NOT_FOUND: "Foto tidak ditemukan. Hapus lalu tambahkan lagi.",
  PHOTO_NOT_UPLOADED: "Ada foto yang belum selesai diunggah.",
  PHOTO_NOT_AVAILABLE: "Ada foto yang sudah tidak bisa dipakai. Hapus lalu tambahkan lagi.",
  PHOTO_FILE_INVALID: "File foto di server bermasalah. Hapus lalu tambahkan lagi.",
  VERSION_CONFLICT: "Data ini sudah diubah di tempat lain. Muat ulang untuk melihat versi terbaru.",
  EDIT_WINDOW_CLOSED: "Batas waktu edit (24 jam) sudah lewat.",
  UNAUTHORIZED: "Sesi Anda berakhir. Silakan masuk lagi.",
  NOT_FOUND: "Data tidak ditemukan atau Anda tidak punya akses.",
  OTP_UNAVAILABLE: "Pengiriman kode WhatsApp sedang bermasalah. Coba lagi beberapa saat lagi.",
  OTP_BUSY: "Sedang banyak permintaan kode. Coba lagi dalam satu menit.",
  OTP_INVALID: "Kode salah.",
  OTP_EXPIRED: "Kode sudah tidak berlaku. Minta kode baru.",
  OTP_TOO_MANY_ATTEMPTS: "Terlalu banyak kode salah. Minta kode baru.",
  OTP_NOT_SENT: "Kode belum terkirim. Tunggu sebentar atau minta kode baru.",
  OTP_REQUEST_IN_PROGRESS: "Permintaan kode sedang diproses. Coba lagi sebentar.",
  INVALID_PHONE: "Nomor HP tidak valid. Gunakan nomor seluler Indonesia, mis. 0812xxxxxxx.",
  INVALID_EMAIL: "Alamat email tidak valid.",
  INVALID_NAME: "Nama wajib diisi (maksimal 100 karakter).",
  INVALID_NIM: "NIM hanya huruf dan angka, 5–20 karakter.",
  NIM_TAKEN: "NIM sudah terdaftar. Masuk dengan nomor HP Anda atau hubungi admin.",
  INVALID_GROUP: "Kelompok tidak tersedia. Pilih kelompok lain.",
  INVALID_CODE: "Kode hanya berisi angka.",
  INVALID_CREDENTIALS: "Nomor HP atau password salah.",
  INVALID_PASSWORD: "Password minimal 8 karakter (maksimal 72 byte) dan tidak boleh hanya spasi.",
  PASSWORD_REQUIRED: "Masukkan password baru.",
  ACCOUNT_BLOCKED: "Akun Anda diblokir. Hubungi admin.",
  INVALID_LABEL: "Ada label yang sudah tidak tersedia. Pilih ulang label pada kotak yang ditandai.",
  LABEL_ARCHIVED: "Ada label yang sudah diarsipkan admin. Pilih ulang label pada kotak yang ditandai.",
};

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === "RATE_LIMITED" && e.retryAfter) {
      return `Terlalu banyak permintaan. Coba lagi dalam ${formatWait(e.retryAfter)}.`;
    }
    if (e.code === "LOGIN_LOCKED") {
      return `Terlalu banyak percobaan masuk untuk nomor ini. Coba lagi${e.retryAfter ? ` dalam ${formatWait(e.retryAfter)}` : " nanti"}, atau gunakan "Lupa password".`;
    }
    const base = MESSAGES[e.code];
    if (base) return base;
    if (e.status >= 500) return "Terjadi gangguan di server. Coba lagi nanti.";
    return e.message || "Terjadi kesalahan.";
  }
  if (e instanceof Error && e.message) return e.message;
  return "Terjadi kesalahan.";
}

export function isCode(e: unknown, ...codes: string[]): e is ApiError {
  return e instanceof ApiError && codes.includes(e.code);
}

function formatWait(sec: number): string {
  return sec < 90 ? `${Math.ceil(sec)} detik` : `${Math.ceil(sec / 60)} menit`;
}
