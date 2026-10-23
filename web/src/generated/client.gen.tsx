/** Generated from api/openapi.yaml; SHA256 91b6572d02e510dcabc063324c1afb7aeaff44446b910ae174ea0155a3f7eb11. Do not edit. */
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

export const CONTRACT_SHA256 = "91b6572d02e510dcabc063324c1afb7aeaff44446b910ae174ea0155a3f7eb11";
