import { useRef, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { api } from "../../api/endpoints";
import { Turnstile, type TurnstileHandle } from "../../components/Turnstile";
import { Banner } from "../../components/ui";
import { errorMessage, isCode } from "../../lib/errors";
import { ChannelBanner } from "./ChannelBanner";
import { pending, safeNext } from "./pending";

/** Lupa password: kode dikirim ke WhatsApp nomor yang terdaftar, lalu buat password baru. */
export function ForgotPage() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const next = safeNext(params.get("next"));
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
      const t = await api.forgotPassword({ phone, turnstile_token: await turnstile.current?.token() });
      pending.set(t, "reset", next);
      navigate("/masuk/kode");
    } catch (err) {
      setError(errorMessage(err));
      if (isCode(err, "TURNSTILE_FAILED")) turnstile.current?.reset();
      setBusy(false);
    }
  };

  return (
    <>
      <h1>Lupa password</h1>
      <p className="muted">Kami kirim kode ke WhatsApp nomor Anda. Setelah kode dimasukkan, Anda bisa membuat password baru.</p>
      <ChannelBanner />
      <form className="card stack" onSubmit={submit} noValidate>
        <label className="field">
          Nomor WhatsApp terdaftar
          <input type="tel" value={phone} onChange={(e) => setPhone(e.target.value)} autoComplete="tel" inputMode="tel" maxLength={20} required />
        </label>
        {error && <Banner kind="error">{error}</Banner>}
        <Turnstile ref={turnstile} />
        <button type="submit" className="btn btn-primary btn-block" disabled={busy || !phone.trim()}>
          {busy ? <><span className="spinner" aria-hidden="true" /> Mengirim kode…</> : "Kirim kode WhatsApp"}
        </button>
        <Link to={`/masuk?next=${encodeURIComponent(next)}`}>Kembali ke halaman masuk</Link>
      </form>
    </>
  );
}
