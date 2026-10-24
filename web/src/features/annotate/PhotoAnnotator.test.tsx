import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import type { Box } from "../../lib/annotation";
import { PhotoAnnotator } from "./PhotoAnnotator";

function Harness({ initial = [] as Box[] }) {
  const [boxes, setBoxes] = useState(initial);
  return <PhotoAnnotator src="x.webp" width={1000} height={500} label="Foto 1" boxes={boxes} onChange={setBoxes} showErrors />;
}

// SVG di jsdom tidak punya ukuran: anggap tampil 200×100 px di (0,0)
function sizeSvg() {
  const svg = screen.getByRole("application");
  svg.getBoundingClientRect = () => ({ left: 0, top: 0, width: 200, height: 100, right: 200, bottom: 100, x: 0, y: 0, toJSON() {} });
  return svg;
}

it("seret di foto membuat kotak bernomor dan memfokuskan isian catatannya", () => {
  render(<Harness />);
  const svg = sizeSvg();
  fireEvent.pointerDown(svg, { button: 0, clientX: 20, clientY: 10, pointerId: 1 });
  fireEvent.pointerMove(svg, { clientX: 120, clientY: 60, pointerId: 1 });
  fireEvent.pointerUp(svg, { clientX: 120, clientY: 60, pointerId: 1 });

  const note = screen.getByLabelText("Catatan kotak 1");
  expect(document.activeElement).toBe(note);
  expect(screen.getByText("Catatan wajib diisi.")).toBeTruthy();
  fireEvent.change(note, { target: { value: "Penyok di sudut" } });
  expect(screen.queryByText("Catatan wajib diisi.")).toBeNull();
});

it("ketukan tanpa seret tidak membuat kotak", () => {
  render(<Harness />);
  const svg = sizeSvg();
  fireEvent.pointerDown(svg, { button: 0, clientX: 50, clientY: 50, pointerId: 1 });
  fireEvent.pointerUp(svg, { clientX: 50, clientY: 50, pointerId: 1 });
  expect(screen.queryByLabelText("Catatan kotak 1")).toBeNull();
});

it("mode gulir tidak membuat kotak; hapus kotak dari daftar", () => {
  render(<Harness initial={[{ id: "a", x: 0.1, y: 0.1, w: 0.3, h: 0.3, text: "retak" }]} />);
  fireEvent.click(screen.getByRole("button", { name: /Menggambar kotak/ }));
  const svg = sizeSvg();
  fireEvent.pointerDown(svg, { button: 0, clientX: 150, clientY: 70, pointerId: 1 });
  fireEvent.pointerMove(svg, { clientX: 190, clientY: 95, pointerId: 1 });
  fireEvent.pointerUp(svg, { clientX: 190, clientY: 95, pointerId: 1 });
  expect(screen.queryByLabelText("Catatan kotak 2")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Hapus kotak 1" }));
  expect(screen.queryByLabelText("Catatan kotak 1")).toBeNull();
});
