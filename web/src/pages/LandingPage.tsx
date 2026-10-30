// SPDX-License-Identifier: Apache-2.0
import { Link } from "react-router";
import { useI18n } from "../i18n";

export default function LandingPage() {
  const { t } = useI18n();
  return (
    <div className="landing">
      <section className="landing-hero">
        <div className="landing-hero-copy">
          <p className="eyebrow">{t("landing.eyebrow")}</p>
          <h1>{t("landing.title")}</h1>
          <p className="lead">{t("landing.lead")}</p>
          <div className="hero-actions">
            <Link className="button" to="/register">
              {t("landing.cta.primary")}
            </Link>
            <Link className="button button-ghost" to="/login">
              {t("landing.cta.secondary")}
            </Link>
          </div>
          <p className="landing-fixture">{t("landing.fixtureNote")}</p>
        </div>
        <div className="landing-orbit" aria-hidden="true">
          <span className="orbit-node orbit-you">{t("world.map.you")}</span>
          <span className="orbit-node orbit-ai">{t("world.map.ai")}</span>
          <span className="orbit-node orbit-object">{t("world.map.objects")}</span>
          <span className="orbit-node orbit-branch">{t("world.map.branches")}</span>
          <span className="orbit-ring orbit-ring-one" />
          <span className="orbit-ring orbit-ring-two" />
        </div>
      </section>

      <section className="landing-section">
        <h2>{t("landing.concept.title")}</h2>
        <div className="concept-grid">
          {(["identity", "object", "relation", "derive"] as const).map((key) => (
            <article className="card concept-card" key={key}>
              <h3>{t(`landing.concept.${key}.title`)}</h3>
              <p>{t(`landing.concept.${key}.body`)}</p>
            </article>
          ))}
        </div>
      </section>

      <section className="landing-section">
        <h2>{t("landing.loop.title")}</h2>
        <p className="lead">{t("landing.loop.lead")}</p>
        <ol className="loop-grid">
          {(["1", "2", "3", "4"] as const).map((step) => (
            <li className="loop-step" key={step}>
              <span className="step-number" aria-hidden="true">
                {step}
              </span>
              <h3>{t(`landing.loop.step${step}.title`)}</h3>
              <p>{t(`landing.loop.step${step}.body`)}</p>
            </li>
          ))}
        </ol>
      </section>

      <section className="landing-section landing-two-types">
        <h2>{t("landing.twoTypes.title")}</h2>
        <div className="concept-grid concept-grid-two">
          <article className="card object-preview object-preview-living_scene">
            <span className="template-badge template-living_scene">
              {t("stage.entryProject")}
            </span>
            <h3>{t("stage.entryProject")}</h3>
            <p>{t("stage.entryProjectHint")}</p>
          </article>
          <article className="card object-preview object-preview-living_scene">
            <span className="template-badge template-living_scene">
              {t("stage.entryStory")}
            </span>
            <h3>{t("stage.entryStory")}</h3>
            <p>{t("stage.entryStoryHint")}</p>
          </article>
        </div>
      </section>

      <section className="landing-section landing-open">
        <div>
          <h2>{t("landing.opensource.title")}</h2>
          <p className="lead">{t("landing.opensource.body")}</p>
        </div>
        <Link className="button" to="/register">
          {t("landing.cta.primary")}
        </Link>
      </section>
    </div>
  );
}
