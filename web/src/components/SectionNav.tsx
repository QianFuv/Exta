/** Semi tabs retain URL-backed views and browser history. */
import Tabs from "@douyinfe/semi-ui/lib/es/tabs";
import { useSearchParams } from "react-router";

/** Render existing secondary destinations without introducing new routes. */
export function SectionNav({
  items,
  label,
}: {
  items: ReadonlyArray<{ value: string; label: string }>;
  label: string;
}) {
  const [params, setParams] = useSearchParams();
  const current = items.find((item) => item.value === params.get("view"))?.value ?? items[0]?.value;
  return (
    <div className="exta-section-tabs" aria-label={label}>
      <Tabs
        activeKey={current}
        type="line"
        tabPaneMotion={false}
        onChange={(key) => setParams({ view: key })}
      >
        {items.map((item) => (
          <Tabs.TabPane key={item.value} itemKey={item.value} tab={item.label} />
        ))}
      </Tabs>
    </div>
  );
}
