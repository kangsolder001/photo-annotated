import { faCamera } from "@fortawesome/free-solid-svg-icons/faCamera";
import { faImages } from "@fortawesome/free-solid-svg-icons/faImages";
import { useRef } from "react";
import { Icon } from "../../components/Icon";
import { limits } from "../../config";

/** Dua tombol besar selebar layar: kamera belakang langsung, atau pilih dari galeri (boleh beberapa sekaligus). */
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
      <div className="photo-pick">
        <button type="button" className="btn btn-primary" disabled={full} onClick={() => camera.current?.click()}>
          <Icon icon={faCamera} /> <span>Ambil foto</span>
        </button>
        <button type="button" className="btn" disabled={full} onClick={() => gallery.current?.click()}>
          <Icon icon={faImages} /> <span>Pilih dari galeri</span>
        </button>
      </div>
      <p className="muted">{full ? `Maksimal ${limits.maxPhotos} foto per pendataan.` : `Bisa menambah ${remaining} foto lagi.`}</p>
      <input ref={camera} type="file" accept="image/*" capture="environment" hidden onChange={(e) => take(e.currentTarget)} />
      <input ref={gallery} type="file" accept="image/*" multiple hidden onChange={(e) => take(e.currentTarget)} />
    </div>
  );
}
