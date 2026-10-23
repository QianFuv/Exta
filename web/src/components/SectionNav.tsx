/** URL-backed secondary navigation retains reload and browser history semantics. */
import { Link, useSearchParams } from "react-router";

/** Render named views within a single primary destination. */
export function SectionNav({
  items,
  label,
}: {
  items: ReadonlyArray<{ value: string; label: string }>;
  label: string;
}) {
  const [params] = useSearchParams();
  const current = items.find((item) => item.value === params.get("view"))?.value ?? items[0]?.value;
  return (
    <nav className="section-nav" aria-label={label}>
      {items.map((item) => (
        <Link
          key={item.value}
          to={`?view=${item.value}`}
          aria-current={current === item.value ? "page" : undefined}
        >
          {item.label}
        </Link>
      ))}
    </nav>
  );
}
