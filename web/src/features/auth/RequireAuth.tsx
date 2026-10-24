import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { config } from "../../config";
import { useSessionUser } from "./useSession";

/** Mode otp: halaman di dalamnya butuh login; tanpa sesi diarahkan ke /masuk lalu kembali. */
export function RequireAuth({ children }: { children: ReactNode }) {
  const user = useSessionUser();
  const loc = useLocation();
  if (config.authMode === "otp" && !user) {
    return <Navigate to={`/masuk?next=${encodeURIComponent(loc.pathname + loc.search)}`} replace />;
  }
  return <>{children}</>;
}
