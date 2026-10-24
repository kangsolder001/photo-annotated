import { useCallback, useRef, useState, type FormEvent } from "react";
import { Link, Navigate, useNavigate } from "react-router-dom";
import { api } from "../../api/endpoints";
import { PasswordField } from "../../components/PasswordField";
import { Turnstile, type TurnstileHandle } from "../../components/Turnstile";
import { Banner } from "../../components/ui";
import { errorMessage, isCode } from "../../lib/errors";
import { passwordProblem } from "../../lib/password";
import { session } from "../../lib/session";
import { ChannelBanner } from "./ChannelBanner";
import { OtpCodeInput } from "./OtpCodeInput";
import { describeStatus } from "./otpStatus";
import { OTP_CODE_LEN, resendCooldown, useNow, useOtpStatus } from "./useOtpStatus";
import { pending as store, type PendingOTP } from "./pending";

/**
 * Masukkan kode WhatsApp; status pengiriman dipantau tiap 2 detik sampai kode terkirim/gagal.
 * Registrasi: kode saja (otomatis terkirim saat 6 digit). Reset password: kode + password baru.
 */
export function OtpPage() {
  const navigate = useNavigate();
  const [p, setP] = useState<PendingOTP | null>(() => store.get());
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [mustResend, setMustResend] = useState(false);
  const [newPassword, setNewPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [touched, setTouched] = useState(false);
  const isReset = p?.purpose === "reset";
  const pwProblem = isReset ? passwordProblem(newPassword) ?? (confirm !== newPassword ? "Konfirmasi password tidak sama." : null) : null;
  const turnstile = useRef<TurnstileHandle>(null);
  const input = useRef<HTMLInputElement>(null);
  const status = useOtpStatus(p?.challengeId, p?.requestedAt, api.otpStatus);
  const now = useNow();

  const verify = useCallback(async (value: string) => {
    if (!p || busy || value.length !== OTP_CODE_LEN) return;
    if (p.purpose === "reset") {
      setTouched(true);
      if (pwProblem) return;
    }
    setBusy(true);
    setError(undefined);
    try {
      const r = await api.verifyOTP({ challenge_id: p.challengeId, code: value, new_password: p.purpose === "reset" ? newPassword : undefined });
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
  }, [p, busy, navigate, pwProblem, newPassword]);

  if (!p) return <Navigate to="/masuk" replace />;

  const info = describeStatus(status, p.estimatedWait);
  const cooldownLeft = resendCooldown(p.requestedAt, p.resendAfter, now);
  // gagal kirim/kedaluwarsa: kirim ulang langsung boleh (jatah dikembalikan server)
  const canResend = info.needsResend || mustResend || cooldownLeft === 0;

  const resend = async () => {
    if (busy) return;
    setBusy(true);
    setError(undefined);
    try {
      const t = await api.resendOTP({ challenge_id: p.challengeId, turnstile_token: await turnstile.current?.token() });
      setP(store.set(t, p.purpose, p.next));
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
      <h1>{isReset ? "Buat password baru" : "Verifikasi nomor"}</h1>
      <ChannelBanner />
      <Banner kind={info.kind}>{info.text}</Banner>
      <form className="card stack" onSubmit={onSubmit}>
        <OtpCodeInput
          ref={input}
          value={code}
          onChange={setCode}
          // registrasi: otomatis saat lengkap (juga saat ditempel); reset menunggu password baru
          onComplete={isReset ? undefined : (v) => void verify(v)}
          disabled={busy || mustResend}
        />
        {isReset && (
          <>
            <PasswordField label="Password baru" value={newPassword} onChange={setNewPassword} autoComplete="new-password" hint="Minimal 8 karakter." />
            <PasswordField label="Ulangi password baru" value={confirm} onChange={setConfirm} autoComplete="new-password" error={touched ? pwProblem : null} />
          </>
        )}
        {error && <Banner kind="error">{error}</Banner>}
        <button type="submit" className="btn btn-primary btn-block" disabled={busy || code.length !== OTP_CODE_LEN || mustResend || (isReset && !confirm)}>
          {busy ? <><span className="spinner" aria-hidden="true" /> Memeriksa…</> : isReset ? "Simpan password & masuk" : "Verifikasi & masuk"}
        </button>
        <div className="row">
          <button type="button" className="btn" onClick={resend} disabled={busy || !canResend}>
            {canResend ? "Kirim ulang kode" : `Kirim ulang dalam ${cooldownLeft} detik`}
          </button>
          <Link to={isReset ? "/masuk/lupa" : "/masuk?daftar=1"} onClick={() => store.clear()}>Ganti nomor</Link>
        </div>
        {!isReset && (
          <p className="muted">Tidak menerima kode? Bila nomor ini sudah terdaftar, kode tidak dikirim: silakan <Link to="/masuk" onClick={() => store.clear()}>masuk</Link> atau gunakan <Link to="/masuk/lupa" onClick={() => store.clear()}>lupa password</Link>.</p>
        )}
        <Turnstile ref={turnstile} />
      </form>
    </>
  );
}
