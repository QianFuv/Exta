/** Fallback for unknown in-app routes. */
import { Link } from "react-router";
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
      <Link className="button" to="/overview">
        {t("nav.overview")}
      </Link>
    </>
  );
}
