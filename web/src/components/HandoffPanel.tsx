// SPDX-License-Identifier: Apache-2.0
import { useState } from "react";
import { getHandoff, type Handoff } from "../api";
import { ErrorNotice } from "../errors";
import { permissionKey } from "../humanize";
import { useI18n } from "../i18n";

function summarizeState(state: Record<string, unknown>): string {
  if (typeof state.title === "string") {
    const body = typeof state.body === "string" ? state.body.length : 0;
    const summary = typeof state.summary === "string" ? state.summary.length : 0;
    return `${state.title} · summary ${summary} chars · body ${body} chars`;
  }
  if (typeof state.prompt === "string") {
    const options = Array.isArray(state.options) ? state.options.length : 0;
    return `${state.prompt} · ${options} options`;
  }
  return Object.keys(state).join(", ");
}

export default function HandoffPanel({ objectID }: { objectID: string }) {
  const { t } = useI18n();
  const [projection, setProjection] = useState<Handoff | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  async function load() {
    setBusy(true);
    setError(null);
    try {
      setProjection(await getHandoff(objectID));
    } catch (failure) {
      setError(failure);
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="card">
      <h2>{t("workspace.handoff")}</h2>
      <p className="muted">{t("workspace.handoffIntro")}</p>
      <p className="muted">{t("workspace.handoffPrivate")}</p>
      <button className="button button-small button-ghost" type="button" disabled={busy} onClick={() => void load()}>
        {busy ? t("common.loading") : t("workspace.handoffHint")}
      </button>
      {error ? <ErrorNotice error={error} compact /> : null}
      {projection ? (
        <div className="handoff-view" data-testid="handoff-preview">
          <dl className="meta-grid">
            <div>
              <dt>{t("workspace.handoffVersion")}</dt>
              <dd className="mono">v{projection.object_version}</dd>
            </div>
            <div>
              <dt>{t("workspace.handoffTruncated")}</dt>
              <dd>
                {projection.truncated ? t("workspace.handoffTruncatedYes") : t("workspace.handoffTruncatedNo")}
              </dd>
            </div>
            <div>
              <dt>{t("workspace.handoffActions")}</dt>
              <dd>
                {projection.permitted_actions.map((action) => (
                  <span className="permission-chip" key={action}>
                    {t(permissionKey(action))}
                  </span>
                ))}
              </dd>
            </div>
            <div>
              <dt>{t("workspace.handoffEvents")}</dt>
              <dd className="mono">{projection.permitted_object_context.recent_events.length}</dd>
            </div>
          </dl>
          <p>
            <strong>{t("workspace.sectionWork")}:</strong>{" "}
            <span className="mono">{summarizeState(projection.permitted_object_context.state)}</span>
          </p>
          {projection.permitted_object_context.recent_events.length > 0 ? (
            <ul className="handoff-events">
              {projection.permitted_object_context.recent_events.map((event) => (
                <li key={`${event.seq}-${event.event_type}`}>
                  <span className="mono">#{event.seq}</span>
                  <span>{event.summary}</span>
                </li>
              ))}
            </ul>
          ) : null}
          <details className="advanced-panel">
            <summary>{t("workspace.handoffRaw")}</summary>
            <pre className="code-block handoff-preview">{JSON.stringify(projection, null, 2)}</pre>
          </details>
        </div>
      ) : null}
    </section>
  );
}
