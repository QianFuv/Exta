/** Fallback for unknown in-app routes. */
import { Link } from "react-router";
import IconArrowRight from "@douyinfe/semi-icons/lib/es/icons/IconArrowRight";
import { useI18n } from "../lib/i18n";
import { PageHeader } from "../components/Page";

/**
 * Not-found page inside the shell.
 *
 * @returns Page element.
 */
export function NotFoundPage() {
  const { t } = useI18n();
  return (
    <>
      <PageHeader title={t("notFound.title")} description={t("notFound.description")} />
      <div className="not-found">
        <span className="not-found-code" aria-hidden="true">
          404
        </span>
        <div className="not-found-actions">
          <Link className="link-button link-button-primary" to="/overview">
            {t("nav.overview")}
            <IconArrowRight aria-hidden />
          </Link>
          <Link className="link-button" to="/">
            {t("nav.home")}
          </Link>
        </div>
      </div>
    </>
  );
}
