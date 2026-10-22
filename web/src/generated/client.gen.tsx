/** Generated from api/openapi.yaml; SHA256 7af1ecfa1053a374d44f6d8235f8ec0a3dac69953e8d55c3edb2853f6e3da512. Do not edit. */
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

export const CONTRACT_SHA256 = "7af1ecfa1053a374d44f6d8235f8ec0a3dac69953e8d55c3edb2853f6e3da512";
