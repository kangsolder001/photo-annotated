import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../../api/endpoints";
import { Turnstile, type TurnstileHandle } from "../../components/Turnstile";
import { Banner } from "../../components/ui";
import { config } from "../../config";
import { toDTO, type Box } from "../../lib/annotation";
import { errorMessage, isCode } from "../../lib/errors";
import { history } from "../../lib/history";
import { readiness } from "../../lib/readiness";
import { PhotoAnnotator } from "../annotate/PhotoAnnotator";
import { currentOwner } from "../auth/owner";
import { PhotoPicker } from "./PhotoPicker";
import { PhotoStrip } from "./PhotoStrip";
import { usePhotoUploads } from "./usePhotoUploads";

/** Laporan baru: pilih/ambil foto → upload otomatis → tandai kotak + catatan → kirim. */
export function NewSubmissionPage() {
  const navigate = useNavigate();
  const turnstile = useRef<TurnstileHandle>(null);
  // Turnstile hanya untuk mode none; pada mode otp identitas dijamin sesi login
  const useTurnstile = config.authMode === "none";
  const tokens = useMemo(() => ({
    token: () => (useTurnstile ? turnstile.current?.token() ?? Promise.resolve(undefined) : Promise.resolve(undefined)),
    reset: () => turnstile.current?.reset(),
  }), [useTurnstile]);
  const uploads = usePhotoUploads(tokens);

  const [boxes, setBoxes] = useState<Record<string, Box[]>>({});
  const [selected, setSelected] = useState<string>();
  const [showErrors, setShowErrors] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string>();
  // satu id per laporan: kirim ulang (dobel ketuk, jaringan putus) tidak membuat duplikat
  const requestId = useRef(crypto.randomUUID());

  const items = uploads.items;
  const current = items.find((i) => i.key === selected && i.previewUrl) ?? items.find((i) => i.previewUrl);
  const ready = readiness(items.map((i) => ({ status: i.status, boxes: boxes[i.key] ?? [] })));
  const totalBoxes = items.reduce((n, i) => n + (boxes[i.key]?.length ?? 0), 0);
  const dirty = items.length > 0;

  // peringatan sebelum meninggalkan halaman dengan pekerjaan yang belum dikirim
  useEffect(() => {
    if (!dirty || submitting) return;
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty, submitting]);

  const removePhoto = (key: string) => {
    uploads.remove(key);
    setBoxes(({ [key]: _gone, ...rest }) => rest);
  };

  const submit = async () => {
    if (submitting) return;
    setShowErrors(true);
    setError(undefined);
    if (!ready.ok) return;
    setSubmitting(true);
    try {
      const body = {
        client_request_id: requestId.current,
        photos: items.map((i) => ({ photo_id: i.photoId!, annotations: (boxes[i.key] ?? []).map(toDTO) })),
      };
      let res;
      try {
        res = await api.submit(body, await tokens.token());
      } catch (e) {
        if (!isCode(e, "TURNSTILE_FAILED")) throw e;
        tokens.reset(); // token habis dipakai presign: minta yang baru sekali
        res = await api.submit(body, await tokens.token());
      }
      history.add({
        id: res.submission_id, createdAt: res.created_at, photos: items.length, boxes: totalBoxes,
        owner: currentOwner(), editToken: res.edit_token,
      });
      navigate(`/s/${res.submission_id}?baru=1`, { replace: true });
    } catch (e) {
      setError(errorMessage(e));
      setSubmitting(false);
    }
  };

  return (
    <>
      <h1>Laporan baru</h1>
      <p className="muted">Ambil atau pilih hingga 5 foto, lalu tandai bagian penting dengan kotak dan tulis catatannya. Foto diperkecil otomatis dan data lokasi (GPS) di foto dihapus.</p>

      <div className="card">
        <PhotoPicker remaining={uploads.maxPhotos - items.length} onFiles={(f) => uploads.add(f, uploads.maxPhotos - items.length)} />
      </div>

      {items.length > 0 && (
        <PhotoStrip
          items={items}
          selectedKey={current?.key}
          onSelect={setSelected}
          onRetry={uploads.retry}
          onRemove={removePhoto}
          boxCount={(k) => boxes[k]?.length ?? 0}
        />
      )}

      {current?.previewUrl && current.width && current.height && (
        <PhotoAnnotator
          key={current.key}
          src={current.previewUrl}
          width={current.width}
          height={current.height}
          label={`Foto ${items.indexOf(current) + 1}`}
          boxes={boxes[current.key] ?? []}
          showErrors={showErrors}
          onChange={(b) => setBoxes((m) => ({ ...m, [current.key]: b }))}
        />
      )}

      {error && <Banner kind="error">{error}</Banner>}
      {useTurnstile && <Turnstile ref={turnstile} />}

      {items.length > 0 && (
        <div className="sticky-actions">
          <div>
            <span className="summary" aria-live="polite">
              {items.length} foto · {totalBoxes} kotak{!ready.ok && ready.reason ? ` — ${ready.reason}` : ""}
            </span>
            <button type="button" className="btn btn-primary" onClick={submit} disabled={submitting || (showErrors && !ready.ok) || !items.every((i) => i.status === "uploaded")}>
              {submitting ? <><span className="spinner" aria-hidden="true" /> Mengirim…</> : "Kirim"}
            </button>
          </div>
        </div>
      )}
    </>
  );
}
