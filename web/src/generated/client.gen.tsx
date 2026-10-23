/** Generated from api/openapi.yaml; SHA256 df7ad045a47e7e22ce929a680aae2c5fee67a43eec8803c196bfcf80407bbfb9. Do not edit. */
import createClient from "openapi-fetch";
import type { ClientOptions } from "openapi-fetch";
import type { paths } from "./types.gen";

/**
 * Create a same-origin client; callers supply contract-required CSRF/idempotency headers.
 * @param options - Optional base URL and fetch adapter for local integration tests.
 */
export function createApiClient(options: Pick<ClientOptions, "baseUrl" | "fetch"> = {}) {
  return createClient<paths>({ baseUrl: "", ...options, credentials: "same-origin" });
}

export const CONTRACT_SHA256 = "df7ad045a47e7e22ce929a680aae2c5fee67a43eec8803c196bfcf80407bbfb9";
