/** Typed access to the frozen Exta contract through the generated same-origin client. */
import { createContext, useContext } from "react";
import { createApiClient } from "../generated/client.gen";
import type { components } from "../generated/types.gen";

/** Generated schema namespace shortcut used by feature modules. */
export type Schemas = components["schemas"];

/** Contract client instance type created by the generated factory. */
export type ApiClient = ReturnType<typeof createApiClient>;

/** Error detail delivered by every non-2xx Exta response. */
export type ErrorDetail = Schemas["ErrorDetail"];

/** 5xx codes the contract defines as refusals before admission or any side effect. */
const DEFINITIVE_SERVER_REFUSALS: ReadonlySet<ErrorDetail["code"]> = new Set([
  "capacity_unavailable",
  "provider_unavailable",
  "quota_unavailable",
  "secret_store_locked",
]);

/** Classifies how a request failed so the UI can avoid claiming certainty it does not have. */
export type FailureKind = "http" | "network" | "malformed";

/**
 * A failed contract call. Network failures carry status 0 and no detail because the
 * outcome of a mutation is unknown when no response arrived.
 */
export class ApiFailure extends Error {
  readonly status: number;
  readonly detail: ErrorDetail | null;
  readonly kind: FailureKind;

  /**
   * Create a failure description.
   *
   * @param kind - Transport classification.
   * @param status - HTTP status, or 0 when no response was received.
   * @param detail - Parsed Exta error detail when the response was well formed.
   */
  constructor(kind: FailureKind, status: number, detail: ErrorDetail | null) {
    super(detail?.message ?? `Request failed (${kind}, ${status})`);
    this.name = "ApiFailure";
    this.kind = kind;
    this.status = status;
    this.detail = detail;
  }

  /** The Exta error code, when the server supplied one. */
  get code(): ErrorDetail["code"] | null {
    return this.detail?.code ?? null;
  }

  /**
   * Whether a mutation with this failure may have been applied by the server. No
   * response, unparseable responses and server failures other than explicit
   * pre-admission refusals are treated as unknown outcomes.
   */
  get isOutcomeUnknown(): boolean {
    if (this.kind !== "http") {
      return true;
    }
    if (this.status < 500) {
      return false;
    }
    return this.code === null || !DEFINITIVE_SERVER_REFUSALS.has(this.code);
  }
}

/** Shape returned by openapi-fetch for every generated operation. */
interface FetchResult<T> {
  data?: T;
  error?: unknown;
  response: Response;
}

/**
 * Check that an unknown error body matches the contract's ApiError envelope.
 *
 * @param value - Parsed response body.
 * @returns The error detail or null when the body is not an Exta error.
 */
function readErrorDetail(value: unknown): ErrorDetail | null {
  if (typeof value !== "object" || value === null || !("error" in value)) {
    return null;
  }
  const detail = (value as { error: unknown }).error;
  if (
    typeof detail === "object" &&
    detail !== null &&
    typeof (detail as { code?: unknown }).code === "string" &&
    typeof (detail as { message?: unknown }).message === "string"
  ) {
    return detail as ErrorDetail;
  }
  return null;
}

/**
 * Await a generated client call and return its data with the HTTP status, or throw
 * an {@link ApiFailure}.
 *
 * @param request - Pending generated client call.
 * @returns Response body and status code.
 */
export async function callWithStatus<T>(
  request: Promise<FetchResult<T>>,
): Promise<{ data: T; status: number }> {
  let result: FetchResult<T>;
  try {
    result = await request;
  } catch {
    throw new ApiFailure("network", 0, null);
  }
  const { response } = result;
  if (!response.ok) {
    const detail = readErrorDetail(result.error);
    throw new ApiFailure(detail === null ? "malformed" : "http", response.status, detail);
  }
  if (result.data === undefined) {
    throw new ApiFailure("malformed", response.status, null);
  }
  return { data: result.data, status: response.status };
}

/**
 * Await a generated client call and return only its data.
 *
 * @param request - Pending generated client call.
 * @returns Response body.
 */
export async function call<T>(request: Promise<FetchResult<T>>): Promise<T> {
  return (await callWithStatus(request)).data;
}

const ApiContext = createContext<ApiClient | null>(null);

/** Provides the contract client to the component tree. */
export const ApiProvider = ApiContext.Provider;

/**
 * Read the contract client provided by {@link ApiProvider}.
 *
 * @returns The generated client.
 */
export function useApi(): ApiClient {
  const client = useContext(ApiContext);
  if (client === null) {
    throw new Error("ApiProvider is missing");
  }
  return client;
}

/**
 * Create the production client. Browser requests stay same-origin with cookies.
 *
 * @returns A generated client without test adapters.
 */
export function createBrowserApiClient(): ApiClient {
  return createApiClient();
}
