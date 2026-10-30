import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { api } from "../../api/endpoints";
import type { GroupRef } from "../../api/types";
import { PasswordField } from "../../components/PasswordField";
import { Turnstile, type TurnstileHandle } from "../../components/Turnstile";
import { Banner } from "../../components/ui";
import { errorMessage, isCode } from "../../lib/errors";
import { passwordProblem } from "../../lib/password";
import { session } from "../../lib/session";
import { ChannelBanner } from "./ChannelBanner";
import { pending, safeNext } from "./pending";

type Tab = "login" | "register";

/** NIM: huruf/angka 5–20 karakter (spec-pendataan §5.4); disimpan huruf besar. */
const NIM_RE = /^[A-Z0-9]{5,20}$/;

/**
 * Masuk: nomor HP + password. Daftar: nama, email, NIM, kelompok, nomor HP, password → kode WhatsApp untuk
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
  const [nim, setNim] = useState("");
  const [groupId, setGroupId] = useState("");
  /** null = sedang dimuat. */
  const [groups, setGroups] = useState<GroupRef[] | null>(null);
  const [groupsError, setGroupsError] = useState<string>();
  /** Error dari server yang ditampilkan di field-nya. */
  const [fieldError, setFieldError] = useState<{ nim?: string; group?: string }>({});
  const nimId = useId();
  const groupFieldId = useId();
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const turnstile = useRef<TurnstileHandle>(null);

  useEffect(() => {
    if (tab !== "register" || groups !== null) return;
    let alive = true;
    api.groups().then(
      (g) => alive && setGroups(g),
      (e: unknown) => alive && setGroupsError(errorMessage(e)),
    );
    return () => {
      alive = false;
    };
  }, [tab, groups]);

  const nimValue = nim.trim();
  const nimProblem = tab === "register" && !NIM_RE.test(nimValue) ? "NIM hanya huruf dan angka, 5–20 karakter." : null;
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
    if (tab === "register" && (pwProblem || confirmProblem || nimProblem)) return;
    setBusy(true);
    setError(undefined);
    setFieldError({});
    try {
      if (tab === "login") {
        const r = await api.login({ phone, password });
        session.save(r.access_token, r.expires_at, r.user);
        navigate(next, { replace: true });
        return;
      }
      const ticket = await api.register({
        name,
        email,
        phone,
        password,
        nim: nimValue,
        group_id: Number(groupId),
        turnstile_token: await turnstile.current?.token(),
      });
      pending.set(ticket, "register", next);
      navigate("/masuk/kode");
    } catch (err) {
      if (isCode(err, "INVALID_NIM", "NIM_TAKEN")) setFieldError({ nim: errorMessage(err) });
      else if (isCode(err, "INVALID_GROUP")) {
        setFieldError({ group: errorMessage(err) });
        setGroupId("");
        setGroups(null); // muat ulang daftar: kelompok itu mungkin baru diarsipkan
      } else setError(errorMessage(err));
      if (isCode(err, "TURNSTILE_FAILED")) turnstile.current?.reset();
      setBusy(false);
    }
  };

  const filled = phone.trim() && password && (tab === "login" || (name.trim() && email.trim() && nimValue && groupId && confirm));
  const nimError = fieldError.nim ?? (touched ? nimProblem : null);

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
            <div className="field">
              <label htmlFor={nimId}>NIM</label>
              <span className="hint">Contoh: 251FS01017</span>
              <input
                id={nimId}
                value={nim}
                onChange={(e) => {
                  setNim(e.target.value.toUpperCase());
                  setFieldError((f) => ({ ...f, nim: undefined }));
                }}
                autoCapitalize="characters"
                autoComplete="off"
                spellCheck={false}
                maxLength={20}
                aria-invalid={!!nimError}
                aria-describedby={nimError ? `${nimId}-err` : undefined}
                required
              />
              {nimError && <span id={`${nimId}-err`} className="field-error">{nimError}</span>}
            </div>
            <div className="field">
              <label htmlFor={groupFieldId}>Kelompok</label>
              {groups === null && !groupsError ? (
                <span className="hint">Memuat daftar kelompok…</span>
              ) : groupsError ? (
                <span className="field-error">{groupsError}</span>
              ) : groups && groups.length === 0 ? (
                <span className="field-error">Belum ada kelompok. Hubungi admin.</span>
              ) : null}
              <select
                id={groupFieldId}
                value={groupId}
                onChange={(e) => {
                  setGroupId(e.target.value);
                  setFieldError((f) => ({ ...f, group: undefined }));
                }}
                disabled={!groups || groups.length === 0}
                aria-invalid={!!fieldError.group}
                aria-describedby={fieldError.group ? `${groupFieldId}-err` : undefined}
                required
              >
                <option value="">Pilih kelompok</option>
                {groups?.map((g) => (
                  <option key={g.id} value={String(g.id)}>{g.name}</option>
                ))}
              </select>
              {fieldError.group && <span id={`${groupFieldId}-err`} className="field-error">{fieldError.group}</span>}
            </div>
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
