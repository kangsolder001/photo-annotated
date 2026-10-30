import type { Label } from "../../api/types";
import { limits } from "../../config";
import type { Box } from "../../lib/annotation";
import type { LabelOption } from "../../lib/labels";
import { LabelsNotice } from "../annotate/LabelsNotice";
import { PhotoAnnotator } from "../annotate/PhotoAnnotator";
import { PhotoPicker } from "../submit/PhotoPicker";
import { PhotoStrip } from "../submit/PhotoStrip";
import type { PhotoItem } from "../submit/usePhotoUploads";

/**
 * Langkah foto wadah (R3): protokol foto di atas tombol kamera, maks. 3 foto (foto pertama = utama),
 * lalu kotak berlabel per foto memakai editor anotasi yang sudah ada.
 */
export function PhotoStep(props: {
  protocol?: string | null;
  items: PhotoItem[];
  selectedKey?: string;
  onSelect(key: string): void;
  onAdd(files: FileList, capacity: number): void;
  onRetry(key: string): void;
  onRemove(key: string): void;
  boxes: Record<string, Box[]>;
  onBoxes(key: string, boxes: Box[]): void;
  labels?: Label[];
  labelsError?: string;
  reloadLabels(): void;
  optionsFor(key: string): LabelOption[];
  showErrors: boolean;
  /** Error ringkas (mis. "Tambahkan minimal 1 foto.") saat langkah ini sudah ditinggalkan. */
  problem?: string;
  onImageError(key: string): void;
}) {
  const { items } = props;
  const current = items.find((i) => i.key === props.selectedKey && i.previewUrl) ?? items.find((i) => i.previewUrl);
  const remaining = limits.maxPhotos - items.length;
  return (
    <div className="stack">
      <h3>Foto wadah *</h3>
      {props.protocol && <p className="photo-protocol">{props.protocol}</p>}
      <LabelsNotice labels={props.labels} error={props.labelsError} onRetry={props.reloadLabels} />
      <PhotoPicker remaining={remaining} onFiles={(f) => props.onAdd(f, remaining)} />
      {props.showErrors && props.problem && <span className="field-error">{props.problem}</span>}
      {items.length > 0 && (
        <PhotoStrip items={items} selectedKey={current?.key} onSelect={props.onSelect} onRetry={props.onRetry} onRemove={props.onRemove}
          boxCount={(k) => props.boxes[k]?.length ?? 0} />
      )}
      {current?.previewUrl && current.width && current.height && (
        <PhotoAnnotator
          key={current.key}
          src={current.previewUrl}
          width={current.width}
          height={current.height}
          label={`Foto ${items.indexOf(current) + 1}${items.indexOf(current) === 0 ? " (utama)" : ""}`}
          boxes={props.boxes[current.key] ?? []}
          labels={props.optionsFor(current.key)}
          showErrors={props.showErrors}
          onChange={(b) => props.onBoxes(current.key, b)}
          onImageError={() => props.onImageError(current.key)}
        />
      )}
    </div>
  );
}
