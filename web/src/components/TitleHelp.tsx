/** Compact, keyboard- and touch-accessible explanations beside selected headings. */
import { useState } from "react";
import Button from "@douyinfe/semi-ui/lib/es/button";
import Tooltip from "@douyinfe/semi-ui/lib/es/tooltip";
import IconHelpCircle from "@douyinfe/semi-icons/lib/es/icons/IconHelpCircle";
import { useI18n } from "../lib/i18n";

/** Use Semi's hover/focus handling, with an explicit touch opening and Escape dismissal. */
export function TitleHelp({ title, children }: { title: string; children: string }) {
  const { t } = useI18n();
  const [isOpen, setOpen] = useState(false);
  return (
    <Tooltip
      content={children}
      visible={isOpen}
      onVisibleChange={setOpen}
      onClickOutSide={() => setOpen(false)}
      motion={false}
      position="bottomLeft"
      guardFocus={false}
      closeOnEsc
      onEscKeyDown={() => setOpen(false)}
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
        onClick={() => setOpen(true)}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onKeyDown={(event) => {
          if (event.key === "Escape") setOpen(false);
        }}
      />
    </Tooltip>
  );
}
