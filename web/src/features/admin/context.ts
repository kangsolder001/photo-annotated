import { createContext, useContext, useEffect } from "react";
import type { AdminMe } from "./types";

export interface AdminContext {
  me: AdminMe | null;
  /** Mode auth server (dari /admin/me); menu Annotator hanya pada mode otp. */
  authMode: "none" | "otp" | null;
  setTitle(t: string): void;
}

export const AdminCtx = createContext<AdminContext>({ me: null, authMode: null, setTitle: () => {} });
export const useAdmin = () => useContext(AdminCtx);

/** Judul halaman (bar atas HP + judul tab). */
export function usePageTitle(title: string) {
  const { setTitle } = useAdmin();
  useEffect(() => {
    setTitle(title);
    document.title = `${title} · Admin Photo Annotated`;
  }, [title, setTitle]);
}
