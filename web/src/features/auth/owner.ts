import { config } from "../../config";
import { session } from "../../lib/session";

/** Pemilik catatan lokal (riwayat, draf): pengguna yang login (mode otp) atau "anon". */
export function currentOwner(): string {
  return config.authMode === "otp" ? (session.user()?.id ?? "anon") : "anon";
}
