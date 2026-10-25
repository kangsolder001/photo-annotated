import { useRef, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { api } from "../../api/endpoints";
import { PasswordField } from "../../components/PasswordField";
import { Turnstile, type TurnstileHandle } from "../../components/Turnstile";
import { Banner } from "../../components/ui";
import { errorMessage, isCode } from "../../lib/errors";
import { passwordProblem } from "../../lib/password";
import { session } from "../../lib/session";
import { ChannelBanner } from "./ChannelBanner";
import { pending, safeNext } from "./pending";

type Tab = "login" | "register";

/**
 * Masuk: nomor HP + password. Daftar: nama, email, nomor HP, password → kode WhatsApp untuk
 * memverifikasi nomor (OTP hanya dipakai saat daftar dan lupa password).
 */
export function LoginPage() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const next = safeNext(params.get("next"));
  const [tab, setTab] = useState<Tab>(params.get("daftar") ? "register" : "login");
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [phone, setPhone] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const turnstile = useRef<TurnstileHandle>(null);

  const pwProblem = tab === "register" ? passwordProblem(password) : null;
  const confirmProblem = tab === "register" && confirm !== password ? "Konfirmasi password tidak sama." : null;
  const switchTab = (t: Tab) => {
    setTab(t);
    setError(undefined);
    setTouched(false);
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setTouched(true);
    if (tab === "register" && (pwProblem || confirmProblem)) return;
    setBusy(true);
    setError(undefined);
    try {
      if (tab === "login") {
        const r = await api.login({ phone, password });
        session.save(r.access_token, r.expires_at, r.user);
        navigate(next, { replace: true });
        return;
      }
      const ticket = await api.register({ name, email, phone, password, turnstile_token: await turnstile.current?.token() });
      pending.set(ticket, "register", next);
      navigate("/masuk/kode");
    } catch (err) {
      setError(errorMessage(err));
      if (isCode(err, "TURNSTILE_FAILED")) turnstile.current?.reset();
      setBusy(false);
    }
  };

  const filled = phone.trim() && password && (tab === "login" || (name.trim() && email.trim() && confirm));

  return (
    <>
      <h1>{tab === "login" ? "Masuk" : "Daftar"}</h1>
      {tab === "register" && <ChannelBanner />}
      <div className="tabs" role="tablist">
        <button type="button" role="tab" aria-selected={tab === "login"} onClick={() => switchTab("login")}>Masuk</button>
        <button type="button" role="tab" aria-selected={tab === "register"} onClick={() => switchTab("register")}>Daftar baru</button>
      </div>
      <form className="card stack" onSubmit={submit} noValidate>
        {tab === "register" && (
          <>
            <label className="field">
              Nama
              <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" maxLength={100} required />
            </label>
            <label className="field">
              Email
              <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" inputMode="email" maxLength={254} required />
            </label>
          </>
        )}
        <label className="field">
          Nomor WhatsApp
          <span className="hint">Nomor seluler Indonesia, mis. 0812 3456 7890</span>
          <input type="tel" value={phone} onChange={(e) => setPhone(e.target.value)} autoComplete="tel" inputMode="tel" maxLength={20} required />
        </label>
        <PasswordField
          label="Password"
          value={password}
          onChange={setPassword}
          autoComplete={tab === "login" ? "current-password" : "new-password"}
          hint={tab === "register" ? "Minimal 8 karakter." : undefined}
          error={touched ? pwProblem : null}
        />
        {tab === "register" && (
          <PasswordField label="Ulangi password" value={confirm} onChange={setConfirm} autoComplete="new-password" error={touched ? confirmProblem : null} />
        )}
        {error && <Banner kind="error">{error}</Banner>}
        {tab === "register" && <Turnstile ref={turnstile} />}
        <button type="submit" className="btn btn-primary btn-block" disabled={busy || !filled}>
          {busy ? <><span className="spinner" aria-hidden="true" /> {tab === "login" ? "Masuk…" : "Mengirim kode…"}</> : tab === "login" ? "Masuk" : "Daftar & kirim kode WhatsApp"}
        </button>
        {tab === "login" ? (
          <div className="row" style={{ justifyContent: "space-between" }}>
            <Link to={`/masuk/lupa?next=${encodeURIComponent(next)}`}>Lupa password?</Link>
            <button type="button" className="link-button" onClick={() => switchTab("register")}>Belum punya akun? Daftar</button>
          </div>
        ) : (
          <p className="muted">Kode verifikasi dikirim ke WhatsApp untuk memastikan nomor ini milik Anda. Setelah itu Anda masuk dengan nomor HP dan password.</p>
        )}
      </form>
    </>
  );
}
