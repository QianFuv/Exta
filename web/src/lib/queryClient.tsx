/** Query cache defaults that never resubmit mutations and avoid retrying definitive errors. */
import { QueryClient } from "@tanstack/react-query";
import { ApiFailure } from "./api";

/**
 * Decide whether a failed read should be retried automatically.
 *
 * @param failureCount - Attempts already made.
 * @param error - The failure.
 * @returns True for at most two retries of transport or 5xx failures.
 */
function shouldRetryRead(failureCount: number, error: unknown): boolean {
  if (failureCount >= 2) {
    return false;
  }
  if (error instanceof ApiFailure) {
    return (
      error.kind === "network" || (error.status >= 500 && error.code !== "secret_store_locked")
    );
  }
  return false;
}

/**
 * Create the application query client.
 *
 * @param options - Disable read retries in deterministic tests.
 * @returns A configured query client.
 */
export function createAppQueryClient(options: { retryReads?: boolean } = {}): QueryClient {
  const retryReads = options.retryReads ?? true;
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: retryReads ? shouldRetryRead : false,
        staleTime: 10_000,
        refetchOnWindowFocus: false,
      },
      mutations: {
        retry: false,
      },
    },
  });
}
