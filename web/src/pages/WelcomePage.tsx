// SPDX-License-Identifier: Apache-2.0
import { Link } from "react-router";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";
import { useMeta } from "../meta";

export default function WelcomePage() {
  const { t } = useI18n();
  const { status, user, profile } = useAuth();
  const { mode } = useMeta();

  if (status === "loading") {
    return <p className="page">{t("common.loading")}</p>;
  }
  if (status === "anonymous" || !user) {
    return (
      <div className="page">
        <p className="notice">{t("auth.required")}</p>
        <Link className="button" to="/login?next=%2Fwelcome">
          {t("nav.login")}
        </Link>
      </div>
    );
  }

  return (
    <div className="page welcome-page">
      <p className="eyebrow">{t("welcome.eyebrow")}</p>
      <h1>{t("welcome.title")}</h1>
      <p className="lead">{t("welcome.lead")}</p>

      <div className="welcome-cards">
        <section className="card welcome-identity">
          <div className="avatar avatar-large" aria-hidden="true">
            {user.display_name.slice(0, 1).toUpperCase()}
          </div>
          <h2>{t("welcome.identity.title")}</h2>
          <p className="welcome-name">{user.display_name}</p>
          <p className="mono">@{user.handle}</p>
          <p>{user.email}</p>
          <p className="muted">{t("welcome.identity.body")}</p>
        </section>
        <section className="card welcome-ai">
          <span className="template-badge template-interactive">{t("nav.ai")}</span>
          <h2>{t("welcome.ai.title")}</h2>
          <p className="welcome-name">{profile?.name ?? "—"}</p>
          <p>{t("welcome.ai.body", { name: profile?.name ?? "Personal AI" })}</p>
          {mode === "fixture" ? <p className="fixture-inline">{t("welcome.fixture")}</p> : null}
          <Link className="text-link" to="/ai">
            {t("nav.ai")} →
          </Link>
        </section>
      </div>

      <section className="welcome-steps">
        {(["1", "2", "3"] as const).map((step) => (
          <article className="welcome-step" key={step}>
            <span className="step-number" aria-hidden="true">
              {step}
            </span>
            <h3>{t(`welcome.step${step}.title`)}</h3>
            <p>{t(`welcome.step${step}.body`)}</p>
          </article>
        ))}
      </section>

      <div className="form-actions">
        <Link className="button" to="/objects/new">
          {t("welcome.create")}
        </Link>
        <Link className="button button-ghost" to="/">
          {t("welcome.world")}
        </Link>
      </div>
      <p className="muted welcome-note">{t("welcome.note")}</p>
    </div>
  );
}
