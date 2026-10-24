import { useMemo, useRef, useState } from "react";
import { Turnstile, type TurnstileHandle } from "../../components/Turnstile";
import { PhotoPicker } from "./PhotoPicker";
import { PhotoStrip } from "./PhotoStrip";
import { usePhotoUploads } from "./usePhotoUploads";

/** Laporan baru: pilih/ambil foto → upload otomatis dengan progres. */
export function NewSubmissionPage() {
  const turnstile = useRef<TurnstileHandle>(null);
  const tokens = useMemo(() => ({
    token: () => turnstile.current?.token() ?? Promise.resolve(undefined),
    reset: () => turnstile.current?.reset(),
  }), []);
  const uploads = usePhotoUploads(tokens);
  const [selected, setSelected] = useState<string>();
  const remaining = uploads.maxPhotos - uploads.items.length;

  return (
    <>
      <h1>Laporan baru</h1>
      <p className="muted">Ambil atau pilih hingga 5 foto. Foto diperkecil otomatis dan lokasi (GPS) di foto dihapus sebelum diunggah.</p>
      <div className="card">
        <PhotoPicker remaining={remaining} onFiles={(f) => uploads.add(f, remaining)} />
      </div>
      {uploads.items.length > 0 && (
        <PhotoStrip
          items={uploads.items}
          selectedKey={selected}
          onSelect={setSelected}
          onRetry={uploads.retry}
          onRemove={uploads.remove}
        />
      )}
      <Turnstile ref={turnstile} />
    </>
  );
}
