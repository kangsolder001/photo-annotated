import { uuid } from "./id";

it("tetap menghasilkan UUID v4 tanpa crypto.randomUUID (http://ip-LAN)", () => {
  const orig = crypto.randomUUID;
  Object.defineProperty(crypto, "randomUUID", { value: undefined, configurable: true });
  try {
    const ids = new Set(Array.from({ length: 100 }, uuid));
    expect(ids.size).toBe(100);
    for (const id of ids) expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  } finally {
    Object.defineProperty(crypto, "randomUUID", { value: orig, configurable: true });
  }
});
