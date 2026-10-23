import { Button } from "./Controls";
/** Chinese/English toggle. */
import { useI18n } from "../lib/i18n";

/**
 * Switch interface language; the choice is a non-secret local preference.
 *
 * @returns Language toggle button.
 */
export function LanguageSwitch() {
  const { locale, setLocale, t } = useI18n();
  const next = locale === "zh" ? "en" : "zh";
  return (
    <Button
      type="button"
      className="button button-ghost"
      onClick={() => setLocale(next)}
      lang={next === "en" ? "en" : "zh-CN"}
      aria-label={`${t("language.switchTo")}: ${next === "en" ? "English" : "中文"}`}
    >
      {next === "en" ? "English" : "中文"}
    </Button>
  );
}
