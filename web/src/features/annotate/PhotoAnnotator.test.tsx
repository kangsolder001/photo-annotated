import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import type { Box } from "../../lib/annotation";
import type { LabelOption } from "../../lib/labels";
import { PhotoAnnotator } from "./PhotoAnnotator";

const LABELS: LabelOption[] = [
  { id: 3, name: "Retak", color: "#e4572e", description: "Retakan pada dinding paket" },
  { id: 5, name: "Penyok", color: "#2e86ab" },
];

function Harness({ initial = [] as Box[], labels = LABELS }) {
  const [boxes, setBoxes] = useState(initial);
  return <PhotoAnnotator src="x.webp" width={1000} height={500} label="Foto 1" boxes={boxes} labels={labels} onChange={setBoxes} showErrors />;
}

// SVG di jsdom tidak punya ukuran: anggap tampil 200×100 px di (0,0)
function sizeSvg() {
  const svg = screen.getByRole("application");
  svg.getBoundingClientRect = () => ({ left: 0, top: 0, width: 200, height: 100, right: 200, bottom: 100, x: 0, y: 0, toJSON() {} });
  return svg;
}

const colorOfBox = (n: number) => document.querySelectorAll("svg g[data-color]")[n]?.getAttribute("data-color");

it("seret di foto membuat kotak bernomor tanpa label (abu-abu) dan memfokuskan dropdown labelnya", () => {
  render(<Harness />);
  const svg = sizeSvg();
  fireEvent.pointerDown(svg, { button: 0, clientX: 20, clientY: 10, pointerId: 1 });
  fireEvent.pointerMove(svg, { clientX: 120, clientY: 60, pointerId: 1 });
  fireEvent.pointerUp(svg, { clientX: 120, clientY: 60, pointerId: 1 });

  const select = screen.getByLabelText("Label kotak 1") as HTMLSelectElement;
  expect(document.activeElement).toBe(select);
  expect(select.value).toBe("");
  expect(screen.getByRole("option", { name: "Pilih label…" })).toBeTruthy();
  expect(screen.getByText("Pilih label.")).toBeTruthy();
  expect(colorOfBox(0)).toBe("#8a8f98");
  // garis putus-putus untuk kotak tanpa label
  expect(document.querySelector("svg g[data-color] rect[stroke-dasharray]")).not.toBeNull();

  fireEvent.change(select, { target: { value: "3" } });
  expect(screen.queryByText("Pilih label.")).toBeNull();
  expect(screen.getByText("Retakan pada dinding paket")).toBeTruthy(); // deskripsi sebagai petunjuk
  expect(colorOfBox(0)).toBe("#e4572e"); // warna kotak = warna label
});

it("tidak ada isian teks bebas", () => {
  render(<Harness initial={[{ id: "a", x: 0.1, y: 0.1, w: 0.3, h: 0.3, labelId: 5 }]} />);
  expect(screen.queryByRole("textbox")).toBeNull();
  expect((screen.getByLabelText("Label kotak 1") as HTMLSelectElement).value).toBe("5");
});

it("ketukan tanpa seret tidak membuat kotak", () => {
  render(<Harness />);
  const svg = sizeSvg();
  fireEvent.pointerDown(svg, { button: 0, clientX: 50, clientY: 50, pointerId: 1 });
  fireEvent.pointerUp(svg, { clientX: 50, clientY: 50, pointerId: 1 });
  expect(screen.queryByLabelText("Label kotak 1")).toBeNull();
});

it("mode gulir tidak membuat kotak; hapus kotak dari daftar", () => {
  render(<Harness initial={[{ id: "a", x: 0.1, y: 0.1, w: 0.3, h: 0.3, labelId: 3 }]} />);
  fireEvent.click(screen.getByRole("button", { name: /Menggambar kotak/ }));
  const svg = sizeSvg();
  fireEvent.pointerDown(svg, { button: 0, clientX: 150, clientY: 70, pointerId: 1 });
  fireEvent.pointerMove(svg, { clientX: 190, clientY: 95, pointerId: 1 });
  fireEvent.pointerUp(svg, { clientX: 190, clientY: 95, pointerId: 1 });
  expect(screen.queryByLabelText("Label kotak 2")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Hapus kotak 1" }));
  expect(screen.queryByLabelText("Label kotak 1")).toBeNull();
});

it("kotak yang labelnya dikosongkan karena tidak tersedia ditandai", () => {
  render(<Harness initial={[{ id: "a", x: 0.1, y: 0.1, w: 0.3, h: 0.3, labelId: null, flagged: true, legacyText: "Kerusakan kiri" }]} />);
  expect(screen.getByText("Label sebelumnya sudah tidak tersedia. Pilih label lain.")).toBeTruthy();
  expect(screen.getByText("catatan lama")).toBeTruthy();
  expect(screen.getByText("Kerusakan kiri")).toBeTruthy();
});

it("label arsip yang sudah ada di foto tetap bisa dipilih, ditandai (diarsipkan)", () => {
  render(<Harness labels={[...LABELS, { id: 9, name: "Sobek", color: "#a23b72", archived: true }]} initial={[{ id: "a", x: 0.1, y: 0.1, w: 0.3, h: 0.3, labelId: 9 }]} />);
  expect(screen.getByRole("option", { name: "Sobek (diarsipkan)" })).toBeTruthy();
  expect(screen.queryByText(/Pilih label/)).toBeTruthy(); // opsi kosong tetap ada
  expect(screen.queryByText("Pilih label.")).toBeNull();
});

it("tampilan baca: nama + warna label; baris lama dengan tanda catatan lama", () => {
  render(
    <PhotoAnnotator src="x.webp" width={1000} height={500} label="Foto 1" readOnly labels={LABELS}
      boxes={[{ id: "a", x: 0.1, y: 0.1, w: 0.3, h: 0.3, labelId: 3 }, { id: "b", x: 0.5, y: 0.5, w: 0.2, h: 0.2, labelId: null, legacyText: "Label <resi> & sobek" }]} />,
  );
  expect(screen.getByText("Retak")).toBeTruthy();
  expect(screen.getByText("Label <resi> & sobek")).toBeTruthy(); // teks apa adanya, bukan HTML
  expect(screen.getByText("catatan lama")).toBeTruthy();
  expect(screen.queryByRole("combobox")).toBeNull();
});
