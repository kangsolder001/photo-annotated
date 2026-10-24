import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { Link, Navigate, useNavigate } from "react-router-dom";
import { api } from "../../api/endpoints";
import type { OTPStatusView } from "../../api/types";
import { Turnstile, type TurnstileHandle } from "../../components/Turnstile";
import { Banner } from "../../components/ui";
import { errorMessage, isCode } from "../../lib/errors";
import { session } from "../../lib/session";
import { ChannelBanner } from "./ChannelBanner";
import { describeStatus } from "./otpStatus";
import { pending as store, type PendingOTP } from "./pending";

const POLL_MS = 2000;
const CODE_LEN = 6;

/** Masukkan kode WhatsApp; status pengiriman dipantau tiap 2 detik sampai kode terkirim/gagal. */
export function OtpPage() {
  const navigate = useNavigate();
  const [p, setP] = useState<PendingOTP | null>(() => store.get());
  const [status, setStatus] = useState<OTPStatusView>();
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [now, setNow] = useState(() => Date.now());
  const [mustResend, setMustResend] = useState(false);
  const turnstile = useRef<TurnstileHandle>(null);
  const input = useRef<HTMLInputElement>(null);

  // pantau status sampai final (sent/failed/expired)
  useEffect(() => {
    if (!p) return;
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const s = await api.otpStatus(p.challengeId);
        if (!alive) return;
        setStatus(s);
        if (s.status === "queued") timer = setTimeout(poll, POLL_MS);
      } catch {
        if (alive) timer = setTimeout(poll, POLL_MS * 2);
      }
    };
    void poll();
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [p]);

  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  const verify = useCallback(async (value: string) => {
    if (!p || busy || value.length !== CODE_LEN) return;
    setBusy(true);
    setError(undefined);
    try {
      const r = await api.verifyOTP({ challenge_id: p.challengeId, code: value });
      session.save(r.access_token, r.expires_at, r.user);
      store.clear();
      navigate(p.next, { replace: true });
    } catch (e) {
      setCode("");
      if (isCode(e, "OTP_INVALID")) {
        const left = e.details?.attempts_left;
        setError(typeof left === "number" ? `Kode salah. Sisa ${left} percobaan.` : "Kode salah.");
      } else {
        setError(errorMessage(e));
        if (isCode(e, "OTP_EXPIRED", "OTP_TOO_MANY_ATTEMPTS")) setMustResend(true);
      }
      setBusy(false);
      input.current?.focus();
    }
  }, [p, busy, navigate]);

  if (!p) return <Navigate to="/masuk" replace />;

  const info = describeStatus(status, p.estimatedWait);
  const cooldownLeft = Math.max(0, Math.ceil((p.requestedAt + p.resendAfter * 1000 - now) / 1000));
  // gagal kirim/kedaluwarsa: kirim ulang langsung boleh (jatah dikembalikan server)
  const canResend = info.needsResend || mustResend || cooldownLeft === 0;

  const resend = async () => {
    if (busy) return;
    setBusy(true);
    setError(undefined);
    try {
      const t = await api.requestOTP({ phone: p.phone, turnstile_token: await turnstile.current?.token() });
      setP(store.set(t, p.phone, p.next));
      setStatus(undefined);
      setMustResend(false);
      setCode("");
    } catch (e) {
      setError(errorMessage(e));
      if (isCode(e, "TURNSTILE_FAILED")) turnstile.current?.reset();
    } finally {
      setBusy(false);
    }
  };

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    void verify(code);
  };

  return (
    <>
      <h1>Masukkan kode</h1>
      <ChannelBanner />
      <Banner kind={info.kind}>{info.text}</Banner>
      <form className="card stack" onSubmit={onSubmit}>
        <label className="field">
          Kode 6 digit dari WhatsApp
          <input
            ref={input}
            className="otp-input"
            value={code}
            onChange={(e) => {
              const v = e.target.value.replace(/\D/g, "").slice(0, CODE_LEN);
              setCode(v);
              if (v.length === CODE_LEN) void verify(v); // otomatis saat lengkap (juga saat ditempel)
            }}
            autoComplete="one-time-code"
            inputMode="numeric"
            pattern="[0-9]*"
            maxLength={CODE_LEN}
            autoFocus
            disabled={busy || mustResend}
            aria-describedby="otp-help"
          />
        </label>
        <span id="otp-help" className="muted">Buka WhatsApp, cari pesan berisi kode, lalu ketik atau tempel di sini.</span>
        {error && <Banner kind="error">{error}</Banner>}
        <button type="submit" className="btn btn-primary btn-block" disabled={busy || code.length !== CODE_LEN || mustResend}>
          {busy ? <><span className="spinner" aria-hidden="true" /> Memeriksa…</> : "Masuk"}
        </button>
        <div className="row">
          <button type="button" className="btn" onClick={resend} disabled={busy || !canResend}>
            {canResend ? "Kirim ulang kode" : `Kirim ulang dalam ${cooldownLeft} detik`}
          </button>
          <Link to="/masuk" onClick={() => store.clear()}>Ganti nomor</Link>
        </div>
        <Turnstile ref={turnstile} />
      </form>
    </>
  );
}
