import type { Box } from "../../../lib/annotation";
import { BoxEditor } from "../../annotate/BoxEditor";

export interface PhotoBox {
  labelId: number | null;
  x: number;
  y: number;
  w: number;
  h: number;
}

/**
 * Foto + overlay kotak bernomor berwarna label, memakai komponen render yang sama dengan tampilan
 * baca user (koordinat ternormalisasi, pas di semua ukuran layar).
 */
export function AnnotatedPhoto(props: {
  src: string;
  width: number;
  height: number;
  alt: string;
  boxes: PhotoBox[];
  colors: Map<number, string>;
  /** Kartu daftar: hanya gambar, tanpa tab stop; dimuat malas. */
  display?: boolean;
  onImageError?(): void;
}) {
  const boxes: Box[] = props.boxes.map((b, i) => ({ id: String(i), x: b.x, y: b.y, w: b.w, h: b.h, labelId: b.labelId }));
  return (
    <BoxEditor
      src={props.src}
      alt={props.alt}
      width={props.width}
      height={props.height}
      boxes={boxes}
      colorOf={(b) => (b.labelId != null ? props.colors.get(b.labelId) : null)}
      drawMode={false}
      readOnly
      display={props.display}
      lazy
      onImageError={props.onImageError}
    />
  );
}
