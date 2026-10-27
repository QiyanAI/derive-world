// SPDX-License-Identifier: Apache-2.0
import { useEffect } from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";
import { useMeta } from "../meta";
import { ErrorNotice } from "../errors";
import FixtureBanner from "./FixtureBanner";
import LanguageToggle from "./LanguageToggle";
import ModeBadge from "./ModeBadge";

const navClass = ({ isActive }: { isActive: boolean }) =>
  isActive ? "nav-link nav-link-active" : "nav-link";

export default function AppShell() {
  const { t } = useI18n();
  const { mode, status: metaStatus } = useMeta();
  const { status, user, logout, logoutError, authError, refresh } = useAuth();
  const location = useLocation();
  const authenticated = status === "authenticated";

  useEffect(() => {
    window.scrollTo({ top: 0, behavior: "auto" });
  }, [location.pathname]);

  useEffect(() => {
    const titles: Array<[RegExp, string]> = [
      [/^\/objects\/[^/]+\/derive/, t("derive.title")],
      [/^\/objects\/[^/]+\/lineage/, t("lineage.title")],
      [/^\/objects\/new/, t("object.create.title")],
      [/^\/objects\/[^/]+/, t("nav.objects")],
      [/^\/objects/, t("objects.title")],
      [/^\/friends/, t("friends.title")],
      [/^\/explore/, t("explore.title")],
      [/^\/ai/, t("ai.title")],
      [/^\/self/, t("self.title")],
      [/^\/settings/, t("settings.title")],
      [/^\/welcome/, t("welcome.title")],
      [/^\/invite/, t("invite.preview")],
      [/^\/login/, t("auth.login.title")],
      [/^\/register/, t("auth.register.title")]
    ];
    const match = titles.find(([pattern]) => pattern.test(location.pathname));
    document.title = match ? `${match[1]} · ${t("app.name")}` : t("app.name");
  }, [location.pathname, t]);

  return (
    <div className={`app-shell dw-shell${authenticated ? " dw-auth" : ""}`}>
      {authenticated ? <aside className="dw-rail" aria-label={t("nav.primary")}><Link to="/" className="dw-rail-logo" aria-label={t("app.name")}>✧</Link><nav className="dw-rail-nav"><NavLink end to="/" className={({isActive})=>isActive?"dw-rail-link active":"dw-rail-link"}>◉<small>{t("nav.world")}</small></NavLink><NavLink to="/friends" className={({isActive})=>isActive?"dw-rail-link active":"dw-rail-link"}>◎<small>{t("nav.friends")}</small></NavLink><NavLink to="/explore" className={({isActive})=>isActive?"dw-rail-link active":"dw-rail-link"}>✦<small>{t("nav.explore")}</small></NavLink></nav><Link to="/self" className="dw-rail-avatar" aria-label={t("nav.self")}>☺</Link></aside> : null}<a className="skip-link" href="#main-content">
        {t("a11y.skip")}
      </a>
      <header className="site-header">
        <Link className="brand" to="/" aria-label={t("app.name")}>
          <span className="brand-mark" aria-hidden="true">
            DW
          </span>
          <span className="brand-text">
            <strong>{t("app.name")}</strong>
            <small>{t("app.tagline")}</small>
          </span>
        </Link>
        <nav className="main-nav" aria-label={t("nav.primary")}>
          
          {authenticated ? (
            <>
              
              
              <NavLink to="/objects" className={navClass}>
                {t("nav.objects")}
              </NavLink>
              <NavLink to="/ai" className={navClass}>
                {t("nav.ai")}
              </NavLink>
              <NavLink to="/self" className={navClass}>
                {t("nav.self")}
              </NavLink>
              <NavLink to="/settings" className={navClass}>
                {t("nav.settings")}
              </NavLink>
            </>
          ) : null}
        </nav>
        <div className="header-actions">
          {authenticated ? <Link className="button button-ghost button-compact dw-ai-setup" to="/settings#ai-connection">{t("system.modelMode")} ⚙</Link> : null}<ModeBadge mode={mode} />
          <LanguageToggle />
          {authenticated && user ? (
            <>
              <span className="signed-in-name" title={user.display_name}>
                {user.display_name}
              </span>
              <button className="button button-ghost button-compact" type="button" onClick={() => void logout()}>
                {t("auth.signOut")}
              </button>
              {logoutError ? (
                <span className="form-error header-error" role="alert">
                  {t("auth.logoutFailed")}
                </span>
              ) : null}
            </>
          ) : status === "error" ? null : (
            <>
              <Link className="button button-ghost button-compact" to="/login">
                {t("nav.login")}
              </Link>
              <Link className="button button-compact" to="/register">
                {t("nav.register")}
              </Link>
            </>
          )}
        </div>
      </header>
      {mode === "fixture" ? <FixtureBanner /> : null}
      {metaStatus === "error" ? <p className="fixture-banner">{t("mode.unavailableBanner")}</p> : null}
      <main className="site-main" id="main-content">
        {status === "error" ? (
          <div className="page">
            <ErrorNotice error={authError} onRetry={() => void refresh()} />
          </div>
        ) : (
          <Outlet />
        )}
      </main>
      <footer className="site-footer">
        <div className="footer-line">
          <span>{t("footer.status")}</span>
          <span>{t("footer.license")}</span>
        </div>
        <span className="footer-release">{t("footer.release")}</span>
      </footer>
    </div>
  );
}
