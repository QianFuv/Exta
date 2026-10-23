/** Shared language and appearance controls for both sides of authentication. */
import { useState } from "react";
import Dropdown from "@douyinfe/semi-ui/lib/es/dropdown";
import Button from "@douyinfe/semi-ui/lib/es/button";
import Tooltip from "@douyinfe/semi-ui/lib/es/tooltip";
import IconLanguage from "@douyinfe/semi-icons/lib/es/icons/IconLanguage";
import IconSun from "@douyinfe/semi-icons/lib/es/icons/IconSun";
import { useI18n } from "../lib/i18n";

/** Semi forwards refs but omits arbitrary ARIA props on menu items. */
function annotatePreference(node: HTMLLIElement | null, isCurrent: boolean) {
  if (!node) return;
  node.setAttribute("aria-current", String(isCurrent));
  node.querySelectorAll(".semi-icon").forEach((icon) => icon.setAttribute("aria-hidden", "true"));
}

/** Keep language and appearance separate, with one open menu and deliberate focus return. */
export function PreferencesMenu() {
  const { t, locale, setLocale } = useI18n();
  const [open, setOpen] = useState<"language" | "theme" | null>(null);
  const [theme, setTheme] = useState(() => document.documentElement.dataset.theme ?? "system");
  const [isSessionOnly, setSessionOnly] = useState(false);
  /** Close the active popup without losing the keyboard's trigger position. */
  function select(action: () => void) {
    const selected = open;
    setOpen(null);
    document
      .querySelector<HTMLButtonElement>(`button[data-preference="${selected}"]`)
      ?.focus();
    action();
  }
  /** Persist presentation only; unavailable storage does not prevent a session choice. */
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
    <div className="interface-preferences">
      {(["language", "theme"] as const).map((kind) => {
        const isOpen = open === kind;
        const label = t(kind === "language" ? "language.label" : "theme.label");
        return (
          <Tooltip
            key={kind}
            content={label}
            motion={false}
            trigger={isOpen ? "custom" : "hover"}
            {...(isOpen ? { visible: false } : {})}
          >
            <span data-exta-popup-open={isOpen} className="preferences-menu">
              <Dropdown
                showTick
                trigger="custom"
                visible={isOpen}
                motion={false}
                position="bottomRight"
                onClickOutSide={() => setOpen(null)}
                closeOnEsc
                onEscKeyDown={(event) => {
                  event.stopPropagation();
                  select(() => {});
                }}
                render={
                  <Dropdown.Menu>
                    <Dropdown.Title>{label}</Dropdown.Title>
                    {kind === "theme"
                      ? (["light", "dark", "system"] as const).map((value) => (
                          <Dropdown.Item
                            key={value}
                            active={theme === value}
                            ref={(node) => annotatePreference(node, theme === value)}
                            showTick
                            onClick={() => select(() => appearance(value))}
                          >
                            {t(`theme.${value}`)}
                          </Dropdown.Item>
                        ))
                      : (["zh", "en"] as const).map((value) => (
                          <Dropdown.Item
                            key={value}
                            active={locale === value}
                            ref={(node) => annotatePreference(node, locale === value)}
                            showTick
                            onClick={() => select(() => setLocale(value))}
                          >
                            {value === "zh" ? "简体中文" : "English"}
                          </Dropdown.Item>
                        ))}
                  </Dropdown.Menu>
                }
              >
                <Button
                  theme="borderless"
                  type="tertiary"
                  icon={
                    kind === "language" ? <IconLanguage aria-hidden /> : <IconSun aria-hidden />
                  }
                  aria-label={label}
                  data-preference={kind}
                  aria-haspopup="menu"
                  aria-expanded={isOpen}
                  onClick={() => setOpen(isOpen ? null : kind)}
                  onKeyDownCapture={(event) => {
                    if (event.key === "Enter" || event.key === " ") event.stopPropagation();
                  }}
                />
              </Dropdown>
            </span>
          </Tooltip>
        );
      })}
      {isSessionOnly ? (
        <span className="visually-hidden" role="status">
          {t("theme.sessionOnly")}
        </span>
      ) : null}
    </div>
  );
}
