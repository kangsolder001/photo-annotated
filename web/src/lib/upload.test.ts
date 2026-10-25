import { ApiError } from "../api/client";
import { backoffMs, isRetryable, uploadWithRetry, type PutResult } from "./upload";

describe("isRetryable", () => {
  it.each<[PutResult, boolean]>([
    [{ status: 0 }, true],
    [{ status: 408 }, true],
    [{ status: 503, code: "UPLOAD_BUSY" }, true],
    [{ status: 503, code: "STORAGE_FULL" }, false],
    [{ status: 503, code: "STORAGE_UNAVAILABLE" }, false],
    [{ status: 409, code: "UPLOAD_IN_PROGRESS" }, true],
    [{ status: 409, code: "PHOTO_NOT_AVAILABLE" }, false],
    [{ status: 422, code: "SIZE_MISMATCH" }, true],
    [{ status: 422, code: "INVALID_IMAGE" }, false],
    [{ status: 403 }, false],
    [{ status: 410 }, false],
    [{ status: 413 }, false],
  ])("%o → %s", (r, want) => expect(isRetryable(r)).toBe(want));
});

it("backoff menghormati Retry-After", () => {
  expect(backoffMs(0, 2)).toBe(2000);
  expect(backoffMs(1, undefined, () => 0)).toBe(2000);
});

describe("uploadWithRetry", () => {
  const blob = new Blob(["x"]);
  const noWait = async () => {};

  it("mengulang kondisi sementara lalu berhasil", async () => {
    const results: PutResult[] = [{ status: 503, code: "UPLOAD_BUSY", retryAfter: 2 }, { status: 0 }, { status: 204 }];
    const waits: number[] = [];
    await uploadWithRetry("u", blob, "image/webp", undefined, undefined, async () => results.shift()!, async (ms) => {
      waits.push(ms);
    });
    expect(waits[0]).toBe(2000);
    expect(results).toHaveLength(0);
  });

  it("berhenti setelah 3 kali ulang", async () => {
    let calls = 0;
    const err = await uploadWithRetry("u", blob, "image/webp", undefined, undefined, async () => {
      calls++;
      return { status: 0 };
    }, noWait).catch((e: unknown) => e);
    expect(calls).toBe(4);
    expect(err).toBeInstanceOf(ApiError);
  });

  it("tidak mengulang kegagalan permanen dan memberi pesan yang jelas", async () => {
    let calls = 0;
    const err = (await uploadWithRetry("u", blob, "image/webp", undefined, undefined, async () => {
      calls++;
      return { status: 503, code: "STORAGE_FULL" };
    }, noWait).catch((e: unknown) => e)) as ApiError;
    expect(calls).toBe(1);
    expect(err.code).toBe("STORAGE_FULL");
    expect(err.message).toMatch(/penuh/);
  });
});
