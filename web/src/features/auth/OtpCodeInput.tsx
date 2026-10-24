import type { Ref } from "react";
import { OTP_CODE_LEN } from "./useOtpStatus";

/** Isian kode 6 digit dari WhatsApp (one-time-code, keyboard angka; tempel juga bisa). */
export function OtpCodeInput(props: {
  value: string;
  onChange(v: string): void;
  /** Dipanggil saat 6 digit lengkap (diketik atau ditempel). */
  onComplete?(v: string): void;
  disabled?: boolean;
  ref?: Ref<HTMLInputElement>;
}) {
  return (
    <>
      <label className="field">
        Kode 6 digit dari WhatsApp
        <input
          ref={props.ref}
          className="otp-input"
          value={props.value}
          onChange={(e) => {
            const v = e.target.value.replace(/\D/g, "").slice(0, OTP_CODE_LEN);
            props.onChange(v);
            if (v.length === OTP_CODE_LEN) props.onComplete?.(v);
          }}
          autoComplete="one-time-code"
          inputMode="numeric"
          pattern="[0-9]*"
          maxLength={OTP_CODE_LEN}
          autoFocus
          disabled={props.disabled}
          aria-describedby="otp-help"
        />
      </label>
      <span id="otp-help" className="muted">Buka WhatsApp, cari pesan berisi kode, lalu ketik atau tempel di sini.</span>
    </>
  );
}
