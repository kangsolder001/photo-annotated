import { faArrowLeft } from "@fortawesome/free-solid-svg-icons/faArrowLeft";
import { faPaperPlane } from "@fortawesome/free-solid-svg-icons/faPaperPlane";
import { faRotate } from "@fortawesome/free-solid-svg-icons/faRotate";
import { useCallback, useRef, useState, type FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { Icon } from "../../components/Icon";
import { ThemeSwitcher } from "../../components/ThemeSwitcher";
import { Turnstile, type TurnstileHandle } from "../../components/Turnstile";
import { Banner } from "../../components/ui";
import { isCode } from "../../lib/errors";
import { ChannelBanner } from "../auth/ChannelBanner";
import { OtpCodeInput } from "../auth/OtpCodeInput";
import { describeStatus } from "../auth/otpStatus";
import { OTP_CODE_LEN, resendCooldown, useNow, useOtpStatus } from "../auth/useOtpStatus";
import { adminApi } from "./api";
import { adminErrorMessage } from "./errors";
import { adminPending, safeAdminNext, type AdminPending } from "./pending";
import { adminSession } from "./session";

/**
 * /admin/masuk: nomor HP superadmin → kode WhatsApp → token admin (sessionStorage). Nomor lain
 * mendapat respons yang sama (anti-enumerasi), jadi pesannya netral.
 */
export function AdminLoginPage() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const next = safeAdminNext(params.get("next"));
  const [p, setP] = useState<AdminPending | null>(() => adminPending.get());
  const [phone, setPhone] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [mustResend, setMustResend] = useState(false);
  const turnstile = useRef<TurnstileHandle>(null);
  const input = useRef<HTMLInputElement>(null);
  const status = useOtpStatus(p?.challengeId, p?.requestedAt, adminApi.otpStatus);
  const now = useNow();

  const requestCode = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(undefined);
    try {
      const t = await adminApi.requestOTP({ phone, turnstile_token: await turnstile.current?.token() });
      setP(adminPending.set(t, next));
      setCode("");
      setMustResend(false);
    } catch (err) {
      setError(adminErrorMessage(err));
      if (isCode(err, "TURNSTILE_FAILED")) turnstile.current?.reset();
    } finally {
      setBusy(false);
    }
  };

  const verify = useCallback(async (value: string) => {
    if (!p || busy || value.length !== OTP_CODE_LEN) return;
    setBusy(true);
    setError(undefined);
    try {
      const r = await adminApi.verifyOTP({ challenge_id: p.challengeId, code: value });
      adminSession.save(r.access_token, r.expires_at, r.admin);
      adminPending.clear();
      navigate(p.next, { replace: true });
    } catch (e) {
      setCode("");
      if (isCode(e, "OTP_INVALID")) {
        const left = e.details?.attempts_left;
        setError(typeof left === "number" ? `Kode salah. Sisa ${left} percobaan.` : "Kode salah.");
      } else {
        setError(adminErrorMessage(e));
        if (isCode(e, "OTP_EXPIRED", "OTP_TOO_MANY_ATTEMPTS")) setMustResend(true);
      }
      setBusy(false);
      input.current?.focus();
    }
  }, [p, busy, navigate]);

  const resend = async () => {
    if (!p || busy) return;
    setBusy(true);
    setError(undefined);
    try {
      const t = await adminApi.resendOTP({ challenge_id: p.challengeId, turnstile_token: await turnstile.current?.token() });
      setP(adminPending.set(t, p.next));
      setMustResend(false);
      setCode("");
    } catch (e) {
      setError(adminErrorMessage(e));
      if (isCode(e, "TURNSTILE_FAILED")) turnstile.current?.reset();
    } finally {
      setBusy(false);
    }
  };

  const changeNumber = () => {
    adminPending.clear();
    setP(null);
    setError(undefined);
    setCode("");
  };

  const info = p ? describeStatus(status, p.estimatedWait) : null;
  const cooldown = p ? resendCooldown(p.requestedAt, p.resendAfter, now) : 0;
  const canResend = !!info?.needsResend || mustResend || cooldown === 0;

  return (
    <div className="admin-login">
      <div className="admin-login-head">
        <span className="brand">Photo Annotated · Admin</span>
        <ThemeSwitcher />
      </div>
      <h1>Masuk admin</h1>
      <ChannelBanner check={adminApi.channelStatus} />
      {!p ? (
        <form className="card stack" onSubmit={requestCode} noValidate>
          <label className="field">
            Nomor WhatsApp superadmin
            <span className="hint">Kode masuk dikirim ke WhatsApp nomor yang terdaftar sebagai superadmin.</span>
            <input type="tel" value={phone} onChange={(e) => setPhone(e.target.value)} autoComplete="tel" inputMode="tel" maxLength={20} required autoFocus />
          </label>
          {error && <Banner kind="error">{error}</Banner>}
          <Turnstile ref={turnstile} />
          <button type="submit" className="btn btn-primary btn-block" disabled={busy || !phone.trim()}>
            {busy ? <><span className="spinner" aria-hidden="true" /> Mengirim kode…</> : <><Icon icon={faPaperPlane} /> Kirim kode WhatsApp</>}
          </button>
        </form>
      ) : (
        <form className="card stack" onSubmit={(e) => { e.preventDefault(); void verify(code); }}>
          {info && <Banner kind={info.kind}>{info.text}</Banner>}
          <OtpCodeInput ref={input} value={code} onChange={setCode} onComplete={(v) => void verify(v)} disabled={busy || mustResend} />
          {error && <Banner kind="error">{error}</Banner>}
          <button type="submit" className="btn btn-primary btn-block" disabled={busy || code.length !== OTP_CODE_LEN || mustResend}>
            {busy ? <><span className="spinner" aria-hidden="true" /> Memeriksa…</> : "Masuk"}
          </button>
          <div className="row">
            <button type="button" className="btn" onClick={resend} disabled={busy || !canResend}>
              <Icon icon={faRotate} /> {canResend ? "Kirim ulang kode" : `Kirim ulang dalam ${cooldown} detik`}
            </button>
            <button type="button" className="btn" onClick={changeNumber} disabled={busy}><Icon icon={faArrowLeft} /> Ganti nomor</button>
          </div>
          <p className="muted">Kode hanya dikirim bila nomor ini adalah nomor superadmin di konfigurasi server.</p>
          <Turnstile ref={turnstile} />
        </form>
      )}
    </div>
  );
}
