import IconClose from "@douyinfe/semi-icons/lib/es/icons/IconClose";
/** Accessible modal dialog with focus containment, Escape handling and focus restore. */
import { useEffect, useId, useRef } from "react";
import type { ReactNode } from "react";
import Modal from "@douyinfe/semi-ui/lib/es/modal";
import SideSheet from "@douyinfe/semi-ui/lib/es/sideSheet";
import { Button } from "./Controls";
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
  const contentRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLElement | null>(null);
  const previousRef = useRef(
    document.activeElement instanceof HTMLElement ? document.activeElement : null,
  );
  const overflowRef = useRef(document.body.style.overflow);
  const closeRef = useRef({ onClose, isBusy });
  useEffect(() => {
    closeRef.current = { onClose, isBusy };
  }, [onClose, isBusy]);

  useEffect(() => {
    const panel = contentRef.current?.closest<HTMLElement>('[role="dialog"]') ?? null;
    panelRef.current = panel;
    if (!panel) return;
    panel.dataset.extaDialog = "true";
    panel.setAttribute("aria-modal", "true");
    panel.setAttribute("aria-labelledby", titleId);
    if (description) panel.setAttribute("aria-describedby", descriptionId);
    else panel.removeAttribute("aria-describedby");
    const background = Array.from(document.body.children).filter(
      (element) => !element.contains(panel),
    );
    const inertBefore = background.map((element) => element.hasAttribute("inert"));
    background.forEach((element) => element.setAttribute("inert", ""));
    const overflowBefore = overflowRef.current;
    document.body.style.overflow = "hidden";
    const controls = focusableControls(panel);
    (
      controls.find((element) => element.closest("[data-autofocus]")) ??
      controls[0] ??
      panel
    ).focus();
    /** Let an open nested popup consume Escape before dismissing its owner. */
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (!panel.contains(event.target as Node)) return;
      if (event.key === "Escape") {
        if (panel.querySelector('[data-exta-popup-open="true"]')) return;
        event.stopPropagation();
        if (!closeRef.current.isBusy) closeRef.current.onClose();
      }
      if (event.key !== "Tab") return;
      event.stopPropagation();
      const focusable = focusableControls(panel);
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (!first || !last) {
        event.preventDefault();
        panel.focus();
      } else if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKeyDown, true);
    const previous = previousRef.current;
    return () => {
      document.removeEventListener("keydown", onKeyDown, true);
      background.forEach((element, index) => {
        if (!inertBefore[index]) element.removeAttribute("inert");
      });
      document.body.style.overflow = overflowBefore;
      if (previous?.isConnected) previous.focus();
    };
  }, [titleId, descriptionId, description]);

  const content = (
    <div ref={contentRef} aria-busy={isBusy}>
      <header className="dialog-header">
        <h2 id={titleId}>{title}</h2>
        <Button
          className="button button-ghost button-icon"
          onClick={() => {
            if (!isBusy) onClose();
          }}
          disabled={isBusy}
          aria-label={t("common.close")}
        >
          <IconClose aria-hidden />
        </Button>
      </header>
      {description ? (
        <p id={descriptionId} className="dialog-description">
          {description}
        </p>
      ) : null}
      <div className="dialog-body">{children}</div>
    </div>
  );
  return variant === "drawer" ? (
    <SideSheet
      visible
      motion={false}
      keepDOM={false}
      maskClosable={false}
      closable={false}
      closeOnEsc={false}
      disableScroll
      width="min(760px, 100vw)"
      className="exta-drawer"
    >
      {content}
    </SideSheet>
  ) : (
    <Modal
      visible
      motion={false}
      keepDOM={false}
      maskClosable={false}
      closable={false}
      closeOnEsc={false}
      footer={null}
      width={`min(${size === "large" ? 760 : 560}px, calc(100vw - 32px))`}
      className="exta-modal"
    >
      {content}
    </Modal>
  );
}
