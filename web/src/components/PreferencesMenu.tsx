/** Shared appearance control for both sides of authentication. */
import { useRef, useState } from "react";
import Dropdown from "@douyinfe/semi-ui/lib/es/dropdown";
import Button from "@douyinfe/semi-ui/lib/es/button";
import Tooltip from "@douyinfe/semi-ui/lib/es/tooltip";
import IconSun from "@douyinfe/semi-icons/lib/es/icons/IconSun";
import { useI18n } from "../lib/i18n";

/** Semi forwards refs but omits arbitrary ARIA props on menu items. */
function annotatePreference(node: HTMLLIElement | null, isCurrent: boolean) {
  if (!node) return;
  node.setAttribute("aria-current", String(isCurrent));
  node.querySelectorAll(".semi-icon").forEach((icon) => icon.setAttribute("aria-hidden", "true"));
}

/** One appearance menu with deliberate focus return; presentation only. */
export function PreferencesMenu() {
  const { t } = useI18n();
  const [isOpen, setOpen] = useState(false);
  const [theme, setTheme] = useState(() => document.documentElement.dataset.theme ?? "system");
  const [isSessionOnly, setSessionOnly] = useState(false);
  const host = useRef<HTMLSpanElement>(null);
  const label = t("theme.label");
  /** Close the popup without losing the keyboard's trigger position. */
  function select(action: () => void) {
    setOpen(false);
    host.current?.querySelector<HTMLButtonElement>("button")?.focus();
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
      <Tooltip
        content={label}
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
                <Dropdown.Title>{label}</Dropdown.Title>
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
              </Dropdown.Menu>
            }
          >
            <Button
              theme="borderless"
              type="tertiary"
              icon={<IconSun aria-hidden />}
              aria-label={label}
              data-preference="theme"
              aria-haspopup="menu"
              aria-expanded={isOpen}
              onClick={() => setOpen(!isOpen)}
              onKeyDownCapture={(event) => {
                if (event.key === "Enter" || event.key === " ") event.stopPropagation();
              }}
            />
          </Dropdown>
        </span>
      </Tooltip>
      {isSessionOnly ? (
        <span className="visually-hidden" role="status">
          {t("theme.sessionOnly")}
        </span>
      ) : null}
    </div>
  );
}
