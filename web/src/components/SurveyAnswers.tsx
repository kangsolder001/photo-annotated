import type { Answers, SurveyForm } from "../api/types";
import { answerText, visibleSections } from "../lib/survey";

/**
 * Jawaban pendataan per bagian, dengan teks opsi (bukan id). Dipakai halaman pendataan user dan detail
 * admin. Bagian yang tidak berlaku (menolak) tidak ditampilkan; jawaban kosong tampil "-".
 */
export function SurveyAnswers({ form, answers }: { form: SurveyForm; answers: Answers }) {
  return (
    <div className="survey-answers">
      {visibleSections(form, answers).map((s) => (
        <section key={s.id} className="card" aria-label={s.title}>
          <h2>{s.title}</h2>
          <dl>
            {s.questions.map((q) => (
              <div key={q.id}>
                <dt>{q.label}</dt>
                <dd>{answerText(q, answers[q.id], answers) || "-"}</dd>
              </div>
            ))}
          </dl>
        </section>
      ))}
    </div>
  );
}
