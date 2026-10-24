import { useEffect, useRef } from "react";
import { limits } from "../../config";
import { boxProblems, PROBLEM_TEXT, type Box } from "../../lib/annotation";

/** Daftar kotak bernomor dengan isian catatan; urutan dan nomor sama dengan label di foto. */
export function AnnotationList(props: {
  boxes: Box[];
  selectedId?: string;
  focusId?: string;
  readOnly?: boolean;
  showErrors?: boolean;
  onSelect(id: string): void;
  onChange(boxes: Box[]): void;
}) {
  const refs = useRef(new Map<string, HTMLTextAreaElement>());

  useEffect(() => {
    if (props.focusId) refs.current.get(props.focusId)?.focus();
  }, [props.focusId]);

  if (props.boxes.length === 0) {
    return <p className="muted">{props.readOnly ? "Tidak ada kotak pada foto ini." : "Belum ada kotak. Seret di atas foto untuk menandai bagian yang ingin dijelaskan."}</p>;
  }

  return (
    <ol className="annotation-list">
      {props.boxes.map((b, i) => {
        const problems = boxProblems(b);
        const sel = b.id === props.selectedId;
        const errId = `ann-err-${b.id}`;
        return (
          <li key={b.id} className={sel ? "selected" : undefined} onClick={() => props.onSelect(b.id)}>
            <span className="ann-num" aria-hidden="true">{i + 1}</span>
            <div className="ann-body">
              {props.readOnly ? (
                <p className="ann-text">{b.text}</p>
              ) : (
                <>
                  <label className="visually-hidden" htmlFor={`ann-${b.id}`}>Catatan kotak {i + 1}</label>
                  <textarea
                    id={`ann-${b.id}`}
                    ref={(el) => {
                      if (el) refs.current.set(b.id, el);
                      else refs.current.delete(b.id);
                    }}
                    value={b.text}
                    rows={2}
                    maxLength={limits.maxTextLength * 2}
                    placeholder="Tulis catatan untuk kotak ini…"
                    aria-invalid={props.showErrors && problems.length > 0}
                    aria-describedby={problems.length ? errId : undefined}
                    onFocus={() => props.onSelect(b.id)}
                    onChange={(e) => props.onChange(props.boxes.map((x) => (x.id === b.id ? { ...x, text: e.target.value } : x)))}
                  />
                  {props.showErrors && problems.length > 0 && (
                    <span id={errId} className="field-error">{problems.map((p) => PROBLEM_TEXT[p]).join(" ")}</span>
                  )}
                </>
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
                Hapus
              </button>
            )}
          </li>
        );
      })}
    </ol>
  );
}
