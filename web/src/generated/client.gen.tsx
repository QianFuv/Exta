/** Generated from api/openapi.yaml; SHA256 02ea761879aed54219f4b929db56a68e1ee744c8fbfed82871d25d11c8483954. Do not edit. */
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

export const CONTRACT_SHA256 = "02ea761879aed54219f4b929db56a68e1ee744c8fbfed82871d25d11c8483954";
