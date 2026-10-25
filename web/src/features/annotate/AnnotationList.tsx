import { faTrashCan } from "@fortawesome/free-solid-svg-icons/faTrashCan";
import { useEffect, useRef } from "react";
import { Icon } from "../../components/Icon";
import { boxProblems, PROBLEM_TEXT, type Box } from "../../lib/annotation";
import { textOn } from "../../lib/color";
import { UNLABELED_COLOR, type LabelOption } from "../../lib/labels";

/**
 * Daftar kotak bernomor; nomor dan warna sama dengan kotak di foto. Saat mengedit, tiap kotak memilih
 * satu label lewat dropdown native (ramah HP); tidak ada isian teks bebas (spec-labels-admin §7.1).
 */
export function AnnotationList(props: {
  boxes: Box[];
  labels: LabelOption[];
  selectedId?: string;
  focusId?: string;
  readOnly?: boolean;
  showErrors?: boolean;
  onSelect(id: string): void;
  onChange(boxes: Box[]): void;
}) {
  const refs = useRef(new Map<string, HTMLSelectElement>());
  const byId = new Map(props.labels.map((l) => [l.id, l]));

  useEffect(() => {
    if (props.focusId) refs.current.get(props.focusId)?.focus();
  }, [props.focusId]);

  if (props.boxes.length === 0) {
    return <p className="muted">{props.readOnly ? "Tidak ada kotak pada foto ini." : "Belum ada kotak. Seret di atas foto untuk menandai bagian yang ingin dilabeli."}</p>;
  }

  const setLabel = (id: string, labelId: number | null) =>
    props.onChange(props.boxes.map((x) => (x.id === id ? { ...x, labelId, flagged: labelId == null ? x.flagged : false } : x)));

  return (
    <ol className="annotation-list">
      {props.boxes.map((b, i) => {
        const problems = boxProblems(b);
        const sel = b.id === props.selectedId;
        const errId = `ann-err-${b.id}`;
        const descId = `ann-desc-${b.id}`;
        const label = b.labelId != null ? byId.get(b.labelId) : undefined;
        const color = label?.color ?? UNLABELED_COLOR;
        const showProblems = !props.readOnly && (props.showErrors || !!b.flagged) && problems.length > 0;
        return (
          <li key={b.id} className={sel ? "selected" : undefined} onClick={() => props.onSelect(b.id)}>
            <span className="ann-num" style={{ background: color, color: textOn(color) }} aria-hidden="true">{i + 1}</span>
            <div className="ann-body">
              {props.readOnly ? (
                label ? (
                  <span className="label-chip"><span className="label-dot" style={{ background: label.color }} aria-hidden="true" />{label.name}</span>
                ) : b.legacyText ? null : (
                  <span className="muted">Tanpa label</span>
                )
              ) : (
                <>
                  <label className="visually-hidden" htmlFor={`ann-${b.id}`}>Label kotak {i + 1}</label>
                  <div className="label-row">
                    <span className={`label-dot${label ? "" : " label-dot-empty"}`} style={label ? { background: label.color } : undefined} aria-hidden="true" />
                    <select
                      id={`ann-${b.id}`}
                      ref={(el) => {
                        if (el) refs.current.set(b.id, el);
                        else refs.current.delete(b.id);
                      }}
                      value={b.labelId ?? ""}
                      aria-invalid={showProblems}
                      aria-describedby={[label?.description ? descId : "", showProblems ? errId : ""].filter(Boolean).join(" ") || undefined}
                      onFocus={() => props.onSelect(b.id)}
                      onChange={(e) => setLabel(b.id, e.target.value === "" ? null : Number(e.target.value))}
                    >
                      <option value="">Pilih label…</option>
                      {props.labels.map((l) => (
                        <option key={l.id} value={l.id}>{l.archived ? `${l.name} (diarsipkan)` : l.name}</option>
                      ))}
                    </select>
                  </div>
                  {label?.description && <span id={descId} className="hint">{label.description}</span>}
                  {showProblems && (
                    <span id={errId} className="field-error">{problems.map((p) => PROBLEM_TEXT[p]).join(" ")}</span>
                  )}
                </>
              )}
              {b.legacyText && (
                <p className="ann-legacy">
                  <span className="legacy-tag">catatan lama</span> <span className="ann-text">{b.legacyText}</span>
                </p>
              )}
            </div>
            {!props.readOnly && (
              <button
                type="button"
                className="btn btn-small btn-danger"
                aria-label={`Hapus kotak ${i + 1}`}
                onClick={(e) => {
                  e.stopPropagation();
                  props.onChange(props.boxes.filter((x) => x.id !== b.id));
                }}
              >
                <Icon icon={faTrashCan} /> Hapus
              </button>
            )}
          </li>
        );
      })}
    </ol>
  );
}
