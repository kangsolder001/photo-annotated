import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../../api/endpoints";
import type { SubmissionView } from "../../api/types";
import { Turnstile, type TurnstileHandle } from "../../components/Turnstile";
import { Banner } from "../../components/ui";
import { config, limits } from "../../config";
import { clearUnavailable, fromDraft, fromView, toDraft, toDTO, type Box } from "../../lib/annotation";
import { drafts, sameAsSaved } from "../../lib/draft";
import { errorMessage, isCode } from "../../lib/errors";
import { history } from "../../lib/history";
import { readiness } from "../../lib/readiness";
import { optionsForPhoto, type LabelOption } from "../../lib/labels";
import { LabelsNotice } from "../annotate/LabelsNotice";
import { PhotoAnnotator } from "../annotate/PhotoAnnotator";
import { useLabels } from "../annotate/useLabels";
import { currentOwner } from "../auth/owner";
import { PhotoPicker } from "../submit/PhotoPicker";
import { PhotoStrip } from "../submit/PhotoStrip";
import { usePhotoUploads, type PhotoItem } from "../submit/usePhotoUploads";

/**
 * Mengedit laporan: ubah kotak/label, hapus foto, tambah foto baru. Isi request adalah keadaan
 * akhir (spec §5.4). Perubahan disimpan sebagai draf di perangkat sampai berhasil disimpan.
 */
export function EditSubmission(props: {
  view: SubmissionView;
  urls: Record<string, string>;
  /** Minta URL baru untuk foto tersimpan (URL lihat berumur 5 menit). */
  refreshUrl(photoId: string): void;
  editToken?: string;
  onSaved(): void;
  onCancel(): void;
  /** Muat ulang dari server (setelah konflik versi atau jendela edit habis). */
  onReload(): void;
}) {
  const { view } = props;
  const owner = currentOwner();
  const turnstile = useRef<TurnstileHandle>(null);
  const useTurnstile = config.authMode === "none";
  const tokens = useMemo(() => ({
    token: () => (useTurnstile ? turnstile.current?.token() ?? Promise.resolve(undefined) : Promise.resolve(undefined)),
    reset: () => turnstile.current?.reset(),
  }), [useTurnstile]);
  const uploads = usePhotoUploads(tokens);
  const { labels, error: labelsError, reload: reloadLabels } = useLabels();

  // keadaan awal: isi tersimpan, atau draf untuk versi yang sama bila berbeda dari yang tersimpan
  const [initial] = useState(() => {
    const saved = Object.fromEntries(view.photos.map((p) => [p.photo_id, p.annotations.map(fromView)]));
    const d = drafts.load(view.id, owner, view.version);
    if (!d || sameAsSaved(d, view.photos)) return { kept: view.photos.map((p) => p.photo_id), boxes: saved, restored: false };
    const present = new Set(view.photos.map((p) => p.photo_id));
    const ds = d.photos.filter((p) => present.has(p.photoId));
    return {
      kept: ds.map((p) => p.photoId),
      boxes: { ...saved, ...Object.fromEntries(ds.map((p) => [p.photoId, p.annotations.map(fromDraft)])) },
      restored: true,
    };
  });
  const [kept, setKept] = useState<string[]>(initial.kept);
  const [boxes, setBoxes] = useState<Record<string, Box[]>>(initial.boxes);
  const [restored, setRestored] = useState(initial.restored);
  const [selected, setSelected] = useState<string>();
  const [showErrors, setShowErrors] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<{ text: string; reload?: boolean }>();

  const byId = new Map(view.photos.map((p) => [p.photo_id, p]));
  // pilihan label per foto: label aktif + label arsip yang sudah ada di foto tersimpan itu (§4.2)
  const optionsFor = (key: string): LabelOption[] => optionsForPhoto(labels ?? [], byId.get(key)?.annotations);

  // label yang sudah tidak tersedia (mis. draf lama, diarsipkan admin) dikosongkan dan ditandai
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
  const existing: PhotoItem[] = kept.map((id) => ({
    key: id, status: "uploaded", progress: 1, photoId: id,
    previewUrl: props.urls[id], width: byId.get(id)?.width, height: byId.get(id)?.height,
  }));
  const items = [...existing, ...uploads.items];
  const current = items.find((i) => i.key === selected && i.previewUrl) ?? items.find((i) => i.previewUrl);
  const ready = readiness(items.map((i) => ({ status: i.status, boxes: boxes[i.key] ?? [] })));
  const remaining = limits.maxPhotos - items.length;

  // simpan draf (hanya foto yang sudah bagian laporan; foto baru tidak bisa dipulihkan)
  useEffect(() => {
    drafts.save(view.id, owner, {
      version: view.version,
      savedAt: new Date().toISOString(),
      photos: kept.map((id) => ({ photoId: id, annotations: (boxes[id] ?? []).map(toDraft) })),
    });
  }, [kept, boxes, view.id, view.version, owner]);

  const removePhoto = (key: string) => {
    if (byId.has(key)) setKept((k) => k.filter((x) => x !== key));
    else uploads.remove(key);
    setBoxes(({ [key]: _gone, ...rest }) => rest);
  };

  const discardDraft = () => {
    drafts.clear(view.id, owner);
    setKept(view.photos.map((p) => p.photo_id));
    setBoxes(Object.fromEntries(view.photos.map((p) => [p.photo_id, p.annotations.map(fromView)])));
    setRestored(false);
  };

  const save = async () => {
    if (saving) return;
    setShowErrors(true);
    setError(undefined);
    if (!ready.ok || !labels?.length) return;
    setSaving(true);
    try {
      const photos = items.map((i) => ({ photo_id: i.photoId!, annotations: (boxes[i.key] ?? []).map(toDTO) }));
      await api.editSubmission(view.id, { version: view.version, photos }, props.editToken);
      drafts.clear(view.id, owner);
      history.update(view.id, { photos: photos.length, boxes: photos.reduce((n, p) => n + p.annotations.length, 0) });
      props.onSaved();
    } catch (e) {
      setSaving(false);
      if (isCode(e, "VERSION_CONFLICT")) setError({ text: errorMessage(e), reload: true });
      else if (isCode(e, "EDIT_WINDOW_CLOSED")) setError({ text: errorMessage(e), reload: true });
      else if (isCode(e, "NOT_FOUND")) setError({ text: "Laporan ini sudah dihapus.", reload: true });
      else setError({ text: errorMessage(e) });
      // label diarsipkan/dihapus admin di tengah jalan: muat ulang daftar; efek di atas menandai kotaknya
      if (isCode(e, "INVALID_LABEL", "LABEL_ARCHIVED")) void reloadLabels();
    }
  };

  return (
    <>
      <LabelsNotice labels={labels} error={labelsError} onRetry={() => void reloadLabels()} />
      {restored && (
        <Banner kind="info">
          Perubahan yang belum disimpan dipulihkan.{" "}
          <button type="button" className="btn btn-small" onClick={discardDraft}>Buang perubahan</button>
        </Banner>
      )}
      <div className="card">
        <PhotoPicker remaining={remaining} onFiles={(f) => uploads.add(f, remaining)} />
      </div>
      <PhotoStrip items={items} selectedKey={current?.key} onSelect={setSelected} onRetry={uploads.retry} onRemove={removePhoto}
        boxCount={(k) => boxes[k]?.length ?? 0} />
      {current?.previewUrl && current.width && current.height && (
        <PhotoAnnotator
          key={current.key}
          src={current.previewUrl}
          width={current.width}
          height={current.height}
          label={`Foto ${items.indexOf(current) + 1}`}
          boxes={boxes[current.key] ?? []}
          labels={optionsFor(current.key)}
          showErrors={showErrors}
          onChange={(b) => setBoxes((m) => ({ ...m, [current.key]: b }))}
          onImageError={() => byId.has(current.key) && props.refreshUrl(current.key)}
        />
      )}
      {error && (
        <Banner kind="error">
          {error.text}{" "}
          {error.reload && (
            <button type="button" className="btn btn-small" onClick={() => { drafts.clear(view.id, owner); props.onReload(); }}>
              Muat ulang
            </button>
          )}
        </Banner>
      )}
      {useTurnstile && <Turnstile ref={turnstile} />}
      <div className="sticky-actions">
        <div>
          <button type="button" className="btn" onClick={props.onCancel} disabled={saving}>Batal</button>
          <span className="summary" aria-live="polite">{!ready.ok && ready.reason ? ready.reason : `${items.length} foto`}</span>
          <button type="button" className="btn btn-primary" onClick={save} disabled={saving || !labels?.length || !items.every((i) => i.status === "uploaded")}>
            {saving ? <><span className="spinner" aria-hidden="true" /> Menyimpan…</> : "Simpan"}
          </button>
        </div>
      </div>
    </>
  );
}
