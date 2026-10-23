import { Button } from "./Controls";
/** Button that fetches the next cursor page. */
import { useI18n } from "../lib/i18n";
import { InlineError } from "./Feedback";

/**
 * Render a "load more" control while another page exists.
 *
 * @param props - Infinite query state.
 * @returns Button, error or nothing.
 */
export function LoadMore({
  query,
}: {
  query: {
    hasNextPage: boolean;
    isFetchingNextPage: boolean;
    isFetchNextPageError: boolean;
    error: unknown;
    fetchNextPage: () => unknown;
  };
}) {
  const { t } = useI18n();
  if (!query.hasNextPage) {
    return null;
  }
  return (
    <div className="load-more">
      {query.isFetchNextPageError ? <InlineError error={query.error} /> : null}
      <Button
        type="button"
        className="button"
        onClick={() => void query.fetchNextPage()}
        disabled={query.isFetchingNextPage}
      >
        {query.isFetchingNextPage ? t("common.loading") : t("common.loadMore")}
      </Button>
    </div>
  );
}
