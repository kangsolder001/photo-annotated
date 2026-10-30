// Satu fungsi per endpoint API. Komponen tidak memanggil fetch langsung.
import { request } from "./client";
import type {
  EditResponse,
  GroupRef,
  Label,
  LoginResponse,
  OTPStatusView,
  OTPTicket,
  PhotoURL,
  PresignResponse,
  SubmissionView,
  SubmitPhotoDTO,
  SubmitResponse,
  User,
} from "./types";

const enc = encodeURIComponent;

function editTokenHeader(editToken?: string): Record<string, string> {
  return editToken ? { "X-Edit-Token": editToken } : {};
}

function turnstileHeader(token?: string): Record<string, string> {
  return token ? { "X-Turnstile-Token": token } : {};
}

export const api = {
  /** Label aktif untuk pemilih label (publik pada mode none, butuh sesi pada mode otp). */
  labels: () => request<{ labels: Label[] }>("GET", "/api/v1/labels").then((r) => r.labels ?? []),

  presign: (p: { content_type: string; size_bytes: number; width: number; height: number }, turnstileToken?: string) =>
    request<PresignResponse>("POST", "/api/v1/uploads/presign", { body: p, headers: turnstileHeader(turnstileToken) }),

  submit: (body: { photos: SubmitPhotoDTO[]; client_request_id: string }, turnstileToken?: string) =>
    request<SubmitResponse>("POST", "/api/v1/submissions", { body, headers: turnstileHeader(turnstileToken) }),

  getSubmission: (id: string, editToken?: string) =>
    request<SubmissionView>("GET", `/api/v1/submissions/${enc(id)}`, { headers: editTokenHeader(editToken) }),

  editSubmission: (id: string, body: { version: number; photos: SubmitPhotoDTO[] }, editToken?: string) =>
    request<EditResponse>("PUT", `/api/v1/submissions/${enc(id)}`, { body, headers: editTokenHeader(editToken) }),

  photoURL: (photoId: string, editToken?: string) =>
    request<PhotoURL>("GET", `/api/v1/photos/${enc(photoId)}/url`, { headers: editTokenHeader(editToken) }),

  /** Kelompok aktif untuk dropdown registrasi (publik, urut nama). */
  groups: () => request<{ groups: GroupRef[] }>("GET", "/api/v1/groups", { auth: false }).then((r) => r.groups ?? []),

  // ---- auth (mode otp)
  register: (body: {
    name: string;
    email: string;
    phone: string;
    password: string;
    nim: string;
    group_id: number;
    turnstile_token?: string;
  }) =>
    request<OTPTicket>("POST", "/api/v1/auth/register", { body, auth: false }),
  login: (body: { phone: string; password: string }) =>
    request<LoginResponse>("POST", "/api/v1/auth/login", { body, auth: false }),
  forgotPassword: (body: { phone: string; turnstile_token?: string }) =>
    request<OTPTicket>("POST", "/api/v1/auth/password/forgot", { body, auth: false }),
  resendOTP: (body: { challenge_id: string; turnstile_token?: string }) =>
    request<OTPTicket>("POST", "/api/v1/auth/otp/resend", { body, auth: false }),
  otpStatus: (challengeId: string) =>
    request<OTPStatusView>("GET", `/api/v1/auth/otp/${enc(challengeId)}`, { auth: false }),
  verifyOTP: (body: { challenge_id: string; code: string; new_password?: string }) =>
    request<LoginResponse>("POST", "/api/v1/auth/otp/verify", { body, auth: false }),
  channelStatus: () => request<{ otp_available: boolean }>("GET", "/api/v1/auth/channel-status", { auth: false }),
  me: () => request<User>("GET", "/api/v1/auth/me"),
  logout: () => request<void>("POST", "/api/v1/auth/logout"),
};
