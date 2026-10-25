import { compress, fitWithin, ImageProcessingError, type Encoder } from "./image";

/** Encoder palsu: ukuran blob sebanding dengan piksel × kualitas. */
function fakeEncoder(bytesPerPixel: number, supportsWebP = true): Encoder & { calls: string[] } {
  const calls: string[] = [];
  return {
    calls,
    async encode(w, h, type, q) {
      calls.push(`${w}x${h} ${type} ${q}`);
      if (type === "image/webp" && !supportsWebP) return null;
      return { size: Math.round(w * h * bytesPerPixel * q), type } as Blob; // tanpa alokasi sungguhan
    },
  };
}

describe("fitWithin", () => {
  it("mengecilkan sisi terpanjang ke batas dan menjaga rasio", () => {
    expect(fitWithin(4000, 3000, 1920)).toEqual({ width: 1920, height: 1440 });
    expect(fitWithin(3000, 4000, 1920)).toEqual({ width: 1440, height: 1920 });
  });
  it("tidak pernah memperbesar", () => {
    expect(fitWithin(800, 600, 1920)).toEqual({ width: 800, height: 600 });
  });
});

describe("compress", () => {
  it("foto HP 4000×3000 menjadi ≤ 2 MB dengan sisi ≤ 1920", async () => {
    const enc = fakeEncoder(1.2);
    const out = await compress(enc, 4000, 3000);
    expect(out.blob.size).toBeLessThanOrEqual(2 * 1024 * 1024);
    expect(Math.max(out.width, out.height)).toBeLessThanOrEqual(1920);
    expect(out.contentType).toBe("image/webp");
  });
  it("menurunkan kualitas lalu dimensi bila perlu", async () => {
    const enc = fakeEncoder(2.5);
    const out = await compress(enc, 4000, 3000);
    expect(out.blob.size).toBeLessThanOrEqual(2 * 1024 * 1024);
    expect(out.width).toBeLessThan(1920);
  });
  it("jatuh ke JPEG bila WebP tidak didukung", async () => {
    const out = await compress(fakeEncoder(0.5, false), 1000, 800);
    expect(out.contentType).toBe("image/jpeg");
  });
  it("menyerah dengan pesan jelas bila tidak mungkin", async () => {
    await expect(compress(fakeEncoder(1000), 4000, 3000)).rejects.toBeInstanceOf(ImageProcessingError);
  });
});
