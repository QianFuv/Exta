/** Exta logo mark drawn as inline SVG; no remote assets. */

/**
 * Decorative Exta mark.
 *
 * @returns SVG element hidden from assistive technology.
 */
export function BrandMark() {
  return (
    <svg
      className="brand-mark"
      viewBox="0 0 32 32"
      width="28"
      height="28"
      aria-hidden="true"
      focusable="false"
    >
      <rect x="1" y="1" width="30" height="30" rx="8" className="brand-mark-bg" />
      <path d="M10 9h12v3H13.5v2.5H21v3h-7.5V20H22v3H10z" className="brand-mark-fg" />
    </svg>
  );
}
