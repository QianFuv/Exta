/** Generated from api/openapi.yaml; SHA256 fa6a05aa04fb9777c597808445ac0425d3843fcbdc76074866e7a737a2616280. Do not edit. */
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

export const CONTRACT_SHA256 = "fa6a05aa04fb9777c597808445ac0425d3843fcbdc76074866e7a737a2616280";
