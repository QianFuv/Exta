/**
 * Administrative session state. The session (including its CSRF token) lives only in
 * React memory; it is re-read from `GET /v1/admin/session` after every reload and is
 * never written to browser storage.
 */
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiFailure, call, useApi } from "./api";
import type { Schemas } from "./api";

/** Closed admin permission names from the contract. */
export type AdminPermission = Schemas["AdminPermission"];

/** Current authentication status for routing decisions. */
export type SessionStatus = "loading" | "authenticated" | "anonymous" | "error";

/** Reason the operator was returned to the login page. */
export type SignedOutReason = "expired" | "logged_out" | null;

interface SessionContextValue {
  status: SessionStatus;
  session: Schemas["Session"] | null;
  loadError: ApiFailure | null;
  signedOutReason: SignedOutReason;
  /** Store a session returned by login. */
  establish: (session: Schemas["Session"]) => void;
  /** Mark the in-memory session as no longer valid and drop cached admin data. */
  expire: (reason: Exclude<SignedOutReason, null>) => void;
  /** Re-run the session lookup after a non-authentication failure. */
  retry: () => void;
  /** Check a stable permission granted to the current principal. */
  can: (permission: AdminPermission) => boolean;
  /** CSRF header value for admin mutations; empty when signed out. */
  csrfToken: string;
}

const SessionContext = createContext<SessionContextValue | null>(null);

/** Query key for the session lookup; excluded from global unauthorized handling. */
export const SESSION_QUERY_KEY = ["session"] as const;

/**
 * Load and hold the administrative session for descendants.
 *
 * @param props - Provider children.
 * @returns The provider element.
 */
export function SessionProvider({ children }: { children: ReactNode }) {
  const api = useApi();
  const queryClient = useQueryClient();
  const [override, setOverride] = useState<
    { kind: "session"; session: Schemas["Session"] } | { kind: "anonymous" } | null
  >(null);
  const [signedOutReason, setSignedOutReason] = useState<SignedOutReason>(null);

  const lookup = useQuery({
    queryKey: SESSION_QUERY_KEY,
    queryFn: async (): Promise<Schemas["Session"] | null> => {
      try {
        return await call(api.GET("/v1/admin/session"));
      } catch (error) {
        if (error instanceof ApiFailure && error.status === 401) {
          return null;
        }
        throw error;
      }
    },
    retry: false,
    staleTime: Infinity,
    gcTime: Infinity,
    refetchOnWindowFocus: false,
  });

  const establish = useCallback(
    (session: Schemas["Session"]) => {
      setSignedOutReason(null);
      setOverride({ kind: "session", session });
    },
    [setOverride],
  );

  const expire = useCallback(
    (reason: Exclude<SignedOutReason, null>) => {
      setOverride({ kind: "anonymous" });
      setSignedOutReason(reason);
      queryClient.removeQueries({ predicate: (query) => query.queryKey[0] !== "session" });
    },
    [queryClient],
  );

  useEffect(() => {
    const isSessionLoss = (error: unknown) =>
      error instanceof ApiFailure && error.status === 401 && error.code !== "invalid_token";
    const onLoss = () => queueMicrotask(() => expire("expired"));
    const stopQueries = queryClient.getQueryCache().subscribe((event) => {
      if (
        event.type === "updated" &&
        event.action.type === "error" &&
        event.query.queryKey[0] !== "session" &&
        isSessionLoss(event.action.error)
      ) {
        onLoss();
      }
    });
    const stopMutations = queryClient.getMutationCache().subscribe((event) => {
      if (
        event.type === "updated" &&
        event.action.type === "error" &&
        isSessionLoss(event.action.error)
      ) {
        onLoss();
      }
    });
    return () => {
      stopQueries();
      stopMutations();
    };
  }, [queryClient, expire]);

  const { refetch } = lookup;
  const retry = useCallback(() => {
    void refetch();
  }, [refetch]);

  const value = useMemo<SessionContextValue>(() => {
    let status: SessionStatus;
    let session: Schemas["Session"] | null = null;
    if (override?.kind === "session") {
      status = "authenticated";
      session = override.session;
    } else if (override?.kind === "anonymous") {
      status = "anonymous";
    } else if (lookup.isPending) {
      status = "loading";
    } else if (lookup.isError) {
      status = "error";
    } else if (lookup.data) {
      status = "authenticated";
      session = lookup.data;
    } else {
      status = "anonymous";
    }
    const permissions = new Set(session?.permissions ?? []);
    return {
      status,
      session,
      loadError: lookup.error instanceof ApiFailure ? lookup.error : null,
      signedOutReason,
      establish,
      expire,
      retry,
      can: (permission) => permissions.has(permission),
      csrfToken: session?.csrf_token ?? "",
    };
  }, [
    override,
    lookup.isPending,
    lookup.isError,
    lookup.data,
    lookup.error,
    signedOutReason,
    establish,
    expire,
    retry,
  ]);

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

/**
 * Read the current administrative session.
 *
 * @returns Session context value.
 */
export function useSession(): SessionContextValue {
  const value = useContext(SessionContext);
  if (value === null) {
    throw new Error("SessionProvider is missing");
  }
  return value;
}

/**
 * Read the CSRF header object for an admin mutation.
 *
 * @returns Header record for generated mutation calls.
 */
export function useCsrfHeader(): { "X-Exta-CSRF": string } {
  return { "X-Exta-CSRF": useSession().csrfToken };
}
