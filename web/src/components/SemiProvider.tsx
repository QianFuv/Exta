/** Share the application's locale and modal-local popup boundary with Semi. */
import type { ReactNode } from "react";
import ConfigProvider from "@douyinfe/semi-ui/lib/es/configProvider";
import enUS from "@douyinfe/semi-ui/lib/es/locale/source/en_US";
import zhCN from "@douyinfe/semi-ui/lib/es/locale/source/zh_CN";
import { useI18n } from "../lib/i18n";

/** Keep dropdowns and calendars inside the active modal's focus and inert boundary. */
export function popupContainer(): HTMLElement {
  return document.querySelector<HTMLElement>("[data-exta-dialog]") ?? document.body;
}

/** Use official local locale bundles; no external runtime assets or requests. */
export function SemiProvider({ children }: { children: ReactNode }) {
  const { locale } = useI18n();
  return (
    <ConfigProvider locale={locale === "zh" ? zhCN : enUS} getPopupContainer={popupContainer}>
      {children}
    </ConfigProvider>
  );
}
