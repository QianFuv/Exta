/** Accessible modal dialog with focus containment, Escape handling and focus restore. */
import { useEffect, useId, useRef } from "react";
import type { KeyboardEvent, ReactNode } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../lib/i18n";

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** Exclude hidden or explicitly unfocusable controls from the keyboard cycle. */
function focusableControls(panel: HTMLElement | null) {
  return Array.from(panel?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? []).filter((element) => {
    if (element.tabIndex < 0 || element.closest("[hidden], [inert]")) return false;
    for (
      let ancestor: HTMLElement | null = element;
      ancestor !== null;
      ancestor = ancestor.parentElement
    ) {
      const style = getComputedStyle(ancestor);
      if (style.display === "none" || style.visibility === "hidden") return false;
      if (
        ancestor.tagName === "DETAILS" &&
        !ancestor.hasAttribute("open") &&
        !element.closest("summary")
      )
        return false;
    }
    return true;
  });
}

/**
 * Modal dialog. Focus moves into the dialog on open, Tab cycles inside it, Escape closes
 * it unless `isBusy`, and focus returns to the previously focused element on close.
 *
 * @param props - Title, optional description, close callback, busy flag and content.
 * @returns Portal-rendered dialog.
 */
export function Dialog({
  title,
  description,
  onClose,
  isBusy = false,
  children,
  size = "medium",
  variant = "modal",
}: {
  title: string;
  description?: string;
  onClose: () => void;
  isBusy?: boolean;
  children: ReactNode;
  size?: "medium" | "large";
  variant?: "modal" | "drawer";
}) {
  const { t } = useI18n();
  const titleId = useId();
  const descriptionId = useId();
  const panelRef = useRef<HTMLDivElement>(null);
  const onCloseRef = useRef(onClose);

  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const panel = panelRef.current;
    const background = Array.from(document.body.children).filter(
      (element) => !element.contains(panel),
    );
    const inertBefore = background.map((element) => element.hasAttribute("inert"));
    background.forEach((element) => element.setAttribute("inert", ""));
    const overflowBefore = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const controls = focusableControls(panel);
    const first = controls.find((element) => element.hasAttribute("data-autofocus")) ?? controls[0];
    (first ?? panel)?.focus();
    return () => {
      background.forEach((element, index) => {
        if (!inertBefore[index]) element.removeAttribute("inert");
      });
      document.body.style.overflow = overflowBefore;
      previous?.focus();
    };
  }, []);

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Escape") {
      event.stopPropagation();
      if (!isBusy) {
        onCloseRef.current();
      }
      return;
    }
    if (event.key !== "Tab" || panelRef.current === null) {
      return;
    }
    const focusable = focusableControls(panelRef.current);
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (first === undefined || last === undefined) {
      event.preventDefault();
      return;
    }
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  };

  return createPortal(
    <div className={`dialog-backdrop ${variant === "drawer" ? "drawer-backdrop" : ""}`}>
      <div
        ref={panelRef}
        className={`dialog dialog-${size}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description === undefined ? undefined : descriptionId}
        aria-busy={isBusy}
        tabIndex={-1}
        onKeyDown={onKeyDown}
      >
        <header className="dialog-header">
          <h2 id={titleId}>{title}</h2>
          <button
            type="button"
            className="button button-ghost button-icon"
            onClick={onClose}
            disabled={isBusy}
            aria-label={t("common.close")}
          >
            ×
          </button>
        </header>
        {description === undefined ? null : (
          <p id={descriptionId} className="dialog-description">
            {description}
          </p>
        )}
        <div className="dialog-body">{children}</div>
      </div>
    </div>,
    document.body,
  );
}
