import { useId } from "react";
import type { AnswerValue, Answers, SurveyQuestion } from "../../api/types";
import { OTHER_TEXT_MAX, TEXTAREA_MAX, otherKey, todayJakarta } from "../../lib/survey";

/**
 * Satu pertanyaan, dirender menurut tipenya (spec-pendataan §4.1). `choice` tampil sebagai tombol besar
 * (radio asli yang digayakan, target sentuh ≥ 44 px); `multi` sebagai kotak centang besar.
 */
export function QuestionField(props: {
  q: SurveyQuestion;
  answers: Answers;
  error?: string | null;
  onChange(id: string, value: AnswerValue | undefined): void;
}) {
  const { q, answers, error } = props;
  const id = useId();
  const value = answers[q.id];
  const errId = `${id}-err`;
  const described = [q.help ? `${id}-help` : null, error ? errId : null].filter(Boolean).join(" ") || undefined;
  const set = (v: AnswerValue | undefined) => props.onChange(q.id, v);
  const label = <>{q.label}{q.required && <span className="req" aria-hidden="true"> *</span>}</>;
  const help = q.help ? <span id={`${id}-help`} className="hint">{q.help}</span> : null;
  const err = error ? <span id={errId} className="field-error">{error}</span> : null;

  if (q.type === "choice" || q.type === "multi") {
    const multi = q.type === "multi";
    const picked = multi ? (Array.isArray(value) ? value : []) : [];
    const otherPicked = multi && (q.options ?? []).some((o) => o.other && picked.includes(o.id));
    const other = answers[otherKey(q.id)];
    return (
      <fieldset className="question" aria-describedby={described} aria-invalid={!!error} aria-required={q.required}>
        <legend>{label}</legend>
        {help}
        <div className={`choice-group${(q.options?.length ?? 0) > 3 ? " choice-group-wide" : ""}`}>
          {(q.options ?? []).map((o) => (
            <label key={o.id} className="choice">
              <input
                type={multi ? "checkbox" : "radio"}
                name={`${id}-${q.id}`}
                value={o.id}
                checked={multi ? picked.includes(o.id) : value === o.id}
                onChange={(e) => {
                  if (!multi) return set(o.id);
                  const next = e.target.checked ? [...picked, o.id] : picked.filter((x) => x !== o.id);
                  set(next.length ? next : undefined);
                }}
              />
              <span>{o.label}</span>
            </label>
          ))}
        </div>
        {otherPicked && (
          <label className="field">
            Sebutkan lainnya
            <input
              value={typeof other === "string" ? other : ""}
              onChange={(e) => props.onChange(otherKey(q.id), e.target.value)}
              maxLength={OTHER_TEXT_MAX}
            />
          </label>
        )}
        {err}
      </fieldset>
    );
  }

  const common = { id, "aria-invalid": !!error, "aria-describedby": described, required: q.required };
  let input;
  switch (q.type) {
    case "date":
      input = <input {...common} type="date" max={todayJakarta()} value={typeof value === "string" ? value : ""} onChange={(e) => set(e.target.value || undefined)} />;
      break;
    case "int":
      input = (
        <input
          {...common}
          type="number"
          inputMode="numeric"
          step={1}
          min={q.min ?? undefined}
          max={q.max ?? undefined}
          value={typeof value === "number" ? String(value) : ""}
          onChange={(e) => set(e.target.value === "" ? undefined : Number(e.target.value))}
        />
      );
      break;
    case "textarea":
      input = <textarea {...common} rows={3} maxLength={TEXTAREA_MAX} value={typeof value === "string" ? value : ""} onChange={(e) => set(e.target.value)} />;
      break;
    default:
      input = <input {...common} maxLength={q.max ?? undefined} value={typeof value === "string" ? value : ""} onChange={(e) => set(e.target.value)} />;
  }
  return (
    <div className="field question">
      <label htmlFor={id}>{label}</label>
      {help}
      {input}
      {err}
    </div>
  );
}
