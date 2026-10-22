/** Generated from api/openapi.yaml; SHA256 26c443aa7084fea8bcb6563660080f1081d81c3329f2a4d3843faa5107859a47. Do not edit. */
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

export const CONTRACT_SHA256 = "26c443aa7084fea8bcb6563660080f1081d81c3329f2a4d3843faa5107859a47";
