// Satu fungsi per endpoint API. Komponen tidak memanggil fetch langsung.
import { request } from "./client";
import type {
  EditResponse,
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

  // ---- auth (mode otp)
  register: (body: { name: string; email: string; phone: string; turnstile_token?: string }) =>
    request<OTPTicket>("POST", "/api/v1/auth/register", { body, auth: false }),
  requestOTP: (body: { phone: string; turnstile_token?: string }) =>
    request<OTPTicket>("POST", "/api/v1/auth/otp/request", { body, auth: false }),
  otpStatus: (challengeId: string) =>
    request<OTPStatusView>("GET", `/api/v1/auth/otp/${enc(challengeId)}`, { auth: false }),
  verifyOTP: (body: { challenge_id: string; code: string }) =>
    request<LoginResponse>("POST", "/api/v1/auth/otp/verify", { body, auth: false }),
  channelStatus: () => request<{ otp_available: boolean }>("GET", "/api/v1/auth/channel-status", { auth: false }),
  me: () => request<User>("GET", "/api/v1/auth/me"),
  logout: () => request<void>("POST", "/api/v1/auth/logout"),
};
