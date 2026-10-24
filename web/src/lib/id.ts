// UUID v4 acak. crypto.randomUUID() hanya ada di konteks aman (HTTPS/localhost); saat aplikasi dibuka
// lewat http://<ip-LAN> ia tidak tersedia, sedangkan crypto.getRandomValues() selalu ada.
export function uuid(): string {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID();
  const b = crypto.getRandomValues(new Uint8Array(16));
  b[6] = (b[6]! & 0x0f) | 0x40; // versi 4
  b[8] = (b[8]! & 0x3f) | 0x80; // varian RFC 4122
  const h = [...b].map((x) => x.toString(16).padStart(2, "0")).join("");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}
