import { faHand, faVectorSquare } from "@fortawesome/free-solid-svg-icons";
import { useState } from "react";
import { Icon } from "../../components/Icon";
import { limits } from "../../config";
import type { Box } from "../../lib/annotation";
import { AnnotationList } from "./AnnotationList";
import { BoxEditor } from "./BoxEditor";

/** Satu foto: editor kotak + daftar catatan. Dipakai saat membuat dan mengedit laporan. */
export function PhotoAnnotator(props: {
  src: string;
  width: number;
  height: number;
  label: string;
  boxes: Box[];
  readOnly?: boolean;
  showErrors?: boolean;
  onChange?(boxes: Box[]): void;
  onImageError?(): void;
}) {
  const [selectedId, setSelectedId] = useState<string>();
  const [focusId, setFocusId] = useState<string>();
  const [drawMode, setDrawMode] = useState(true);
  const full = props.boxes.length >= limits.maxBoxesPerPhoto;

  return (
    <section className="card annotator" aria-label={props.label}>
      {!props.readOnly && (
        <div className="row annotator-toolbar">
          <button type="button" className={`btn btn-small${drawMode ? " btn-primary" : ""}`} aria-pressed={drawMode} onClick={() => setDrawMode((v) => !v)}>
            {drawMode ? <><Icon icon={faVectorSquare} /> Menggambar kotak</> : <><Icon icon={faHand} /> Mode gulir</>}
          </button>
          <span className="muted">
            {full ? `Maksimal ${limits.maxBoxesPerPhoto} kotak per foto.` : drawMode ? "Seret di foto untuk membuat kotak." : "Ketuk tombol untuk kembali menggambar."}
          </span>
        </div>
      )}
      <BoxEditor
        src={props.src}
        alt={props.label}
        width={props.width}
        height={props.height}
        boxes={props.boxes}
        selectedId={selectedId}
        drawMode={drawMode && !full}
        readOnly={props.readOnly}
        onChange={props.onChange}
        onSelect={setSelectedId}
        onCreate={setFocusId}
        onImageError={props.onImageError}
      />
      <AnnotationList
        boxes={props.boxes}
        selectedId={selectedId}
        focusId={focusId}
        readOnly={props.readOnly}
        showErrors={props.showErrors}
        onSelect={setSelectedId}
        onChange={(b) => props.onChange?.(b)}
      />
    </section>
  );
}
