import SemiNotice from "@douyinfe/semi-ui/lib/es/notification/notice";
import "@douyinfe/semi-foundation/lib/es/notification/notification.css";
/** Polite live-region announcements for completed actions. */
import { createContext, useCallback, useContext, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useI18n } from "../lib/i18n";
import type { Tone } from "./Feedback";

interface Notification {
  id: number;
  tone: Tone;
  message: string;
}

interface NotificationsValue {
  notify: (tone: Tone, message: string) => void;
}

const NotificationsContext = createContext<NotificationsValue | null>(null);

/**
 * Provide `notify` and render recent notifications in an ARIA live region.
 *
 * @param props - Children.
 * @returns Provider and region.
 */
export function NotificationsProvider({ children }: { children: ReactNode }) {
  const { t } = useI18n();
  const [items, setItems] = useState<Notification[]>([]);
  const nextId = useRef(1);

  const dismiss = useCallback((id: number) => {
    setItems((current) => current.filter((item) => item.id !== id));
  }, []);

  const notify = useCallback(
    (tone: Tone, message: string) => {
      const id = nextId.current;
      nextId.current += 1;
      setItems((current) => [...current.slice(-3), { id, tone, message }]);
      window.setTimeout(() => dismiss(id), 8000);
    },
    [dismiss],
  );

  const value = useMemo(() => ({ notify }), [notify]);

  return (
    <NotificationsContext.Provider value={value}>
      {children}
      <div
        className="toasts"
        role="status"
        aria-live="polite"
        aria-label={t("common.notifications")}
      >
        {items.map((item) => (
          <SemiNotice
            key={item.id}
            id={String(item.id)}
            type={
              item.tone === "danger" ? "error" : item.tone === "neutral" ? "default" : item.tone
            }
            content={item.message}
            duration={0}
            showClose
            close={() => dismiss(item.id)}
            onClose={() => dismiss(item.id)}
          />
        ))}
      </div>
    </NotificationsContext.Provider>
  );
}

/**
 * Read the notification function.
 *
 * @returns Notify callback.
 */
export function useNotify(): NotificationsValue["notify"] {
  const value = useContext(NotificationsContext);
  if (value === null) {
    throw new Error("NotificationsProvider is missing");
  }
  return value.notify;
}
