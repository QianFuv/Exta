/** One keyboard-accessible menu for appearance, language and session actions. */
import { useRef, useState } from "react";
import Dropdown from "@douyinfe/semi-ui/lib/es/dropdown";
import Button from "@douyinfe/semi-ui/lib/es/button";
import Tooltip from "@douyinfe/semi-ui/lib/es/tooltip";
import IconSetting from "@douyinfe/semi-icons/lib/es/icons/IconSetting";
import { useI18n } from "../lib/i18n";

/** Semi forwards refs but omits arbitrary ARIA props; annotate the real item and decorative tick. */
function annotatePreference(node: HTMLLIElement | null, isCurrent: boolean) {
  if (!node) return;
  node.setAttribute("aria-current", String(isCurrent));
  node.querySelectorAll(".semi-icon").forEach((icon) => icon.setAttribute("aria-hidden", "true"));
}

/** Keep all preferences local and preserve the existing authenticated sign-out action. */
export function PreferencesMenu({
  onLogout,
  isLoggingOut,
}: {
  onLogout: () => void;
  isLoggingOut: boolean;
}) {
  const { t, locale, setLocale } = useI18n();
  const [isOpen, setOpen] = useState(false);
  const [theme, setTheme] = useState(() => document.documentElement.dataset.theme ?? "system");
  const [isSessionOnly, setSessionOnly] = useState(false);
  const host = useRef<HTMLSpanElement>(null);
  /** Close and restore focus before applying a menu action. */
  function select(action: () => void) {
    setOpen(false);
    host.current?.querySelector("button")?.focus();
    action();
  }
  /** Apply appearance immediately; storage failure never prevents this session's choice. */
  function appearance(value: "light" | "dark" | "system") {
    document.documentElement.setAttribute("data-theme", value);
    document.body.setAttribute(
      "theme-mode",
      value === "dark" ||
        (value === "system" && window.matchMedia?.("(prefers-color-scheme: dark)").matches)
        ? "dark"
        : "light",
    );
    setTheme(value);
    try {
      localStorage.setItem("exta.theme", value);
      setSessionOnly(false);
    } catch {
      setSessionOnly(true);
    }
  }
  return (
    <Tooltip
      content={t("nav.interface")}
      motion={false}
      trigger={isOpen ? "custom" : "hover"}
      {...(isOpen ? { visible: false } : {})}
    >
      <span ref={host} data-exta-popup-open={isOpen} className="preferences-menu">
        <Dropdown
          showTick
          trigger="custom"
          visible={isOpen}
          motion={false}
          position="bottomRight"
          onClickOutSide={() => setOpen(false)}
          closeOnEsc
          onEscKeyDown={(event) => {
            event.stopPropagation();
            select(() => {});
          }}
          render={
            <Dropdown.Menu>
              <Dropdown.Title>{t("theme.label")}</Dropdown.Title>
              {(["light", "dark", "system"] as const).map((value) => (
                <Dropdown.Item
                  key={value}
                  active={theme === value}
                  ref={(node) => annotatePreference(node, theme === value)}
                  showTick
                  onClick={() => select(() => appearance(value))}
                >
                  {t(`theme.${value}`)}
                </Dropdown.Item>
              ))}
              <Dropdown.Divider />
              <Dropdown.Title>{t("language.label")}</Dropdown.Title>
              <Dropdown.Item
                active={locale === "zh"}
                ref={(node) => annotatePreference(node, locale === "zh")}
                showTick
                onClick={() => select(() => setLocale("zh"))}
              >
                简体中文
              </Dropdown.Item>
              <Dropdown.Item
                active={locale === "en"}
                ref={(node) => annotatePreference(node, locale === "en")}
                showTick
                onClick={() => select(() => setLocale("en"))}
              >
                English
              </Dropdown.Item>
              <Dropdown.Divider />
              <Dropdown.Item
                ref={(node) => annotatePreference(node, false)}
                disabled={isLoggingOut}
                onClick={() => select(onLogout)}
              >
                {isLoggingOut ? t("session.signingOut") : t("session.signOut")}
              </Dropdown.Item>
            </Dropdown.Menu>
          }
        >
          <Button
            theme="borderless"
            type="tertiary"
            icon={<IconSetting aria-hidden />}
            aria-label={t("nav.interface")}
            aria-haspopup="menu"
            aria-expanded={isOpen}
            onClick={() => setOpen(!isOpen)}
            onKeyDownCapture={(event) => {
              if (event.key === "Enter" || event.key === " ") event.stopPropagation();
            }}
          />
        </Dropdown>
        {isSessionOnly ? (
          <span className="visually-hidden" role="status">
            {t("theme.sessionOnly")}
          </span>
        ) : null}
      </span>
    </Tooltip>
  );
}
