/**
 * Public product home. Every statement describes behaviour implemented in this build; it
 * shows no metrics, customer claims or certifications, and makes no requests beyond the
 * shared session lookup that selects the console entry.
 */
import { useEffect } from "react";
import type { MouseEvent, ReactNode } from "react";
import { Link } from "react-router";
import Tabs from "@douyinfe/semi-ui/lib/es/tabs";
import IconArrowRight from "@douyinfe/semi-icons/lib/es/icons/IconArrowRight";
import IconTick from "@douyinfe/semi-icons/lib/es/icons/IconTick";
import IconKeyStroked from "@douyinfe/semi-icons/lib/es/icons/IconKeyStroked";
import IconShieldStroked from "@douyinfe/semi-icons/lib/es/icons/IconShieldStroked";
import IconRoute from "@douyinfe/semi-icons/lib/es/icons/IconRoute";
import IconOrderedListStroked from "@douyinfe/semi-icons/lib/es/icons/IconOrderedListStroked";
import { BrandMark } from "../../components/BrandMark";
import { MockModeBanner } from "../../components/Layout";
import { PreferencesMenu } from "../../components/PreferencesMenu";
import { CopyButton } from "../../components/Values";
import { useI18n } from "../../lib/i18n";
import type { MessageKey } from "../../lib/messages";
import { STATIC_TARGETS } from "../../lib/routeTargets";
import { useSession } from "../../lib/session";

const OPERATIONS = [
  "search",
  "advanced_search",
  "fetch",
  "map",
  "crawl",
  "research_create",
] as const;

const SECTIONS = [
  { id: "capabilities", label: "home.nav.capabilities" },
  { id: "console", label: "home.nav.console" },
  { id: "boundaries", label: "home.nav.boundaries" },
  { id: "deploy", label: "home.nav.deploy" },
] as const satisfies ReadonlyArray<{ id: string; label: MessageKey }>;

const CONSOLE_AREAS = [
  { title: "home.console.keys", text: "home.console.keysText", icon: <IconKeyStroked /> },
  { title: "home.console.access", text: "home.console.accessText", icon: <IconShieldStroked /> },
  { title: "home.console.routing", text: "home.console.routingText", icon: <IconRoute /> },
  { title: "home.console.logs", text: "home.console.logsText", icon: <IconOrderedListStroked /> },
] as const satisfies ReadonlyArray<{ title: MessageKey; text: MessageKey; icon: ReactNode }>;

const BOUNDARIES = [
  "home.boundaries.session",
  "home.boundaries.secrets",
  "home.boundaries.content",
  "home.boundaries.network",
  "home.boundaries.assets",
  "home.boundaries.balances",
] as const satisfies readonly MessageKey[];

/**
 * Scroll to an in-page section and move focus there, without a router navigation.
 *
 * @param event - Anchor click.
 * @param id - Target section ID.
 */
function jumpTo(event: MouseEvent<HTMLAnchorElement>, id: string) {
  const target = document.getElementById(id);
  if (!target) return;
  event.preventDefault();
  const isReduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? true;
  target.scrollIntoView?.({ behavior: isReduced ? "auto" : "smooth", block: "start" });
  target.focus({ preventScroll: true });
}

/**
 * Product home page served at `/app/`.
 *
 * @returns Page element.
 */
export function HomePage() {
  const { t } = useI18n();
  const { status } = useSession();
  const isSignedIn = status === "authenticated";
  const consoleTarget = isSignedIn ? "/overview" : "/login";
  const consoleLabel = isSignedIn ? t("home.ctaConsole") : t("home.ctaSignIn");
  useEffect(() => {
    document.title = `Exta · ${t("home.eyebrow")}`;
  }, [t]);

  return (
    <div className="home">
      <a className="skip-link" href="#main">
        {t("common.skipToContent")}
      </a>
      <MockModeBanner />
      <header className="home-header">
        <div className="home-header-inner">
          <Link className="home-brand" to="/" aria-label={`Exta · ${t("nav.home")}`}>
            <BrandMark />
            <span aria-hidden="true">Exta</span>
          </Link>
          <nav className="home-sections" aria-label={t("home.navLabel")}>
            <ul>
              {SECTIONS.map((section) => (
                <li key={section.id}>
                  <a href={`#${section.id}`} onClick={(event) => jumpTo(event, section.id)}>
                    {t(section.label)}
                  </a>
                </li>
              ))}
            </ul>
          </nav>
          <div className="home-header-actions">
            <PreferencesMenu />
            <Link className="link-button link-button-primary link-button-small" to={consoleTarget}>
              {consoleLabel}
            </Link>
          </div>
        </div>
      </header>
      <main id="main" tabIndex={-1} className="home-main">
        <section className="hero" aria-labelledby="hero-title">
          <div className="hero-grid" aria-hidden="true" />
          <div className="hero-inner">
            <div className="hero-copy">
              <p className="hero-eyebrow">
                <span className="hero-eyebrow-dot" aria-hidden="true" />
                {t("home.eyebrow")}
              </p>
              <h1 id="hero-title">{t("home.title")}</h1>
              <p className="hero-lead">{t("home.lead")}</p>
              <div className="hero-actions">
                <Link
                  className="link-button link-button-primary link-button-large"
                  to={consoleTarget}
                >
                  {consoleLabel}
                  <IconArrowRight aria-hidden />
                </Link>
                <a
                  className="link-button link-button-large"
                  href="#integrate"
                  onClick={(event) => jumpTo(event, "integrate")}
                >
                  {t("home.ctaIntegrate")}
                </a>
              </div>
              <ul className="hero-facts">
                {(
                  [
                    "home.fact.protocols",
                    "home.fact.binary",
                    "home.fact.targets",
                    "home.fact.local",
                  ] as const
                ).map((fact) => (
                  <li key={fact}>
                    <IconTick aria-hidden />
                    {t(fact)}
                  </li>
                ))}
              </ul>
            </div>
            <FlowVisual />
          </div>
        </section>

        <section
          id="capabilities"
          className="home-section"
          tabIndex={-1}
          aria-labelledby="capabilities-title"
        >
          <div className="home-section-inner home-split">
            <header className="home-section-heading">
              <p className="home-kicker">01</p>
              <h2 id="capabilities-title">{t("home.capabilities.title")}</h2>
              <p>{t("home.capabilities.lead")}</p>
            </header>
            <ul className="operation-list" aria-label={t("home.capabilities.title")}>
              {OPERATIONS.map((operation) => (
                <li key={operation} className="operation-row reveal">
                  <div className="operation-head">
                    <code className="operation-code">{operation}</code>
                    <p>{t(`home.op.${operation}`)}</p>
                  </div>
                  <ul className="profile-chips" aria-label={t("home.capabilities.profiles")}>
                    {STATIC_TARGETS[operation].map((target) => (
                      <li
                        key={`${target.provider}-${target.profile}`}
                        className={`profile-chip profile-${target.provider}`}
                      >
                        <span className="profile-provider">{t(`provider.${target.provider}`)}</span>
                        <span className="profile-name">{target.profile}</span>
                      </li>
                    ))}
                  </ul>
                </li>
              ))}
            </ul>
          </div>
        </section>

        <section
          id="console"
          className="home-section home-section-muted"
          tabIndex={-1}
          aria-labelledby="console-title"
        >
          <div className="home-section-inner">
            <header className="home-section-heading home-section-heading-wide">
              <p className="home-kicker">02</p>
              <h2 id="console-title">{t("home.console.title")}</h2>
              <p>{t("home.console.lead")}</p>
            </header>
            <ol className="console-areas">
              {CONSOLE_AREAS.map((area, index) => (
                <li key={area.title} className="console-area reveal">
                  <span className="console-area-index" aria-hidden="true">
                    {String(index + 1).padStart(2, "0")}
                  </span>
                  <span className="console-area-icon" aria-hidden="true">
                    {area.icon}
                  </span>
                  <h3>{t(area.title)}</h3>
                  <p>{t(area.text)}</p>
                </li>
              ))}
            </ol>
          </div>
        </section>

        <section
          id="boundaries"
          className="home-section home-section-inverse"
          tabIndex={-1}
          aria-labelledby="boundaries-title"
        >
          <div className="home-section-inner home-split">
            <header className="home-section-heading">
              <p className="home-kicker">03</p>
              <h2 id="boundaries-title">{t("home.boundaries.title")}</h2>
              <p>{t("home.boundaries.lead")}</p>
            </header>
            <ul className="boundary-list">
              {BOUNDARIES.map((item) => (
                <li key={item} className="reveal">
                  <span className="boundary-mark" aria-hidden="true">
                    <IconTick />
                  </span>
                  {t(item)}
                </li>
              ))}
            </ul>
          </div>
        </section>

        <section id="deploy" className="home-section" tabIndex={-1} aria-labelledby="deploy-title">
          <div className="home-section-inner">
            <header className="home-section-heading home-section-heading-wide">
              <p className="home-kicker">04</p>
              <h2 id="deploy-title">{t("home.deploy.title")}</h2>
              <p>{t("home.deploy.lead")}</p>
            </header>
            <ol className="deploy-steps">
              <DeployStep
                title={t("home.deploy.init")}
                text={t("home.deploy.initText")}
                command="exta init --password-stdin --data-dir ./exta-data"
              />
              <DeployStep
                title={t("home.deploy.serve")}
                text={t("home.deploy.serveText")}
                command="exta serve --data-dir ./exta-data"
              />
              <DeployStep
                title={t("home.deploy.open")}
                text={t("home.deploy.openText")}
                command="http://127.0.0.1:8789/app/"
              />
            </ol>
            <p className="deploy-note">{t("home.deploy.container")}</p>
            <Integrate />
          </div>
        </section>

        <section className="home-cta" aria-labelledby="cta-title">
          <div className="home-cta-inner">
            <h2 id="cta-title">{t("home.cta.title")}</h2>
            <p>{t("home.cta.text")}</p>
            <Link className="link-button link-button-primary link-button-large" to={consoleTarget}>
              {consoleLabel}
              <IconArrowRight aria-hidden />
            </Link>
          </div>
        </section>
      </main>
      <footer className="home-footer">
        <div className="home-footer-inner">
          <span className="home-footer-brand">
            <BrandMark size={22} />
            Exta
          </span>
          <p>{t("home.footer.trademark")}</p>
        </div>
      </footer>
    </div>
  );
}

/**
 * Schematic of one call: client request, gateway decisions, providers and the record.
 *
 * @returns Decorative-but-labelled figure.
 */
function FlowVisual() {
  const { t } = useI18n();
  return (
    <figure className="flow" aria-label={t("home.flow.label")}>
      <div className="flow-node flow-client">
        <div className="flow-node-head">
          <span className="flow-tag">01</span>
          <strong>{t("home.flow.client")}</strong>
        </div>
        <p className="flow-detail">{t("home.flow.clientDetail")}</p>
        <pre className="flow-code">
          <span className="flow-code-method">POST</span> /v1/search{"\n"}
          <span className="flow-code-key">Authorization:</span> Bearer &lt;EXTA_TOKEN&gt;{"\n"}
          {'{ "query": "…", "max_results": 5 }'}
        </pre>
      </div>
      <span className="flow-link" aria-hidden="true" />
      <div className="flow-node flow-gateway">
        <div className="flow-node-head">
          <span className="flow-tag">02</span>
          <strong>{t("home.flow.gateway")}</strong>
        </div>
        <p className="flow-detail">{t("home.flow.gatewayDetail")}</p>
        <div className="flow-route">
          <span className="flow-route-label">{t("home.flow.route")}</span>
          <code>search</code>
          <span aria-hidden="true">→</span>
          <code>exa · common</code>
          <span aria-hidden="true">,</span>
          <code>tavily · common</code>
        </div>
        <p className="flow-attempts">{t("home.flow.attempts", { count: 3 })}</p>
      </div>
      <span className="flow-link flow-link-split" aria-hidden="true" />
      <div className="flow-providers">
        <div className="flow-node flow-provider">
          <span className="flow-tag">03</span>
          <strong>{t("provider.exa")}</strong>
        </div>
        <div className="flow-node flow-provider">
          <span className="flow-tag">03</span>
          <strong>{t("provider.tavily")}</strong>
        </div>
      </div>
      <figcaption className="flow-caption">
        <span>{t("home.flow.providersDetail")}</span>
        <span className="flow-record">
          <span className="flow-record-dot" aria-hidden="true" />
          {t("home.flow.record")}
        </span>
      </figcaption>
    </figure>
  );
}

/**
 * One numbered deployment step with a copyable command.
 *
 * @param props - Step title, explanation and command text.
 * @returns List item.
 */
function DeployStep({ title, text, command }: { title: string; text: string; command: string }) {
  const { t } = useI18n();
  return (
    <li className="deploy-step reveal">
      <h3>{title}</h3>
      <p>{text}</p>
      <div className="command-line">
        <code>{command}</code>
        <CopyButton text={command} label={t("home.integrate.copy", { title })} />
      </div>
    </li>
  );
}

/**
 * REST and MCP client examples built from this server's own origin.
 *
 * @returns Integration panel.
 */
function Integrate() {
  const { t } = useI18n();
  const origin = window.location.origin;
  const examples = [
    {
      key: "rest",
      title: t("home.integrate.rest"),
      command: `curl -H "Authorization: Bearer <EXTA_TOKEN>" \\\n  -H "Content-Type: application/json" \\\n  -d '{"query":"example","max_results":5}' \\\n  "${origin}/v1/search"`,
    },
    {
      key: "mcp",
      title: t("home.integrate.mcp"),
      command: `curl -H "Authorization: Bearer <EXTA_TOKEN>" \\\n  -H "Content-Type: application/json" \\\n  -H "Accept: application/json, text/event-stream" \\\n  -H "Mcp-Protocol-Version: 2026-07-28" \\\n  -H "Mcp-Method: server/discover" \\\n  --data '{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}' \\\n  "${origin}/mcp"`,
    },
  ];
  return (
    <div id="integrate" className="integrate reveal" tabIndex={-1}>
      <div className="integrate-heading">
        <h3>{t("home.integrate.title")}</h3>
        <p>{t("home.integrate.lead")}</p>
      </div>
      <Tabs type="line" tabPaneMotion={false} className="integrate-tabs">
        {examples.map((example) => (
          <Tabs.TabPane key={example.key} itemKey={example.key} tab={example.title}>
            <div className="integrate-code">
              <pre className="code-block">{example.command}</pre>
              <div className="integrate-code-footer">
                <span className="muted">{t("home.integrate.placeholder")}</span>
                <CopyButton
                  text={example.command}
                  label={t("home.integrate.copy", { title: example.title })}
                />
              </div>
            </div>
          </Tabs.TabPane>
        ))}
      </Tabs>
    </div>
  );
}
