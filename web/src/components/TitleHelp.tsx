/** Compact, keyboard- and touch-accessible explanations beside selected headings. */
import { useEffect, useRef, useState } from "react";
import Button from "@douyinfe/semi-ui/lib/es/button";
import Tooltip from "@douyinfe/semi-ui/lib/es/tooltip";
import IconHelpCircle from "@douyinfe/semi-icons/lib/es/icons/IconHelpCircle";
import { useI18n } from "../lib/i18n";

/** Own visibility so pointer hover cannot override keyboard or touch activation. */
export function TitleHelp({ title, children }: { title: string; children: string }) {
  const { t } = useI18n();
  const [isOpen, setOpen] = useState(false);
  const hasFocus = useRef(false);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(closeTimer.current), []);
  /** Cancel delayed pointer dismissal when focus or the popup keeps help in use. */
  function open() {
    clearTimeout(closeTimer.current);
    setOpen(true);
  }
  /** Allow the pointer to cross the gap between its trigger and the help bubble. */
  function leave() {
    clearTimeout(closeTimer.current);
    closeTimer.current = setTimeout(() => {
      if (!hasFocus.current) setOpen(false);
    }, 150);
  }
  /** Explicit dismissal takes precedence even while the trigger retains focus. */
  function close() {
    clearTimeout(closeTimer.current);
    setOpen(false);
  }
  return (
    <Tooltip
      trigger="custom"
      content={
        <span onMouseEnter={open} onMouseLeave={leave}>
          {children}
        </span>
      }
      visible={isOpen}
      onClickOutSide={close}
      motion={false}
      position="bottomLeft"
      guardFocus={false}
      closeOnEsc
      onEscKeyDown={close}
      className="title-help-content"
    >
      <Button
        className="title-help"
        theme="borderless"
        type="tertiary"
        size="small"
        icon={<IconHelpCircle aria-hidden />}
        aria-label={t("common.helpFor", { title })}
        aria-expanded={isOpen}
        onClick={open}
        onMouseEnter={open}
        onMouseLeave={leave}
        onFocus={() => {
          hasFocus.current = true;
          open();
        }}
        onBlur={() => {
          hasFocus.current = false;
          leave();
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape") close();
        }}
      />
    </Tooltip>
  );
}
