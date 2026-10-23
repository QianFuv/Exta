/** Resolve display names from already authorized key metadata without reading secrets. */
import { call, useApi } from "./api";
import { useCursorList } from "./useCursorList";

/** Return names for the loaded key inventory, retaining an honest ID fallback for missing keys. */
export function useKeyNames() {
  const api = useApi();
  const { items } = useCursorList(["keys", "all"], (cursor) =>
    call(
      api.GET("/v1/admin/keys", {
        params: { query: { limit: 100, ...(cursor ? { cursor } : {}) } },
      }),
    ),
  );
  return new Map(items.map((key) => [key.id, key.label]));
}
