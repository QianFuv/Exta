/** Generated from api/openapi.yaml; SHA256 e108c18ec715f618c40d2a41eedc662d224b56e6e523e74ae78d43ba34e2aaa3. Do not edit. */
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

export const CONTRACT_SHA256 = "e108c18ec715f618c40d2a41eedc662d224b56e6e523e74ae78d43ba34e2aaa3";
