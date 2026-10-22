/** Cursor pagination over contract list endpoints (`{ items, next_cursor }`). */
import { useInfiniteQuery } from "@tanstack/react-query";
import type { QueryKey } from "@tanstack/react-query";

/** A contract list page. */
export interface CursorPage<T> {
  items: T[];
  next_cursor: string | null;
}

/**
 * Load a cursor-paginated list. Pages are appended in server order; nothing is
 * re-sorted client-side.
 *
 * @param queryKey - Cache key including filters.
 * @param fetchPage - Fetches one page for an optional cursor.
 * @param options - Enable flag; disabled lists issue no request.
 * @returns Flattened items plus the infinite query state.
 */
export function useCursorList<P extends CursorPage<unknown>>(
  queryKey: QueryKey,
  fetchPage: (cursor: string | undefined) => Promise<P>,
  options: { enabled?: boolean } = {},
) {
  const query = useInfiniteQuery({
    queryKey,
    queryFn: ({ pageParam }) => fetchPage(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: options.enabled ?? true,
  });
  const items: P["items"] = query.data?.pages.flatMap((page) => page.items) ?? [];
  return { query, items };
}
