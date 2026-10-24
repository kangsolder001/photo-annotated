import { passwordProblem } from "./password";

it.each([
  ["rahasia123", null],
  ["kata sandi panjang", null],
  ["pässwörd", null],
  ["pendek", /minimal 8/],
  ["        ", /spasi/],
  ["a".repeat(73), /72 byte/],
  ["rahasia\u0000123", /tidak diizinkan/],
])("%j", (p, want) => {
  const got = passwordProblem(p);
  if (want === null) expect(got).toBeNull();
  else expect(got).toMatch(want);
});
