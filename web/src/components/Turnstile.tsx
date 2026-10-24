import { useEffect, useImperativeHandle, useRef, useState, type Ref } from "react";
import { mountTurnstile, turnstileEnabled } from "../lib/turnstile";

export interface TurnstileHandle {
  /** Token Turnstile; undefined bila Turnstile tidak diaktifkan. */
  token(): Promise<string | undefined>;
  reset(): void;
}

/** Widget Turnstile (biasanya tak terlihat; muncul hanya bila Cloudflare meminta interaksi). */
export function Turnstile({ ref }: { ref: Ref<TurnstileHandle> }) {
  const el = useRef<HTMLDivElement>(null);
  const ctl = useRef<Awaited<ReturnType<typeof mountTurnstile>> | null>(null);
  const ready = useRef<Promise<void> | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!turnstileEnabled() || !el.current) return;
    let cancelled = false;
    ready.current = mountTurnstile(el.current).then(
      (c) => {
        if (cancelled) c.destroy();
        else ctl.current = c;
      },
      (e: Error) => setError(e.message),
    );
    return () => {
      cancelled = true;
      ctl.current?.destroy();
      ctl.current = null;
    };
  }, []);

  useImperativeHandle(ref, () => ({
    async token() {
      if (!turnstileEnabled()) return undefined;
      await ready.current;
      if (!ctl.current) throw new Error(error ?? "Verifikasi keamanan belum siap.");
      return ctl.current.token();
    },
    reset() {
      ctl.current?.reset();
    },
  }), [error]);

  if (!turnstileEnabled()) return null;
  return (
    <>
      <div ref={el} />
      {error && <p className="field-error">{error}</p>}
    </>
  );
}
