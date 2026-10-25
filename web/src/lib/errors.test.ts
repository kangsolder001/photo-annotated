import { ApiError } from "../api/client";
import { errorMessage } from "./errors";

it("rate limit menyebut waktu tunggu", () => {
  expect(errorMessage(new ApiError(429, "RATE_LIMITED", "x", 42))).toBe("Terlalu banyak permintaan. Coba lagi dalam 42 detik.");
  expect(errorMessage(new ApiError(429, "RATE_LIMITED", "x", 3000))).toMatch(/50 menit/);
});
it("kode tak dikenal 5xx tidak membocorkan pesan server", () => {
  expect(errorMessage(new ApiError(500, "INTERNAL", "pq: relation does not exist"))).toMatch(/gangguan di server/);
});
