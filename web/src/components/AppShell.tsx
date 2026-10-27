// SPDX-License-Identifier: Apache-2.0
import { useEffect, useRef, useState } from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";
import { useMeta } from "../meta";
import { ErrorNotice } from "../errors";
import LanguageToggle from "./LanguageToggle";
import ModeBadge from "./ModeBadge";
import AISettingsDialog from "./v2/AISettingsDialog";
import HelpDialog from "./v2/HelpDialog";
import Icon from "./v2/Icon";

const navClass = ({ isActive }: { isActive: boolean }) =>
  isActive ? "dw-rail-link active" : "dw-rail-link";

export default function AppShell() {
  const { t, lang } = useI18n();
  const { mode, status: metaStatus } = useMeta();
  const { status, user, logout, logoutError, authError, refresh } = useAuth();
  const location = useLocation();
  const authenticated = status === "authenticated";
  const zh = lang === "zh";
  const [aiOpen, setAIOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const profileRef = useRef<HTMLDetailsElement>(null);

  useEffect(() => {
    window.scrollTo({ top: 0, behavior: "auto" });
    profileRef.current?.removeAttribute("open");
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

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      const target = document.activeElement;
      const tag = target?.tagName;
      const editing = tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || (target as HTMLElement | null)?.isContentEditable;
      if (editing) {
        return;
      }
      if (event.key === "?" || event.key === "F1") {
        event.preventDefault();
        setHelpOpen(true);
      } else if (event.key === "n" || event.key === "N") {
        if (authenticated) {
          event.preventDefault();
          window.dispatchEvent(new Event("dw:create-world"));
        }
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [authenticated]);

  return (
    <div className={`dw-app${authenticated ? " dw-app-authenticated" : ""}`}>
      {authenticated ? (
        <aside className="dw-rail" aria-label={t("nav.primary")}>
          <Link to="/" className="dw-rail-logo" aria-label={t("app.name")}>
            <Icon name="spark" size={21} />
          </Link>
          <nav className="dw-rail-nav" aria-label={t("nav.primary")}>
            <NavLink end to="/" className={navClass} aria-label={zh ? "我的世界" : "My world"}>
              <Icon name="world" size={21} />
              <span>{zh ? "我的世界" : "World"}</span>
            </NavLink>
            <NavLink to="/friends" className={navClass} aria-label={zh ? "一起创造" : "Together"}>
              <Icon name="together" size={21} />
              <span>{zh ? "一起创造" : "Together"}</span>
            </NavLink>
            <NavLink to="/explore" className={navClass} aria-label={zh ? "探索世界" : "Explore"}>
              <Icon name="explore" size={21} />
              <span>{zh ? "探索世界" : "Explore"}</span>
            </NavLink>
          </nav>
          <div className="dw-rail-bottom">
            <button
              className="dw-round-button"
              type="button"
              onClick={() => setHelpOpen(true)}
              aria-label={zh ? "体验指南" : "Guide"}
              title={zh ? "体验指南" : "Guide"}
            >
              <Icon name="help" size={18} />
            </button>
            {user ? (
              <Link to="/self" className="dw-rail-avatar" aria-label={zh ? "你的个人空间" : "Your profile"}>
                {user.display_name.slice(0, 1).toUpperCase()}
              </Link>
            ) : null}
          </div>
        </aside>
      ) : null}

      <div className="dw-shell-frame">
        <a className="skip-link" href="#main-content">
          {t("a11y.skip")}
        </a>
        <header className="dw-top">
          <div className="dw-brand-line">
            <Link className="dw-brand" to="/" aria-label={t("app.name")}>
              derive world<span>.</span>
            </Link>
            <span className="dw-brand-divider" aria-hidden="true" />
            <span className="dw-prototype-status">
              <span className="dw-status-dot" aria-hidden="true" />
              {zh ? "living experience / 02" : "living experience / 02"}
            </span>
          </div>
          <div className="dw-top-actions">
            {authenticated ? (
              <button
                className="dw-top-button dw-ai-entry"
                type="button"
                onClick={() => setAIOpen(true)}
                aria-label={zh ? "设置 AI 模型" : "AI settings"}
              >
                <Icon name="gear" size={16} />
                <span>{zh ? "设置 AI" : "AI setup"}</span>
              </button>
            ) : null}
            <button className="dw-top-button" type="button" onClick={() => setHelpOpen(true)}>
              <Icon name="help" size={16} />
              <span>{zh ? "如何体验" : "How it works"}</span>
            </button>
            <span className="dw-mode-wrap" title={mode === "fixture" ? (zh ? "Fixture 模式：AI 输出为确定性演示，不是真实模型" : "Fixture mode: AI output is deterministic demo output, not a real model") : (zh ? "真实模型模式" : "Real model mode")}>
              <ModeBadge mode={mode} />
            </span>
            <LanguageToggle />
            {authenticated && user ? (
              <details className="dw-profile" ref={profileRef}>
                <summary aria-label={zh ? "账户菜单" : "Account menu"}>
                  <span className="dw-profile-avatar">{user.display_name.slice(0, 1).toUpperCase()}</span>
                  <span className="dw-profile-name">{user.display_name}</span>
                </summary>
                <div className="dw-profile-popover">
                  <div className="dw-profile-id">
                    <strong>{user.display_name}</strong>
                    <small>@{user.handle}</small>
                  </div>
                  <Link to="/self">
                    <Icon name="user" size={15} />
                    {t("nav.self")}
                  </Link>
                  <Link to="/objects">
                    <Icon name="layers" size={15} />
                    {t("nav.objects")}
                  </Link>
                  <Link to="/settings">
                    <Icon name="gear" size={15} />
                    {t("nav.settings")}
                  </Link>
                  <button type="button" onClick={() => setAIOpen(true)}>
                    <Icon name="spark" size={15} />
                    {zh ? "设置 AI" : "AI setup"}
                  </button>
                  <button type="button" onClick={() => void logout()}>
                    <Icon name="logout" size={15} />
                    {t("auth.signOut")}
                  </button>
                  {logoutError ? (
                    <span className="dw-form-error" role="alert">
                      {t("auth.logoutFailed")}
                    </span>
                  ) : null}
                </div>
              </details>
            ) : status === "error" ? null : (
              <>
                <Link className="dw-top-button" to="/login">
                  {t("nav.login")}
                </Link>
                <Link className="dw-top-button dw-top-button-primary" to="/register">
                  {t("nav.register")}
                </Link>
              </>
            )}
          </div>
        </header>

        <main className="dw-main" id="main-content">
          {status === "error" ? (
            <div className="dw-page">
              <ErrorNotice error={authError} onRetry={() => void refresh()} />
            </div>
          ) : (
            <Outlet />
          )}
        </main>

        <footer className="dw-footer">
          <span>
            {t("footer.status")}
            {metaStatus === "error" ? ` · ${t("mode.unavailableBanner")}` : ""}
          </span>
          <span className="dw-footer-mode">
            {mode === "fixture"
              ? zh
                ? "FIXTURE 模式 · AI 输出为确定性演示，不是真实模型"
                : "FIXTURE MODE · AI output is deterministic demo output, not a real model"
              : zh
                ? "REAL 模式 · 服务器已配置真实模型"
                : "REAL MODE · a real model is configured on the server"}
          </span>
          <span>{t("footer.license")}</span>
        </footer>
      </div>

      <AISettingsDialog open={aiOpen} onClose={() => setAIOpen(false)} />
      <HelpDialog open={helpOpen} onClose={() => setHelpOpen(false)} />
    </div>
  );
}
