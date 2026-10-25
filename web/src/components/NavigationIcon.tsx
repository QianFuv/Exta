/** Official Semi line icons for the five administration destinations. */
import IconHistogram from "@douyinfe/semi-icons/lib/es/icons/IconHistogram";
import IconKeyStroked from "@douyinfe/semi-icons/lib/es/icons/IconKeyStroked";
import IconShieldStroked from "@douyinfe/semi-icons/lib/es/icons/IconShieldStroked";
import IconOrderedListStroked from "@douyinfe/semi-icons/lib/es/icons/IconOrderedListStroked";
import IconSettingStroked from "@douyinfe/semi-icons/lib/es/icons/IconSettingStroked";

const ICONS = {
  overview: IconHistogram,
  keys: IconKeyStroked,
  access: IconShieldStroked,
  requests: IconOrderedListStroked,
  settings: IconSettingStroked,
} as const;

/** Name of a navigation glyph. */
export type NavigationIconName = keyof typeof ICONS;

/**
 * Render a decorative navigation glyph; its adjacent label supplies the name.
 *
 * @param props - Destination icon name.
 * @returns Hidden icon element.
 */
export function NavigationIcon({ name }: { name: NavigationIconName }) {
  const Icon = ICONS[name];
  return <Icon aria-hidden className="nav-icon" />;
}
