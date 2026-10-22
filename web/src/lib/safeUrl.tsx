/** URL allowlist shared by report Markdown and citation links. */

const SAFE_PROTOCOLS = new Set(["http:", "https:", "mailto:"]);

/**
 * Allow only absolute http(s)/mailto URLs and in-page fragments.
 *
 * @param url - Untrusted URL.
 * @returns The URL when safe, otherwise an empty string.
 */
export function toSafeHref(url: string): string {
  if (url.startsWith("#")) {
    return url;
  }
  try {
    const parsed = new URL(url);
    return SAFE_PROTOCOLS.has(parsed.protocol) ? parsed.href : "";
  } catch {
    return "";
  }
}
