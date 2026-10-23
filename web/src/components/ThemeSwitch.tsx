import { Select } from "./Controls";
/** Persistent appearance selection, independent of authentication and server state. */
import { useState } from "react";
import { useI18n } from "../lib/i18n";

/** Select a session-safe appearance, even when persistent storage is unavailable. */
export function ThemeSwitch() {
  const { t } = useI18n();
  const [theme, setTheme] = useState(() => document.documentElement.dataset.theme ?? "system");
  const [isSessionOnly, setSessionOnly] = useState(false);
  return (
    <div className="theme-control">
      <div>
        <span className="visually-hidden">{t("theme.label")}</span>
        <Select
          aria-label={t("theme.label")}
          value={theme}
          onChange={(event) => {
            const value = event.target.value;
            document.documentElement.dataset.theme = value;
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
          }}
        >
          <option value="light">{t("theme.light")}</option>
          <option value="dark">{t("theme.dark")}</option>
          <option value="system">{t("theme.system")}</option>
        </Select>
      </div>
      {isSessionOnly ? (
        <span className="field-hint" role="status">
          {t("theme.sessionOnly")}
        </span>
      ) : null}
    </div>
  );
}
