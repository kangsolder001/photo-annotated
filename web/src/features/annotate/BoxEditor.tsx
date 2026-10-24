import { useEffect, useRef, useState, type PointerEvent as RPointerEvent, type KeyboardEvent } from "react";
import { isBigEnough, moveBox, rectFromPoints, resizeBox, type Box, type Corner } from "../../lib/annotation";

// Editor kotak di atas foto. Satu jalur input untuk mouse, sentuh, dan pena (Pointer Events).
//  - Mode gambar aktif: seret di area kosong = kotak baru; seret kotak terpilih = pindah;
//    seret pegangan sudut = ubah ukuran. Halaman tidak ikut bergulir (touch-action: none).
//  - Mode gambar nonaktif: halaman bisa digulir di atas foto; ketuk kotak = pilih.

interface Props {
  src: string;
  alt: string;
  /** Ukuran foto (piksel) — dipakai sebagai sistem koordinat SVG. */
  width: number;
  height: number;
  boxes: Box[];
  selectedId?: string;
  drawMode: boolean;
  readOnly?: boolean;
  onChange?(boxes: Box[]): void;
  onSelect?(id: string | undefined): void;
  /** Dipanggil setelah kotak baru selesai digambar (mis. untuk memfokuskan isian catatannya). */
  onCreate?(id: string): void;
  /** Gambar gagal dimuat (mis. URL bertanda tangan kedaluwarsa). */
  onImageError?(): void;
}

type Gesture =
  | { kind: "draw"; x0: number; y0: number; x1: number; y1: number }
  | { kind: "move"; id: string; px: number; py: number; orig: Box }
  | { kind: "resize"; id: string; corner: Corner; orig: Box };

const CORNERS: Corner[] = ["nw", "ne", "sw", "se"];
const KEY_STEP = 0.005;

export function BoxEditor(p: Props) {
  const svg = useRef<SVGSVGElement>(null);
  const gesture = useRef<Gesture | null>(null);
  const [draft, setDraft] = useState<Gesture | null>(null);
  // piksel foto per piksel layar: menjaga garis/pegangan tetap berukuran sama di layar mana pun
  const [unit, setUnit] = useState(1);

  useEffect(() => {
    const el = svg.current;
    if (!el) return;
    const update = () => {
      const w = el.getBoundingClientRect().width;
      if (w > 0) setUnit(p.width / w);
    };
    update();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, [p.width]);

  const editable = !p.readOnly && p.drawMode;

  const point = (e: RPointerEvent) => {
    const r = svg.current!.getBoundingClientRect();
    return { x: (e.clientX - r.left) / r.width, y: (e.clientY - r.top) / r.height };
  };

  const begin = (e: RPointerEvent, g: Gesture) => {
    gesture.current = g;
    setDraft(g);
    svg.current?.setPointerCapture?.(e.pointerId); // jsdom/browser lama: tanpa capture tetap jalan
  };

  const onBackgroundDown = (e: RPointerEvent) => {
    if (e.button !== 0) return;
    p.onSelect?.(undefined);
    if (!editable) return;
    const { x, y } = point(e);
    begin(e, { kind: "draw", x0: x, y0: y, x1: x, y1: y });
  };

  const onBoxDown = (e: RPointerEvent, b: Box) => {
    if (e.button !== 0) return;
    e.stopPropagation();
    p.onSelect?.(b.id);
    if (!editable) return;
    const { x, y } = point(e);
    begin(e, { kind: "move", id: b.id, px: x, py: y, orig: b });
  };

  const onHandleDown = (e: RPointerEvent, b: Box, corner: Corner) => {
    if (e.button !== 0 || !editable) return;
    e.stopPropagation();
    begin(e, { kind: "resize", id: b.id, corner, orig: b });
  };

  const replace = (next: Box) => p.onChange?.(p.boxes.map((b) => (b.id === next.id ? next : b)));

  const onMove = (e: RPointerEvent) => {
    const g = gesture.current;
    if (!g) return;
    const { x, y } = point(e);
    if (g.kind === "draw") {
      const ng = { ...g, x1: x, y1: y };
      gesture.current = ng;
      setDraft(ng);
    } else if (g.kind === "move") {
      replace(moveBox(g.orig, x - g.px, y - g.py));
    } else {
      replace(resizeBox(g.orig, g.corner, x, y));
    }
  };

  const onUp = () => {
    const g = gesture.current;
    gesture.current = null;
    setDraft(null);
    if (g?.kind !== "draw") return;
    const r = rectFromPoints(g.x0, g.y0, g.x1, g.y1);
    if (!isBigEnough(r)) return; // ketukan biasa, bukan seret
    const box: Box = { id: crypto.randomUUID(), ...r, text: "" };
    p.onChange?.([...p.boxes, box]);
    p.onSelect?.(box.id);
    p.onCreate?.(box.id);
  };

  // keyboard: panah menggeser kotak terpilih (Shift = 5×), Delete menghapus
  const onKey = (e: KeyboardEvent) => {
    const sel = p.boxes.find((b) => b.id === p.selectedId);
    if (!sel || p.readOnly) return;
    const step = e.shiftKey ? KEY_STEP * 5 : KEY_STEP;
    const d: Record<string, [number, number]> = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] };
    if (d[e.key]) {
      e.preventDefault();
      replace(moveBox(sel, d[e.key]![0], d[e.key]![1]));
    } else if (e.key === "Delete" || e.key === "Backspace") {
      e.preventDefault();
      p.onChange?.(p.boxes.filter((b) => b.id !== sel.id));
      p.onSelect?.(undefined);
    }
  };

  const W = p.width, H = p.height;
  const stroke = 2.5 * unit;
  const draftRect = draft?.kind === "draw" ? rectFromPoints(draft.x0, draft.y0, draft.x1, draft.y1) : null;

  return (
    <div className="box-editor">
      <img src={p.src} alt={p.alt} draggable={false} onError={p.onImageError} />
      <svg
        ref={svg}
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="none"
        style={{ touchAction: editable ? "none" : "pan-x pan-y pinch-zoom", cursor: editable ? "crosshair" : "default" }}
        onPointerDown={onBackgroundDown}
        onPointerMove={onMove}
        onPointerUp={onUp}
        onPointerCancel={onUp}
        onKeyDown={onKey}
        tabIndex={0}
        role="application"
        aria-label={p.readOnly ? "Foto dengan kotak anotasi" : "Area menggambar kotak. Seret untuk membuat kotak; panah untuk menggeser kotak terpilih; Delete untuk menghapus."}
      >
        {p.boxes.map((b, i) => {
          const sel = b.id === p.selectedId;
          const color = sel ? "var(--box-selected)" : "var(--box)";
          const label = String(i + 1);
          const fs = 14 * unit;
          return (
            <g key={b.id}>
              <rect
                x={b.x * W} y={b.y * H} width={b.w * W} height={b.h * H}
                fill={sel ? "rgb(14 165 233 / 18%)" : "rgb(245 158 11 / 10%)"}
                stroke={color} strokeWidth={stroke}
                style={{ cursor: editable && sel ? "move" : "pointer" }}
                onPointerDown={(e) => onBoxDown(e, b)}
              />
              <rect x={b.x * W} y={b.y * H} width={(label.length * 0.62 + 0.9) * fs} height={1.45 * fs} fill={color} pointerEvents="none" />
              <text x={b.x * W + 0.45 * fs} y={b.y * H + 1.1 * fs} fontSize={fs} fontWeight={700} fill="#111" pointerEvents="none">{label}</text>
              {sel && editable && CORNERS.map((c) => {
                const cx = (c === "nw" || c === "sw" ? b.x : b.x + b.w) * W;
                const cy = (c === "nw" || c === "ne" ? b.y : b.y + b.h) * H;
                return (
                  <g key={c} onPointerDown={(e) => onHandleDown(e, b, c)} style={{ cursor: c === "nw" || c === "se" ? "nwse-resize" : "nesw-resize" }}>
                    {/* area sentuh ±44 px, lingkaran tampak 18 px */}
                    <circle cx={cx} cy={cy} r={22 * unit} fill="transparent" />
                    <circle cx={cx} cy={cy} r={9 * unit} fill="#fff" stroke="var(--box-selected)" strokeWidth={stroke} />
                  </g>
                );
              })}
            </g>
          );
        })}
        {draftRect && (
          <rect x={draftRect.x * W} y={draftRect.y * H} width={draftRect.w * W} height={draftRect.h * H}
            fill="rgb(14 165 233 / 15%)" stroke="var(--box-selected)" strokeWidth={stroke} strokeDasharray={`${8 * unit} ${6 * unit}`} pointerEvents="none" />
        )}
      </svg>
    </div>
  );
}
