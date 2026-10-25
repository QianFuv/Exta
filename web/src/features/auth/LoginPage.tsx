import { Button, Input } from "../../components/Controls";
/** Administrator password exchange for an HttpOnly admin session. */
import { useEffect, useId, useState } from "react";
import type { FormEvent } from "react";
import { Link, Navigate, useLocation, useNavigate } from "react-router";
import IconTick from "@douyinfe/semi-icons/lib/es/icons/IconTick";
import IconArrowLeft from "@douyinfe/semi-icons/lib/es/icons/IconArrowLeft";
import { useMutation } from "@tanstack/react-query";
import { call, useApi } from "../../lib/api";
import { useI18n } from "../../lib/i18n";
import { useSession } from "../../lib/session";
import { InlineError, Notice } from "../../components/Feedback";
import { PreferencesMenu } from "../../components/PreferencesMenu";
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
  const characterCount = Array.from(credential).length;
  const isValidPassword =
    characterCount >= 8 && characterCount <= 512 && !/[\r\n]/.test(credential);
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

  useEffect(() => {
    document.title = `${t("login.title")} · Exta`;
  }, [t]);

  if (status === "authenticated" && !login.isPending) {
    return <Navigate to={target} replace />;
  }

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!isValidPassword || login.isPending) {
      return;
    }
    login.mutate({ secret: credential });
  };

  return (
    <div className="login">
      <MockModeBanner />
      <header className="login-header">
        <Link className="login-home" to="/">
          <IconArrowLeft aria-hidden />
          <span>{t("login.backHome")}</span>
        </Link>
        <PreferencesMenu />
      </header>
      <div className="login-body">
        <main className="login-card" id="main">
          <div className="login-brand">
            <BrandMark size={36} />
            <span className="login-brand-name">Exta</span>
          </div>
          <h1>{t("login.title")}</h1>
          <p className="login-subtitle">{t("login.subtitle")}</p>
          {signedOutReason === "expired" ? (
            <Notice tone="warning" role="status" title={t("login.expired")} />
          ) : null}
          {signedOutReason === "logged_out" ? (
            <Notice tone="info" role="status" title={t("login.loggedOut")} />
          ) : null}
          <form onSubmit={onSubmit} className="form login-form">
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
              className="button button-primary login-submit"
              disabled={login.isPending || !isValidPassword}
              loading={login.isPending}
            >
              {login.isPending ? t("login.signingIn") : t("login.signIn")}
            </Button>
          </form>
          <p className="login-session-note">{t("login.sessionNote")}</p>
        </main>
        <aside className="login-aside" aria-label={t("home.eyebrow")}>
          <p className="login-aside-eyebrow">{t("home.eyebrow")}</p>
          <p className="login-aside-title">{t("login.asideTitle")}</p>
          <ul className="login-aside-points">
            {(
              ["login.asidePointKeys", "login.asidePointAccess", "login.asidePointLogs"] as const
            ).map((point) => (
              <li key={point}>
                <IconTick aria-hidden />
                {t(point)}
              </li>
            ))}
          </ul>
          <div className="login-aside-grid" aria-hidden="true" />
        </aside>
      </div>
    </div>
  );
}
