/** Exta logo mark drawn as inline SVG; no remote assets. */

/**
 * Decorative Exta mark: two upstream strokes converging into one routed path.
 *
 * @param props - Optional pixel size.
 * @returns SVG element hidden from assistive technology.
 */
export function BrandMark({ size = 28 }: { size?: number }) {
  return (
    <svg
      className="brand-mark"
      viewBox="0 0 32 32"
      width={size}
      height={size}
      aria-hidden="true"
      focusable="false"
    >
      <rect x="0.5" y="0.5" width="31" height="31" rx="9" className="brand-mark-bg" />
      <path d="M9 10.5h8.5a4.5 4.5 0 0 1 0 9H9" className="brand-mark-stroke" />
      <path d="M9 16h14" className="brand-mark-stroke brand-mark-accent" />
      <circle cx="23" cy="16" r="2" className="brand-mark-dot" />
    </svg>
  );
}
