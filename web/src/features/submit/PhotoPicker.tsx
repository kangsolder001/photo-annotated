import { useRef } from "react";

/** Dua tombol: kamera belakang langsung, atau pilih dari galeri (boleh beberapa sekaligus). */
export function PhotoPicker({ remaining, onFiles }: { remaining: number; onFiles(files: FileList): void }) {
  const camera = useRef<HTMLInputElement>(null);
  const gallery = useRef<HTMLInputElement>(null);
  const take = (el: HTMLInputElement | null) => {
    if (el?.files?.length) onFiles(el.files);
    if (el) el.value = ""; // pilih file yang sama lagi tetap memicu change
  };
  const full = remaining <= 0;
  return (
    <div className="stack">
      <div className="row">
        <button type="button" className="btn btn-primary" disabled={full} onClick={() => camera.current?.click()}>
          📷 Ambil foto
        </button>
        <button type="button" className="btn" disabled={full} onClick={() => gallery.current?.click()}>
          🖼️ Pilih dari galeri
        </button>
      </div>
      <p className="muted">{full ? "Maksimal 5 foto per laporan." : `Bisa menambah ${remaining} foto lagi.`}</p>
      <input ref={camera} type="file" accept="image/*" capture="environment" hidden onChange={(e) => take(e.currentTarget)} />
      <input ref={gallery} type="file" accept="image/*" multiple hidden onChange={(e) => take(e.currentTarget)} />
    </div>
  );
}
