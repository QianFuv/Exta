/** Generated from api/openapi.yaml; SHA256 b5511d5a4eccfa52b90b9230c567a71a1b5add4e6642c36f885ae843aba51d73. Do not edit. */
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

export const CONTRACT_SHA256 = "b5511d5a4eccfa52b90b9230c567a71a1b5add4e6642c36f885ae843aba51d73";
