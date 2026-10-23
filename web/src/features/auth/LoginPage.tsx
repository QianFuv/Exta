import { Button, Input } from "../../components/Controls";
/** Administrator password exchange for an HttpOnly admin session. */
import { useId, useState } from "react";
import type { FormEvent } from "react";
import { Navigate, useLocation, useNavigate } from "react-router";
import { useMutation } from "@tanstack/react-query";
import { call, useApi } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useSession } from "../../lib/session";
import { InlineError, Notice } from "../../components/Feedback";
import { LanguageSwitch } from "../../components/LanguageSwitch";
import { ThemeSwitch } from "../../components/ThemeSwitch";
import { BrandMark } from "../../components/BrandMark";
import { MockModeBanner } from "../../components/Layout";

/**
 * Login page. The credential exists only in this form's state and is cleared after
 * each attempt; the resulting session is held in memory.
 *
 * @returns Page element.
 */
export function LoginPage() {
  const { t } = useI18n();
  const api = useApi();
  const navigate = useNavigate();
  const location = useLocation();
  const { status, establish, signedOutReason } = useSession();
  const [credential, setCredential] = useState("");
  const fieldId = useId();
  const from = (location.state as { from?: unknown } | null)?.from;
  const target =
    typeof from === "string" && from.startsWith("/") && !from.startsWith("/login")
      ? from
      : "/overview";

  const login = useMutation({
    gcTime: 0,
    mutationFn: (value: { secret: string }) =>
      call(api.POST("/v1/admin/session", { body: { credential: value.secret } })),
    onSuccess: (session) => {
      establish(session);
      void navigate(target, { replace: true });
    },
    onSettled: (...settled) => {
      settled[2].secret = "";
      setCredential("");
    },
  });

  if (status === "authenticated" && !login.isPending) {
    return <Navigate to={target} replace />;
  }

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (credential.length === 0 || login.isPending) {
      return;
    }
    login.mutate({ secret: credential });
  };

  return (
    <div className="login">
      <MockModeBanner />
      <header className="login-language">
        <ThemeSwitch />
        <LanguageSwitch />
      </header>
      <main className="login-card" id="main">
        <div className="login-brand">
          <BrandMark />
          <h1>Exta</h1>
        </div>
        <p className="muted">{t("login.subtitle")}</p>
        {signedOutReason === "expired" ? (
          <Notice tone="warning" role="status" title={t("login.expired")} />
        ) : null}
        {signedOutReason === "logged_out" ? (
          <Notice tone="info" role="status" title={t("login.loggedOut")} />
        ) : null}
        <form onSubmit={onSubmit} className="form">
          <div className="field">
            <label id={fieldId + "-label"} htmlFor={fieldId}>
              {t("login.credential")}
            </label>
            <Input
              id={fieldId}
              type="password"
              autoComplete="current-password"
              value={credential}
              onChange={(event) => setCredential(event.target.value)}
              required
              aria-describedby={`${fieldId}-hint`}
            />
            <p id={`${fieldId}-hint`} className="field-hint">
              {t("login.credentialHint")}
            </p>
          </div>
          {login.isError ? <InlineError error={login.error} /> : null}
          <Button
            type="submit"
            className="button button-primary"
            disabled={login.isPending || credential.length === 0}
            loading={login.isPending}
          >
            {login.isPending ? t("login.signingIn") : t("login.signIn")}
          </Button>
        </form>
      </main>
    </div>
  );
}
