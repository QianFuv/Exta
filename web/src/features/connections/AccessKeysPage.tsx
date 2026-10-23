/** Service credentials authorize clients to call Exta, independently of upstream keys. */
import { PageHeader, RequirePermission } from "../../components/Page";
import { useI18n } from "../../lib/i18n";
import { TokensSection } from "./TokensSection";

/** Render the dedicated service-access credential page under its read boundary. */
export function AccessKeysPage() {
  const { t } = useI18n();
  return (
    <>
      <PageHeader title={t("keys.access")} help={t("keys.accessDescription")} />
      <RequirePermission permission="admin:read">
        <TokensSection />
      </RequirePermission>
    </>
  );
}
