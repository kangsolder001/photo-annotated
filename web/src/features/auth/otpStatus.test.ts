import { describeStatus } from "./otpStatus";

const base = { resend_after_seconds: 0, expires_at: null, failure_reason: null };

it("antre menampilkan perkiraan waktu", () => {
  expect(describeStatus({ ...base, status: "queued" }, 8).text).toMatch(/8 detik/);
});
it("nomor tanpa WhatsApp mendapat pesan yang jelas dan bisa kirim ulang", () => {
  const s = describeStatus({ ...base, status: "failed", failure_reason: "SEND_FAILED" }, 0);
  expect(s).toMatchObject({ kind: "error", needsResend: true });
  expect(s.text).toMatch(/memakai WhatsApp/);
});
it("gateway bermasalah", () => {
  expect(describeStatus({ ...base, status: "failed", failure_reason: "WA_UNAVAILABLE" }, 0).text).toMatch(/bermasalah/);
});
it("kedaluwarsa meminta kode baru", () => {
  expect(describeStatus({ ...base, status: "expired" }, 0).needsResend).toBe(true);
});
