import { useId, useState } from "react";

/** Isian password dengan tombol tampilkan/sembunyikan (memudahkan mengetik di HP). */
export function PasswordField(props: {
  label: string;
  value: string;
  onChange(v: string): void;
  autoComplete: "current-password" | "new-password";
  hint?: string;
  error?: string | null;
}) {
  const [show, setShow] = useState(false);
  const id = useId();
  return (
    <div className="field">
      <label htmlFor={id}>{props.label}</label>
      {props.hint && <span className="hint">{props.hint}</span>}
      <div className="password-row">
        <input
          id={id}
          type={show ? "text" : "password"}
          value={props.value}
          onChange={(e) => props.onChange(e.target.value)}
          autoComplete={props.autoComplete}
          aria-invalid={!!props.error}
          aria-describedby={props.error ? `${id}-err` : undefined}
          maxLength={72}
          required
        />
        <button type="button" className="btn btn-small" onClick={() => setShow((v) => !v)} aria-pressed={show}>
          {show ? "Sembunyikan" : "Lihat"}
        </button>
      </div>
      {props.error && <span id={`${id}-err`} className="field-error">{props.error}</span>}
    </div>
  );
}
