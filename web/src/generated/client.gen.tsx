/** Generated from api/openapi.yaml; SHA256 ffe9129cc3435499dea1750e1d96a1c3651cc30d75d739428c282c6e2aecf53d. Do not edit. */
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

export const CONTRACT_SHA256 = "ffe9129cc3435499dea1750e1d96a1c3651cc30d75d739428c282c6e2aecf53d";
