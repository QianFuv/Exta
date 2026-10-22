/** Small Chinese/English dictionary with locale-aware formatting; no localization service. */
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { EN_MESSAGES, ZH_MESSAGES } from "./messages";
import type { MessageKey } from "./messages";

/** Supported interface languages. Chinese is the product default. */
export type Locale = "zh" | "en";

const LOCALE_STORAGE_KEY = "exta.locale";

const DICTIONARIES: Record<Locale, Record<MessageKey, string>> = {
  zh: ZH_MESSAGES,
  en: EN_MESSAGES,
};

const INTL_LOCALES: Record<Locale, string> = { zh: "zh-CN", en: "en-US" };

/** Interpolation values for message placeholders such as `{count}`. */
export type MessageValues = Record<string, string | number>;

interface I18nValue {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: (key: MessageKey, values?: MessageValues) => string;
  formatDateTime: (value: string | null | undefined) => string;
  formatNumber: (value: number, maximumFractionDigits?: number) => string;
  formatDuration: (milliseconds: number | null | undefined) => string;
}

const I18nContext = createContext<I18nValue | null>(null);

/**
 * Read the saved non-secret language preference.
 *
 * @returns Saved locale or Chinese.
 */
function readInitialLocale(): Locale {
  try {
    const saved = window.localStorage.getItem(LOCALE_STORAGE_KEY);
    return saved === "en" || saved === "zh" ? saved : "zh";
  } catch {
    return "zh";
  }
}

/**
 * Replace `{name}` placeholders with values.
 *
 * @param template - Message template.
 * @param values - Placeholder values.
 * @returns Interpolated text.
 */
function interpolate(template: string, values: MessageValues | undefined): string {
  if (!values) {
    return template;
  }
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    name in values ? String(values[name]) : match,
  );
}

/**
 * Provide translation and formatting helpers.
 *
 * @param props - Optional fixed locale for tests and children.
 * @returns The provider element.
 */
export function I18nProvider({
  children,
  initialLocale,
}: {
  children: ReactNode;
  initialLocale?: Locale;
}) {
  const [locale, setLocaleState] = useState<Locale>(() => initialLocale ?? readInitialLocale());

  useEffect(() => {
    document.documentElement.lang = INTL_LOCALES[locale];
  }, [locale]);

  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next);
    try {
      window.localStorage.setItem(LOCALE_STORAGE_KEY, next);
    } catch {
      return;
    }
  }, []);

  const value = useMemo<I18nValue>(() => {
    const dictionary = DICTIONARIES[locale];
    const intlLocale = INTL_LOCALES[locale];
    const dateFormat = new Intl.DateTimeFormat(intlLocale, {
      dateStyle: "medium",
      timeStyle: "medium",
    });
    return {
      locale,
      setLocale,
      t: (key, values) => interpolate(dictionary[key], values),
      formatDateTime: (input) => {
        if (input === null || input === undefined) {
          return "—";
        }
        const date = new Date(input);
        return Number.isNaN(date.getTime()) ? input : dateFormat.format(date);
      },
      formatNumber: (input, maximumFractionDigits = 0) =>
        new Intl.NumberFormat(intlLocale, { maximumFractionDigits }).format(input),
      formatDuration: (milliseconds) => {
        if (milliseconds === null || milliseconds === undefined) {
          return "—";
        }
        if (milliseconds < 1000) {
          return `${milliseconds} ms`;
        }
        const seconds = milliseconds / 1000;
        if (seconds < 120) {
          return `${new Intl.NumberFormat(intlLocale, { maximumFractionDigits: 1 }).format(seconds)} s`;
        }
        return `${new Intl.NumberFormat(intlLocale, { maximumFractionDigits: 1 }).format(seconds / 60)} min`;
      },
    };
  }, [locale, setLocale]);

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

/**
 * Read translation helpers.
 *
 * @returns Locale, translation and formatting functions.
 */
export function useI18n(): I18nValue {
  const value = useContext(I18nContext);
  if (value === null) {
    throw new Error("I18nProvider is missing");
  }
  return value;
}
