import { useSyncExternalStore } from "react";
import { session } from "../../lib/session";

/** Pengguna yang sedang login (mode otp); berubah otomatis saat login, logout, atau token ditolak. */
export function useSessionUser() {
  return useSyncExternalStore(session.subscribe, () => session.token() ?? "", () => "") ? session.user() : null;
}
