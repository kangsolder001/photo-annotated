import type { IconDefinition } from "@fortawesome/fontawesome-svg-core";
import { FontAwesomeIcon } from "@fortawesome/react-fontawesome";
import type { ButtonHTMLAttributes } from "react";

// Ikon Font Awesome Free (SVG inline, di-import satu per satu; spec-labels-admin §7.5).
// Ikon mengikuti warna teks (currentColor) sehingga ikut tema terang/gelap.

/** Ikon dekoratif di samping teks: selalu aria-hidden, ukuran 1em. */
export function Icon({ icon, className, spin }: { icon: IconDefinition; className?: string; spin?: boolean }) {
  return <FontAwesomeIcon icon={icon} className={className} spin={spin} aria-hidden="true" focusable="false" />;
}

/**
 * Tombol hanya-ikon: wajib punya label (aria-label + tooltip). Ikon 20 px, area sentuh ≥ 44 px.
 */
export function IconButton({
  icon,
  label,
  className,
  type = "button",
  ...rest
}: { icon: IconDefinition; label: string } & Omit<ButtonHTMLAttributes<HTMLButtonElement>, "aria-label" | "title" | "children">) {
  return (
    <button type={type} className={`icon-btn${className ? ` ${className}` : ""}`} aria-label={label} title={label} {...rest}>
      <Icon icon={icon} />
    </button>
  );
}
