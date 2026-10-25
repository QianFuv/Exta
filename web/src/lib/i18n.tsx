/** Simplified Chinese interface text with zh-CN formatting; no runtime language switch. */
import { createContext, useContext, useEffect, useMemo } from "react";
import type { ReactNode } from "react";
import { MESSAGES } from "./messages";
import type { MessageKey } from "./messages";

/** Intl locale tag used for every date, number and unit format. */
export const INTL_LOCALE = "zh-CN";

/** Interpolation values for message placeholders such as `{count}`. */
export type MessageValues = Record<string, string | number>;

interface I18nValue {
  t: (key: MessageKey, values?: MessageValues) => string;
  formatDateTime: (value: string | null | undefined) => string;
  formatNumber: (value: number, maximumFractionDigits?: number) => string;
  formatDuration: (milliseconds: number | null | undefined) => string;
}

const I18nContext = createContext<I18nValue | null>(null);

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
 * @param props - Children.
 * @returns The provider element.
 */
export function I18nProvider({ children }: { children: ReactNode }) {
  useEffect(() => {
    document.documentElement.lang = INTL_LOCALE;
  }, []);

  const value = useMemo<I18nValue>(() => {
    const dateFormat = new Intl.DateTimeFormat(INTL_LOCALE, {
      dateStyle: "medium",
      timeStyle: "medium",
    });
    const decimal = new Intl.NumberFormat(INTL_LOCALE, { maximumFractionDigits: 1 });
    return {
      t: (key, values) => interpolate(MESSAGES[key], values),
      formatDateTime: (input) => {
        if (input === null || input === undefined) {
          return "—";
        }
        const date = new Date(input);
        return Number.isNaN(date.getTime()) ? input : dateFormat.format(date);
      },
      formatNumber: (input, maximumFractionDigits = 0) =>
        new Intl.NumberFormat(INTL_LOCALE, { maximumFractionDigits }).format(input),
      formatDuration: (milliseconds) => {
        if (milliseconds === null || milliseconds === undefined) {
          return "—";
        }
        if (milliseconds < 1000) {
          return `${milliseconds} ms`;
        }
        const seconds = milliseconds / 1000;
        if (seconds < 120) {
          return `${decimal.format(seconds)} s`;
        }
        return `${decimal.format(seconds / 60)} min`;
      },
    };
  }, []);

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

/**
 * Read translation helpers.
 *
 * @returns Translation and formatting functions.
 */
export function useI18n(): I18nValue {
  const value = useContext(I18nContext);
  if (value === null) {
    throw new Error("I18nProvider is missing");
  }
  return value;
}
