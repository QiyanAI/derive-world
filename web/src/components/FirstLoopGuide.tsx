// SPDX-License-Identifier: Apache-2.0
import { Link } from "react-router";
import type { ObjectView } from "../api";
import { useI18n } from "../i18n";

export default function FirstLoopGuide({
  object,
  hasBranch,
  canDerive,
  onDismiss
}: {
  object: ObjectView;
  hasBranch: boolean;
  canDerive: boolean;
  onDismiss: () => void;
}) {
  const { t } = useI18n();
  const changed = object.version > 1;

  function focusWork() {
    document.getElementById("object-work-panel")?.scrollIntoView({ behavior: "auto", block: "start" });
  }

  return (
    <section className="card first-loop" data-testid="first-loop-guide">
      <div className="section-heading">
        <div>
          <p className="eyebrow">{t("firstLoop.eyebrow")}</p>
          <h2>{t("firstLoop.title")}</h2>
          <p className="muted">{t("firstLoop.lead")}</p>
        </div>
        <button className="text-button" type="button" onClick={onDismiss}>
          {t("firstLoop.dismiss")}
        </button>
      </div>
      <ol className="first-loop-steps">
        <li className={changed ? "first-loop-done" : ""}>
          <span className="step-number" aria-hidden="true">
            1
          </span>
          <div>
            <strong>{t("firstLoop.change.title")}</strong>
            <p>{t("firstLoop.change.body")}</p>
            <button className="text-link" type="button" onClick={focusWork}>
              {changed ? t("firstLoop.change.done") : t("firstLoop.change.action")}
            </button>
          </div>
        </li>
        <li>
          <span className="step-number" aria-hidden="true">
            2
          </span>
          <div>
            <strong>{t("firstLoop.ai.title")}</strong>
            <p>{t("firstLoop.ai.body")}</p>
            <Link className="text-link" to={`/objects/${object.id}?tab=ai`}>
              {t("firstLoop.ai.action")}
            </Link>
          </div>
        </li>
        <li className={hasBranch ? "first-loop-done" : ""}>
          <span className="step-number" aria-hidden="true">
            3
          </span>
          <div>
            <strong>{t("firstLoop.branch.title")}</strong>
            <p>{t("firstLoop.branch.body")}</p>
            {canDerive ? (
              <Link className="text-link" to={`/objects/${object.id}/derive`}>
                {hasBranch ? t("firstLoop.branch.done") : t("firstLoop.branch.action")}
              </Link>
            ) : (
              <span className="muted">{t("firstLoop.branch.noPermission")}</span>
            )}
          </div>
        </li>
      </ol>
      <p className="muted">{t("firstLoop.persist")}</p>
    </section>
  );
}
