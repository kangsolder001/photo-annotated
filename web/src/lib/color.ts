// Warna: luminans & kontras WCAG 2.x, dan warna teks yang terbaca di atas warna label.

export function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r! + 0.7152 * g! + 0.0722 * b!;
}

export function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi! + 0.05) / (lo! + 0.05);
}

const DARK = "#111111";
const LIGHT = "#ffffff";

/** Teks hitam atau putih, mana yang lebih kontras di atas `bg` (#rrggbb). */
export function textOn(bg: string): string {
  return contrast(DARK, bg) >= contrast(LIGHT, bg) ? DARK : LIGHT;
}

export const isHexColor = (v: string) => /^#[0-9a-f]{6}$/.test(v);
