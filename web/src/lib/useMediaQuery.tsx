/** Subscribe to a CSS media query so layouts can recompose instead of only scaling. */
import { useEffect, useState } from "react";

/** Width below which dense tables are presented as stacked records. */
export const COMPACT_QUERY = "(max-width: 820px)";

/**
 * Track whether a media query currently matches. Environments without `matchMedia`
 * (such as unit-test DOMs) report `false`, which selects the full desktop layout.
 *
 * @param query - CSS media query text.
 * @returns Current match state.
 */
export function useMediaQuery(query: string): boolean {
  const [isMatch, setMatch] = useState(() => window.matchMedia?.(query).matches ?? false);
  useEffect(() => {
    const media = window.matchMedia?.(query);
    if (!media) return;
    const update = () => setMatch(media.matches);
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, [query]);
  return isMatch;
}
