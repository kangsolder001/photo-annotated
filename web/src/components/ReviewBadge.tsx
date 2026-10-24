import { faClock } from "@fortawesome/free-regular-svg-icons/faClock";
import { faCheck } from "@fortawesome/free-solid-svg-icons/faCheck";
import { faXmark } from "@fortawesome/free-solid-svg-icons/faXmark";
import { Icon } from "./Icon";

export type ReviewStatusValue = "pending" | "accepted" | "rejected";

const TEXT: Record<ReviewStatusValue, string> = { pending: "Menunggu review", accepted: "Diterima", rejected: "Ditolak" };
const KIND: Record<ReviewStatusValue, string> = { pending: "warn", accepted: "ok", rejected: "danger" };
const ICON = { pending: faClock, accepted: faCheck, rejected: faXmark };

export const reviewText = (s: ReviewStatusValue) => TEXT[s];

/** Lencana status review (ikon + teks, tidak hanya warna). Dipakai dashboard dan riwayat user. */
export function ReviewBadge({ status }: { status: ReviewStatusValue }) {
  return (
    <span className={`badge badge-${KIND[status]}`}>
      <Icon icon={ICON[status]} /> {TEXT[status]}
    </span>
  );
}
