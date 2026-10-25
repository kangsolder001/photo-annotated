// Pembungkus localStorage yang aman: mode privat/penyimpanan diblokir tidak membuat aplikasi crash.

export function readJSON<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    return raw == null ? fallback : (JSON.parse(raw) as T);
  } catch {
    return fallback;
  }
}

export function writeJSON(key: string, value: unknown): void {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // penuh atau diblokir: data ini hanya kenyamanan, aplikasi tetap berjalan
  }
}

export function remove(key: string): void {
  try {
    localStorage.removeItem(key);
  } catch {
    // abaikan
  }
}
