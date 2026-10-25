import { useEffect, useState } from "react";
import { api } from "../../api/endpoints";
import { Banner } from "../../components/ui";

/**
 * Banner bila pengiriman kode WhatsApp sedang bermasalah (GET /auth/channel-status; login admin
 * memakai GET /admin/channel-status lewat `check`).
 */
export function ChannelBanner({ check = api.channelStatus }: { check?: () => Promise<{ otp_available: boolean }> }) {
  const [available, setAvailable] = useState(true);
  useEffect(() => {
    let alive = true;
    const run = () => check().then((r) => alive && setAvailable(r.otp_available), () => {});
    void run();
    const t = setInterval(run, 30_000);
    return () => {
      alive = false;
      clearInterval(t);
    };
    // check dianggap tetap (fungsi modul)
  }, []);
  if (available) return null;
  return <Banner kind="warn">Pengiriman kode WhatsApp sedang bermasalah. Coba lagi beberapa saat lagi.</Banner>;
}
