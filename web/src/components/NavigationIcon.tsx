/** Small original line icons for the four administration destinations. */
const ICON_PATHS = {
  overview: "M4 19V12h3v7M10 19V5h3v14M16 19V9h3v10M3 21h18",
  keys: "M14 4a6 6 0 1 1-4 10L4 20H2v-4l6-6a6 6 0 0 1 6-6Zm2 4h.01",
  requests: "M6 3h12v18H6zM9 7h6M9 11h6M9 15h3",
  settings: "M4 7h16M4 17h16M8 4v6M16 14v6",
} as const;

/** Render a decorative navigation glyph; its adjacent label supplies the name. */
export function NavigationIcon({ name }: { name: keyof typeof ICON_PATHS }) {
  return (
    <svg
      viewBox="0 0 24 24"
      width="19"
      height="19"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={ICON_PATHS[name]} />
    </svg>
  );
}
