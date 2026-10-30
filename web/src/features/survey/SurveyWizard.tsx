import { faCircleCheck } from "@fortawesome/free-solid-svg-icons/faCircleCheck";
import { faCircleExclamation } from "@fortawesome/free-solid-svg-icons/faCircleExclamation";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../../api/endpoints";
import type { AnswerValue, Answers, SubmissionView, SurveyForm, SurveySection } from "../../api/types";
import { Icon } from "../../components/Icon";
import { Banner } from "../../components/ui";
import { clearUnavailable, fromDraft, fromView, toDraft, toDTO, type Box } from "../../lib/annotation";
import { NEW_DRAFT_ID, drafts, sameAsSaved } from "../../lib/draft";
import { errorMessage, isCode } from "../../lib/errors";
import { history, surveySummary } from "../../lib/history";
import { uuid } from "../../lib/id";
import { optionsForPhoto } from "../../lib/labels";
import { readiness } from "../../lib/readiness";
import { session } from "../../lib/session";
import {
  CONSENT_ID,
  IDENTITY_SECTION,
  cleanAnswers,
  isDeclined,
  sectionErrors,
  todayJakarta,
  visibleSections,
} from "../../lib/survey";
import { useLabels } from "../annotate/useLabels";
import { currentOwner } from "../auth/owner";
import { usePhotoUploads, type PhotoItem } from "../submit/usePhotoUploads";
import { PhotoStep } from "./PhotoStep";
import { QuestionField } from "./QuestionField";

/** Bagian tempat langkah foto ditampilkan (di atas pertanyaan R3). */
export const PHOTO_SECTION = "R3";

interface Initial {
  answers: Answers;
  step: number;
  /** Foto tersimpan yang dipertahankan (edit) dan kotaknya. */
  kept: string[];
  boxes: Record<string, Box[]>;
  restored: boolean;
}

/** Keadaan awal: draf (versi form/laporan sama) atau isi tersimpan / tanggal hari ini. */
function initialState(form: SurveyForm, owner: string, view?: SubmissionView): Initial {
  if (!view) {
    const d = drafts.load(NEW_DRAFT_ID, owner, 0);
    // draf yang hanya berisi tanggal bawaan tidak perlu ditawarkan sebagai "dipulihkan"
    if (d?.answers && d.form_version === form.version && Object.keys(d.answers).some((k) => k !== "a_date")) {
      return { answers: d.answers, step: d.step ?? 0, kept: [], boxes: {}, restored: true };
    }
    return { answers: { a_date: todayJakarta() }, step: 0, kept: [], boxes: {}, restored: false };
  }
  const saved: Initial = {
    answers: view.answers ?? {},
    step: 0,
    kept: view.photos.map((p) => p.photo_id),
    boxes: Object.fromEntries(view.photos.map((p) => [p.photo_id, p.annotations.map(fromView)])),
    restored: false,
  };
  const d = drafts.load(view.id, owner, view.version);
  if (!d) return saved;
  const sameAnswers = JSON.stringify(d.answers ?? {}) === JSON.stringify(saved.answers);
  if (sameAnswers && sameAsSaved(d, view.photos)) return saved;
  const present = new Set(saved.kept);
  const ds = d.photos.filter((p) => present.has(p.photoId));
  return {
    answers: d.answers && d.form_version === form.version ? d.answers : saved.answers,
    step: 0,
    kept: ds.map((p) => p.photoId),
    boxes: { ...saved.boxes, ...Object.fromEntries(ds.map((p) => [p.photoId, p.annotations.map(fromDraft)])) },
    restored: true,
  };
}

/**
 * Wizard pendataan (spec-pendataan §3.2): satu layar per bagian, Kembali/Lanjut, progres "Bagian x dari
 * y", langkah foto di bagian R3, lalu ringkasan + Kirim. Menolak di bagian A melewati bagian lain dan
 * langsung bisa dikirim. Jawaban tersimpan otomatis sebagai draf. Dengan `view`, wizard yang sama
 * dipakai untuk edit 24 jam (PUT, keadaan akhir).
 */
export function SurveyWizard(props: {
  form: SurveyForm;
  /** Edit: laporan tersimpan, URL foto tersimpan, dan callback. */
  view?: SubmissionView;
  urls?: Record<string, string>;
  refreshUrl?(photoId: string): void;
  /** Mode none: token edit dari riwayat perangkat. */
  editToken?: string;
  onSaved?(): void;
  onCancel?(): void;
  onReload?(): void;
}) {
  const { form, view } = props;
  const owner = currentOwner();
  const navigate = useNavigate();
  const user = session.user();
  const uploads = usePhotoUploads();
  const { labels, error: labelsError, reload: reloadLabels } = useLabels();

  const [initial] = useState(() => initialState(form, owner, view));
  const [answers, setAnswers] = useState<Answers>(initial.answers);
  const [step, setStep] = useState(initial.step);
  const [kept, setKept] = useState<string[]>(initial.kept);
  const [boxes, setBoxes] = useState<Record<string, Box[]>>(initial.boxes);
  const [restored, setRestored] = useState(initial.restored);
  const [selected, setSelected] = useState<string>();
  /** Bagian yang sudah ditinggalkan lewat Lanjut / dibuka dari ringkasan: error-nya ditampilkan. */
  const [tried, setTried] = useState<Set<string>>(() => new Set());
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<{ text: string; reload?: boolean }>();
  // satu id per pendataan: kirim ulang (dobel ketuk, jaringan putus) tidak membuat duplikat
  const requestId = useRef(uuid());
  const heading = useRef<HTMLHeadingElement>(null);
  const firstRender = useRef(true);

  const sections = visibleSections(form, answers);
  const declined = isDeclined(answers);
  const total = sections.length;
  const at = declined ? 0 : Math.min(step, total); // total = ringkasan
  const section: SurveySection | undefined = sections[at];
  const draftId = view?.id ?? NEW_DRAFT_ID;

  // foto: tersimpan (edit) + baru
  const byId = new Map((view?.photos ?? []).map((p) => [p.photo_id, p]));
  const existing: PhotoItem[] = kept.map((id) => ({
    key: id, status: "uploaded", progress: 1, photoId: id,
    previewUrl: props.urls?.[id], width: byId.get(id)?.width, height: byId.get(id)?.height,
  }));
  const items = [...existing, ...uploads.items];
  const photos = readiness(items.map((i) => ({ status: i.status, boxes: boxes[i.key] ?? [] })));
  const optionsFor = (key: string) => optionsForPhoto(labels ?? [], byId.get(key)?.annotations);

  const problemsOf = (s: SurveySection) => {
    const errs = sectionErrors(s, answers);
    return Object.keys(errs).length > 0 || (s.id === PHOTO_SECTION && !photos.ok);
  };
  const incomplete = sections.filter(problemsOf);
  const canSend = incomplete.length === 0 && (declined || photos.ok);

  // label yang sudah tidak tersedia (diarsipkan admin) dikosongkan dan ditandai
  useEffect(() => {
    if (!labels) return;
    setBoxes((m) => {
      let changed = false;
      const next = Object.fromEntries(Object.entries(m).map(([k, b]) => {
        const ok = new Set(optionsForPhoto(labels, byId.get(k)?.annotations).map((l) => l.id));
        const nb = clearUnavailable(b, (id) => ok.has(id));
        if (nb !== b) changed = true;
        return [k, nb];
      }));
      return changed ? next : m;
    });
    // byId diturunkan dari view yang tetap selama komponen ini hidup
  }, [labels]);

  // draf: jawaban + langkah (+ foto tersimpan dan kotaknya saat edit; foto baru tidak bisa dipulihkan)
  useEffect(() => {
    if (sending) return;
    drafts.save(draftId, owner, {
      version: view?.version ?? 0,
      savedAt: new Date().toISOString(),
      photos: kept.map((id) => ({ photoId: id, annotations: (boxes[id] ?? []).map(toDraft) })),
      answers,
      form_version: form.version,
      step: at,
    });
  }, [answers, at, kept, boxes, owner, draftId, view?.version, form.version, sending]);

  // peringatan sebelum meninggalkan halaman dengan foto yang belum terkirim
  useEffect(() => {
    if (uploads.items.length === 0 || sending) return;
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [uploads.items.length, sending]);

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

  const markTried = (...ids: string[]) => setTried((t) => new Set([...t, ...ids]));
  const go = (i: number) => {
    if (section) markTried(section.id);
    setStep(Math.max(0, Math.min(i, total)));
  };
  const openSection = (i: number) => {
    markTried(sections[i]!.id);
    setStep(i);
  };

  const startOver = () => {
    if (view) {
      drafts.clear(view.id, owner);
      const saved = initialState(form, owner, view);
      setAnswers(saved.answers);
      setKept(saved.kept);
      setBoxes(saved.boxes);
    } else {
      drafts.clear(NEW_DRAFT_ID, owner);
      setAnswers({ a_date: todayJakarta() });
    }
    setStep(0);
    setTried(new Set());
    setRestored(false);
  };

  const removePhoto = (key: string) => {
    if (byId.has(key)) setKept((k) => k.filter((x) => x !== key));
    else uploads.remove(key);
    setBoxes(({ [key]: _gone, ...rest }) => rest);
  };

  const send = async () => {
    if (sending) return;
    setError(undefined);
    markTried(...sections.map((s) => s.id));
    if (!canSend) return;
    setSending(true);
    const body = {
      form_version: form.version,
      answers: cleanAnswers(form, answers),
      photos: declined ? [] : items.map((i) => ({ photo_id: i.photoId!, annotations: (boxes[i.key] ?? []).map(toDTO) })),
    };
    const counts = { photos: body.photos.length, boxes: body.photos.reduce((n, p) => n + p.annotations.length, 0), survey: surveySummary(body.answers) };
    try {
      if (view) {
        await api.editSubmission(view.id, { version: view.version, ...body }, props.editToken);
        drafts.clear(view.id, owner);
        history.update(view.id, counts);
        props.onSaved?.();
        return;
      }
      const res = await api.submit({ client_request_id: requestId.current, ...body });
      drafts.clear(NEW_DRAFT_ID, owner);
      history.add({ id: res.submission_id, createdAt: res.created_at, owner, ...counts });
      navigate(`/s/${res.submission_id}?baru=1`, { replace: true });
    } catch (e) {
      setSending(false);
      if (isCode(e, "VERSION_CONFLICT", "EDIT_WINDOW_CLOSED") && view) setError({ text: errorMessage(e), reload: true });
      else if (isCode(e, "NOT_FOUND") && view) setError({ text: "Pendataan ini sudah dihapus.", reload: true });
      else setError({ text: errorMessage(e) });
      if (isCode(e, "INVALID_LABEL", "LABEL_ARCHIVED")) void reloadLabels();
      // jawaban ditolak server ("answers.b7: …"): buka bagian pertanyaan itu
      const qid = isCode(e, "INVALID_ANSWER") ? /answers\.(\w+)/.exec(e.message)?.[1] : undefined;
      const idx = qid ? sections.findIndex((s) => s.questions.some((q) => q.id === qid || `${q.id}_other` === qid)) : -1;
      if (idx >= 0) setStep(idx);
    }
  };

  const errors = section ? sectionErrors(section, answers) : {};
  const showErrors = section ? tried.has(section.id) : false;
  const sendLabel = view ? "Simpan" : "Kirim";

  return (
    <>
      <p className="muted survey-progress" aria-live="polite">
        {section ? `Bagian ${at + 1} dari ${total}` : "Ringkasan"}
      </p>
      {restored && (
        <Banner kind="info">
          {view ? "Perubahan yang belum disimpan dipulihkan." : "Isian yang belum dikirim dipulihkan."}{" "}
          <button type="button" className="btn btn-small" onClick={startOver}>{view ? "Buang perubahan" : "Mulai dari awal"}</button>
        </Banner>
      )}

      {section ? (
        <section className="card stack" aria-labelledby="survey-step-title">
          <h2 id="survey-step-title" ref={heading} tabIndex={-1}>{section.title}</h2>
          {section.help && section.id !== PHOTO_SECTION && <p className="muted">{section.help}</p>}
          {section.id === IDENTITY_SECTION && (
            <dl className="identity-card">
              <div><dt>Nama mahasiswa</dt><dd>{user?.name ?? "-"}</dd></div>
              <div><dt>NIM</dt><dd>{user?.nim ?? "-"}</dd></div>
              <div><dt>Kelompok</dt><dd>{user?.group?.name ?? "-"}</dd></div>
            </dl>
          )}
          {section.id === PHOTO_SECTION && (
            <PhotoStep
              protocol={section.help}
              items={items}
              selectedKey={selected}
              onSelect={setSelected}
              onAdd={(f, cap) => uploads.add(f, cap)}
              onRetry={uploads.retry}
              onRemove={removePhoto}
              boxes={boxes}
              onBoxes={(k, b) => setBoxes((m) => ({ ...m, [k]: b }))}
              labels={labels}
              labelsError={labelsError}
              reloadLabels={() => void reloadLabels()}
              optionsFor={optionsFor}
              showErrors={showErrors}
              problem={photos.reason}
              onImageError={(k) => byId.has(k) && props.refreshUrl?.(k)}
            />
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
          <p className="muted">
            {items.length} foto · {items.reduce((n, i) => n + (boxes[i.key]?.length ?? 0), 0)} kotak. Ketuk bagian untuk memeriksa atau memperbaiki.
          </p>
          <ul className="survey-summary">
            {sections.map((s, i) => {
              const bad = problemsOf(s);
              return (
                <li key={s.id}>
                  <button type="button" className={`btn btn-block survey-summary-item${bad ? " incomplete" : ""}`} onClick={() => openSection(i)}>
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

      {error && (
        <Banner kind="error">
          {error.text}{" "}
          {error.reload && (
            <button type="button" className="btn btn-small" onClick={() => { drafts.clear(draftId, owner); props.onReload?.(); }}>
              Muat ulang
            </button>
          )}
        </Banner>
      )}
      {view && (
        <button type="button" className="btn" onClick={() => { drafts.clear(view.id, owner); props.onCancel?.(); }} disabled={sending}>
          Batal edit
        </button>
      )}

      <div className="sticky-actions">
        <div>
          <button type="button" className="btn" onClick={() => go(at - 1)} disabled={at === 0 || sending}>Kembali</button>
          <span className="summary" aria-live="polite">
            {!section || declined ? (canSend ? "" : declined ? "Bagian A belum lengkap." : `${incomplete.length} bagian belum lengkap.`) : ""}
          </span>
          {section && !declined ? (
            <button type="button" className="btn btn-primary" onClick={() => go(at + 1)}>Lanjut</button>
          ) : (
            <button type="button" className="btn btn-primary" onClick={() => void send()} disabled={sending}>
              {sending ? <><span className="spinner" aria-hidden="true" /> {view ? "Menyimpan…" : "Mengirim…"}</> : sendLabel}
            </button>
          )}
        </div>
      </div>
    </>
  );
}
