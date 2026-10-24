import type { Box } from "./annotation";
import { readiness } from "./readiness";

const good: Box = { id: "a", x: 0.1, y: 0.1, w: 0.2, h: 0.2, labelId: 1 };

it("butuh foto", () => expect(readiness([]).ok).toBe(false));
it("menunggu upload selesai", () => expect(readiness([{ status: "uploading", boxes: [good] }]).reason).toMatch(/upload/));
it("foto gagal menahan kirim", () => expect(readiness([{ status: "error", boxes: [] }]).ok).toBe(false));
it("kotak tanpa label menahan kirim", () => {
  const r = readiness([{ status: "uploaded", boxes: [good, { ...good, id: "b", labelId: null }] }]);
  expect(r).toMatchObject({ ok: false, invalidBoxes: 1 });
});
it("siap", () => expect(readiness([{ status: "uploaded", boxes: [good] }, { status: "uploaded", boxes: [] }]).ok).toBe(true));
