import { useRef, useState, type FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { api } from "../../api/endpoints";
import { Turnstile, type TurnstileHandle } from "../../components/Turnstile";
import { Banner } from "../../components/ui";
import { errorMessage, isCode } from "../../lib/errors";
import { ChannelBanner } from "./ChannelBanner";
import { pending, safeNext } from "./pending";

type Tab = "login" | "register";

/** Masuk (nomor HP saja) atau daftar (nama, email, nomor HP); keduanya lanjut ke kode WhatsApp. */
export function LoginPage() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const next = safeNext(params.get("next"));
  const [tab, setTab] = useState<Tab>("login");
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [phone, setPhone] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const turnstile = useRef<TurnstileHandle>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(undefined);
    try {
      const turnstile_token = await turnstile.current?.token();
      const ticket = tab === "register"
        ? await api.register({ name, email, phone, turnstile_token })
        : await api.requestOTP({ phone, turnstile_token });
      pending.set(ticket, phone, next);
      navigate("/masuk/kode");
    } catch (err) {
      setError(errorMessage(err));
      if (isCode(err, "TURNSTILE_FAILED")) turnstile.current?.reset();
      setBusy(false);
    }
  };

  return (
    <>
      <h1>{tab === "login" ? "Masuk" : "Daftar"}</h1>
      <p className="muted">Kode verifikasi dikirim lewat WhatsApp. Tidak perlu kata sandi.</p>
      <ChannelBanner />
      <div className="tabs" role="tablist">
        <button type="button" role="tab" aria-selected={tab === "login"} onClick={() => setTab("login")}>Masuk</button>
        <button type="button" role="tab" aria-selected={tab === "register"} onClick={() => setTab("register")}>Daftar baru</button>
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
        {error && <Banner kind="error">{error}</Banner>}
        <Turnstile ref={turnstile} />
        <button type="submit" className="btn btn-primary btn-block" disabled={busy || !phone.trim() || (tab === "register" && (!name.trim() || !email.trim()))}>
          {busy ? <><span className="spinner" aria-hidden="true" /> Mengirim kode…</> : "Kirim kode WhatsApp"}
        </button>
        {tab === "login" && <p className="muted">Belum punya akun? Pilih <strong>Daftar baru</strong>.</p>}
      </form>
    </>
  );
}
