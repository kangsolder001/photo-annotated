import { ProgressBar } from "../../components/ui";
import type { PhotoItem } from "./usePhotoUploads";

const STATUS_TEXT: Record<PhotoItem["status"], string> = {
  processing: "Menyiapkan…",
  uploading: "Mengunggah…",
  uploaded: "Siap",
  error: "Gagal",
};

/** Deretan thumbnail foto: status upload per foto dan pilihan foto yang sedang dianotasi. */
export function PhotoStrip(props: {
  items: PhotoItem[];
  selectedKey?: string;
  onSelect(key: string): void;
  onRetry(key: string): void;
  onRemove(key: string): void;
  boxCount?(key: string): number;
}) {
  return (
    <ul className="photo-strip" aria-label="Foto">
      {props.items.map((p, i) => {
        const selected = p.key === props.selectedKey;
        const n = props.boxCount?.(p.key) ?? 0;
        return (
          <li key={p.key} className={`photo-tile${selected ? " selected" : ""}`}>
            <button
              type="button"
              className="photo-thumb"
              onClick={() => props.onSelect(p.key)}
              aria-pressed={selected}
              aria-label={`Foto ${i + 1}: ${STATUS_TEXT[p.status]}${n ? `, ${n} kotak` : ""}`}
              disabled={!p.previewUrl}
            >
              {p.previewUrl ? <img src={p.previewUrl} alt="" /> : <span className="spinner" aria-hidden="true" />}
              <span className="photo-index">{i + 1}</span>
              {n > 0 && <span className="photo-badge">{n}</span>}
            </button>
            {(p.status === "uploading" || p.status === "processing") && <ProgressBar value={p.progress} label={`Upload foto ${i + 1}`} />}
            <div className={`photo-status status-${p.status}`}>{STATUS_TEXT[p.status]}</div>
            {p.error && <div className="field-error">{p.error}</div>}
            <div className="row">
              {p.status === "error" && (
                <button type="button" className="btn btn-small" onClick={() => props.onRetry(p.key)}>Ulangi</button>
              )}
              <button type="button" className="btn btn-small btn-danger" onClick={() => props.onRemove(p.key)} aria-label={`Hapus foto ${i + 1}`}>
                Hapus
              </button>
            </div>
          </li>
        );
      })}
    </ul>
  );
}
