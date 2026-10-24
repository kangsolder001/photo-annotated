import { useEffect, useState } from "react";
import { api } from "../../api/endpoints";
import { Banner } from "../../components/ui";

/** Banner bila pengiriman kode WhatsApp sedang bermasalah (GET /auth/channel-status). */
export function ChannelBanner() {
  const [available, setAvailable] = useState(true);
  useEffect(() => {
    let alive = true;
    const check = () => api.channelStatus().then((r) => alive && setAvailable(r.otp_available), () => {});
    void check();
    const t = setInterval(check, 30_000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, []);
  if (available) return null;
  return <Banner kind="warn">Pengiriman kode WhatsApp sedang bermasalah. Coba lagi beberapa saat lagi.</Banner>;
}
