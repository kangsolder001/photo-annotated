import { faCircleCheck } from "@fortawesome/free-solid-svg-icons/faCircleCheck";
import { faCircleExclamation } from "@fortawesome/free-solid-svg-icons/faCircleExclamation";
import { useEffect, useRef, useState } from "react";
import type { AnswerValue, Answers, SurveyForm, SurveySection } from "../../api/types";
import { Icon } from "../../components/Icon";
import { Banner } from "../../components/ui";
import { NEW_DRAFT_ID, drafts } from "../../lib/draft";
import { session } from "../../lib/session";
import { CONSENT_ID, IDENTITY_SECTION, isDeclined, sectionErrors, todayJakarta, visibleSections } from "../../lib/survey";
import { currentOwner } from "../auth/owner";
import { QuestionField } from "./QuestionField";

/** Keadaan awal pendataan baru: draf (versi form sama) atau tanggal hari ini. */
function initialState(form: SurveyForm, owner: string): { answers: Answers; step: number; restored: boolean } {
  const d = drafts.load(NEW_DRAFT_ID, owner, 0);
  // draf yang hanya berisi tanggal bawaan tidak perlu ditawarkan sebagai "dipulihkan"
  if (d?.answers && d.form_version === form.version && Object.keys(d.answers).some((k) => k !== "a_date")) {
    return { answers: d.answers, step: d.step ?? 0, restored: true };
  }
  return { answers: { a_date: todayJakarta() }, step: 0, restored: false };
}

/**
 * Wizard pendataan (spec-pendataan §3.2): satu layar per bagian, Kembali/Lanjut, progres "Bagian x dari
 * y", lalu ringkasan. Menolak di bagian A melewati bagian lain. Jawaban tersimpan otomatis sebagai draf.
 */
export function SurveyWizard({ form }: { form: SurveyForm }) {
  const owner = currentOwner();
  const user = session.user();
  const [initial] = useState(() => initialState(form, owner));
  const [answers, setAnswers] = useState<Answers>(initial.answers);
  const [step, setStep] = useState(initial.step);
  const [restored, setRestored] = useState(initial.restored);
  /** Bagian yang sudah ditinggalkan lewat Lanjut / dibuka dari ringkasan: error-nya ditampilkan. */
  const [tried, setTried] = useState<Set<string>>(() => new Set());
  const heading = useRef<HTMLHeadingElement>(null);
  const firstRender = useRef(true);

  const sections = visibleSections(form, answers);
  const declined = isDeclined(answers);
  const total = sections.length;
  const at = Math.min(step, total); // total = ringkasan
  const section: SurveySection | undefined = sections[at];

  useEffect(() => {
    drafts.save(NEW_DRAFT_ID, owner, { version: 0, savedAt: new Date().toISOString(), photos: [], answers, form_version: form.version, step: at });
  }, [answers, at, owner, form.version]);

  // pindah langkah: fokus ke judul (pembaca layar + layar kembali ke atas)
  useEffect(() => {
    if (firstRender.current) {
      firstRender.current = false;
      return;
    }
    heading.current?.focus();
  }, [at]);

  const change = (id: string, v: AnswerValue | undefined) =>
    setAnswers(({ [id]: _old, ...rest }) => (v === undefined ? rest : { ...rest, [id]: v }));

  const go = (i: number) => {
    if (section) setTried((t) => new Set(t).add(section.id));
    setStep(Math.max(0, Math.min(i, total)));
  };
  const openFromSummary = (i: number) => {
    setTried((t) => new Set(t).add(sections[i]!.id));
    setStep(i);
  };

  const startOver = () => {
    drafts.clear(NEW_DRAFT_ID, owner);
    setAnswers({ a_date: todayJakarta() });
    setStep(0);
    setTried(new Set());
    setRestored(false);
  };

  const errors = section ? sectionErrors(section, answers) : {};
  const showErrors = section ? tried.has(section.id) : false;

  return (
    <>
      <p className="muted survey-progress" aria-live="polite">
        {section ? `Bagian ${at + 1} dari ${total}` : "Ringkasan"}
      </p>
      {restored && (
        <Banner kind="info">
          Isian yang belum dikirim dipulihkan.{" "}
          <button type="button" className="btn btn-small" onClick={startOver}>Mulai dari awal</button>
        </Banner>
      )}

      {section ? (
        <section className="card stack" aria-labelledby="survey-step-title">
          <h2 id="survey-step-title" ref={heading} tabIndex={-1}>{section.title}</h2>
          {section.help && <p className="muted">{section.help}</p>}
          {section.id === IDENTITY_SECTION && (
            <dl className="identity-card">
              <div><dt>Nama mahasiswa</dt><dd>{user?.name ?? "-"}</dd></div>
              <div><dt>NIM</dt><dd>{user?.nim ?? "-"}</dd></div>
              <div><dt>Kelompok</dt><dd>{user?.group?.name ?? "-"}</dd></div>
            </dl>
          )}
          {section.questions.map((q) => (
            <div key={q.id} className="stack">
              {q.id === CONSENT_ID && <blockquote className="consent-text">{form.consent_text}</blockquote>}
              <QuestionField q={q} answers={answers} error={showErrors ? errors[q.id] : null} onChange={change} />
            </div>
          ))}
          {section.id === IDENTITY_SECTION && declined && (
            <Banner kind="info">Responden tidak bersedia: hanya bagian A yang disimpan. Bagian lain dilewati.</Banner>
          )}
        </section>
      ) : (
        <section className="card stack" aria-labelledby="survey-step-title">
          <h2 id="survey-step-title" ref={heading} tabIndex={-1}>Ringkasan</h2>
          <ul className="survey-summary">
            {sections.map((s, i) => {
              const bad = Object.keys(sectionErrors(s, answers)).length > 0;
              return (
                <li key={s.id}>
                  <button type="button" className={`btn btn-block survey-summary-item${bad ? " incomplete" : ""}`} onClick={() => openFromSummary(i)}>
                    <Icon icon={bad ? faCircleExclamation : faCircleCheck} />
                    <span>{s.title}</span>
                    <span className="survey-summary-status">{bad ? "Belum lengkap" : "Lengkap"}</span>
                  </button>
                </li>
              );
            })}
          </ul>
        </section>
      )}

      <div className="sticky-actions">
        <div>
          <button type="button" className="btn" onClick={() => go(at - 1)} disabled={at === 0}>Kembali</button>
          <span className="summary" />
          {at < total && (
            <button type="button" className="btn btn-primary" onClick={() => go(at + 1)}>Lanjut</button>
          )}
        </div>
      </div>
    </>
  );
}
