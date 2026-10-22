/** Report renderer that escapes raw HTML, blocks images and forces safe link targets. */
import Markdown from "react-markdown";
import type { Components, UrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";
import { toSafeHref } from "../lib/safeUrl";

const safeUrlTransform: UrlTransform = (url) => toSafeHref(url);

const COMPONENTS: Components = {
  h1: ({ children }) => <h3>{children}</h3>,
  h2: ({ children }) => <h4>{children}</h4>,
  h3: ({ children }) => <h5>{children}</h5>,
  h4: ({ children }) => <h6>{children}</h6>,
  h5: ({ children }) => <h6>{children}</h6>,
  img: ({ alt }) => <span className="md-image-blocked">[{alt || "image"}]</span>,
  a: ({ href, children }) => {
    if (!href) {
      return <span className="md-unsafe-link">{children}</span>;
    }
    if (href.startsWith("#")) {
      return <a href={href}>{children}</a>;
    }
    return (
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer nofollow"
        referrerPolicy="no-referrer"
      >
        {children}
      </a>
    );
  },
};

/**
 * Render untrusted Markdown. Raw HTML is dropped rather than interpreted, images are
 * replaced by their alt text without loading, other embeddable elements are removed, and links open in a
 * new context without referrer or opener. Headings are demoted so reports nest under
 * the page section heading.
 *
 * @param props - Markdown source.
 * @returns Rendered content.
 */
export function SafeMarkdown({ source }: { source: string }) {
  return (
    <div className="markdown">
      <Markdown
        remarkPlugins={[remarkGfm]}
        skipHtml
        disallowedElements={[
          "iframe",
          "object",
          "embed",
          "video",
          "audio",
          "source",
          "picture",
          "script",
          "style",
        ]}
        unwrapDisallowed
        urlTransform={safeUrlTransform}
        components={COMPONENTS}
      >
        {source}
      </Markdown>
    </div>
  );
}
