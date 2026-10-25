/** Share the Simplified Chinese locale and modal-local popup boundary with Semi. */
import { useEffect } from "react";
import type { ReactNode } from "react";
import ConfigProvider from "@douyinfe/semi-ui/lib/es/configProvider";
import zhCN from "@douyinfe/semi-ui/lib/es/locale/source/zh_CN";
import { useI18n } from "../lib/i18n";
import type { MessageKey } from "../lib/messages";

/** Keep dropdowns and calendars inside the active modal's focus and inert boundary. */
export function popupContainer(): HTMLElement {
  return document.querySelector<HTMLElement>("[data-exta-dialog]") ?? document.body;
}

/** Accessible names that Semi 2.104 hard-codes in English instead of reading its locale. */
const SEMI_LABELS: Record<string, MessageKey> = {
  "Show password": "semi.showPassword",
  "Hidden password": "semi.hidePassword",
  "Choose date": "semi.chooseDate",
  "Change date": "semi.changeDate",
  selected: "semi.selected",
  warning: "semi.warning",
  success: "semi.success",
  info: "semi.info",
  danger: "semi.danger",
  Close: "common.close",
};

/**
 * Translate one element's built-in Semi label. A tag's `Tag:` label is removed so its
 * visible text names it, matching the screen.
 *
 * @param element - Element carrying an `aria-label`.
 * @param translate - Message lookup.
 */
function localizeLabel(element: Element, translate: (key: MessageKey) => string) {
  const label = element.getAttribute("aria-label");
  if (label === null) return;
  if (/^(Closable )?Tag: /.test(label)) {
    element.removeAttribute("aria-label");
    return;
  }
  const key = SEMI_LABELS[label];
  if (key) element.setAttribute("aria-label", translate(key));
}

/** Use the official local zh_CN bundle; no external runtime assets or requests. */
export function SemiProvider({ children }: { children: ReactNode }) {
  const { t } = useI18n();
  useEffect(() => {
    const translate = (key: MessageKey) => t(key);
    document.body
      .querySelectorAll("[aria-label]")
      .forEach((node) => localizeLabel(node, translate));
    const observer = new MutationObserver((records) => {
      for (const record of records) {
        if (record.type === "attributes") localizeLabel(record.target as Element, translate);
        for (const node of record.addedNodes) {
          if (!(node instanceof Element)) continue;
          localizeLabel(node, translate);
          node.querySelectorAll("[aria-label]").forEach((child) => localizeLabel(child, translate));
        }
      }
    });
    observer.observe(document.body, {
      subtree: true,
      childList: true,
      attributes: true,
      attributeFilter: ["aria-label"],
    });
    return () => observer.disconnect();
  }, [t]);
  return (
    <ConfigProvider locale={zhCN} getPopupContainer={popupContainer}>
      {children}
    </ConfigProvider>
  );
}
