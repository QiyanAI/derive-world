// SPDX-License-Identifier: Apache-2.0
import { useState, type ReactNode } from "react";
import { Link } from "react-router";
import type { EventView, Lineage, LineageDescendant, ObjectView } from "../api";
import { ErrorNotice } from "../errors";
import { actionKey, actorLabel, eventAction, eventKey } from "../humanize";
import { useI18n } from "../i18n";

function collectVersions(events: EventView[]): Map<number, EventView[]> {
  const versions = new Map<number, EventView[]>();
  for (const event of events) {
    if (
      event.event_type === "object.created" ||
      event.event_type === "object.derived" ||
      event.event_type === "object.state_changed"
    ) {
      const list = versions.get(event.object_version) ?? [];
      list.push(event);
      versions.set(event.object_version, list);
    }
  }
  return versions;
}

function DescendantTree({
  nodes,
  render
}: {
  nodes: LineageDescendant[];
  render: (node: LineageDescendant) => ReactNode;
}) {
  if (nodes.length === 0) {
    return null;
  }
  return (
    <ul className="lineage-tree">
      {nodes.map((node) => (
        <li key={node.object_id}>
          {render(node)}
          <DescendantTree nodes={node.children} render={render} />
        </li>
      ))}
    </ul>
  );
}

export default function ObjectHistoryTab({
  object,
  events,
  eventCursor,
  lineage,
  currentUserId,
  canDerive,
  loadingMore,
  loadMoreError,
  onLoadMore
}: {
  object: ObjectView;
  events: EventView[];
  eventCursor: string | null;
  lineage: Lineage | null;
  currentUserId: string | undefined;
  canDerive: boolean;
  loadingMore: boolean;
  loadMoreError: unknown;
  onLoadMore: () => Promise<void> | void;
}) {
  const { t, lang } = useI18n();
  const [showPayload, setShowPayload] = useState<Record<string, boolean>>({});
  const formatter = new Intl.DateTimeFormat(lang === "zh" ? "zh-CN" : "en", {
    dateStyle: "medium",
    timeStyle: "short"
  });
  const versions = [...collectVersions(events).entries()].sort((left, right) => right[0] - left[0]);

  return (
    <div className="stack">
      <section className="card">
        <h2>{t("workspace.versionHistory")}</h2>
        <p className="muted">{t("derive.independent")}</p>
        {versions.length === 0 ? <p className="muted">{t("workspace.noVersions")}</p> : null}
        <ol className="version-list">
          {versions.map(([version, versionEvents]) => {
            const latest = versionEvents[versionEvents.length - 1]!;
            const actions = versionEvents
              .map((event) => eventAction(event))
              .filter(Boolean)
              .map((action) => t(actionKey(action)));
            const isCurrent = version === object.version;
            return (
              <li key={version}>
                <div className="version-row">
                  <span className="version-badge">v{version}</span>
                  <div className="version-main">
                    <strong>
                      {actions.length > 0
                        ? actions.join(" · ")
                        : version === 1
                          ? t(object.lineage ? "event.object.derived" : "event.object.created")
                          : t("event.object.state_changed")}
                    </strong>
                    <small>
                      {actorLabel(latest, currentUserId, t)} · {formatter.format(new Date(latest.created_at))}
                    </small>
                  </div>
                  {isCurrent ? <span className="status-chip status-active">{t("workspace.currentVersion")}</span> : null}
                  {canDerive ? (
                    <Link className="text-link" to={`/objects/${object.id}/derive?source=${version}`}>
                      {t("workspace.deriveFromVersion", { version })}
                    </Link>
                  ) : null}
                </div>
              </li>
            );
          })}
        </ol>
      </section>

      <section className="card">
        <div className="section-heading">
          <h2>{t("lineage.map")}</h2>
          <Link className="text-link" to={`/objects/${object.id}/lineage`}>
            {t("object.viewLineage")}
          </Link>
        </div>
        {lineage?.source?.publication_id ? (
          <p>
            <Link className="text-link" to={`/explore/${lineage.source.publication_id}`}>
              {t("workspace.publicSource", {
                title: lineage.source.publication_title || t("common.unknown")
              })}
            </Link>
          </p>
        ) : null}
        <h3>{t("lineage.ancestors")}</h3>
        {lineage && lineage.ancestors.length > 0 ? (
          <ol className="lineage-list">
            {lineage.ancestors.map((ancestor, index) => (
              <li key={`${ancestor.object_id ?? "redacted"}-${index}`}>
                {ancestor.redacted ? (
                  <span className="redacted-node">{t("lineage.redacted")}</span>
                ) : (
                  <Link to={`/objects/${ancestor.object_id}`}>{ancestor.title || t("common.unknown")}</Link>
                )}
                <span>{t("lineage.depth", { depth: ancestor.depth })}</span>
                {ancestor.version ? (
                  <span className="mono">
                    {t("lineage.ancestorVersion", { version: ancestor.version })}
                  </span>
                ) : null}
              </li>
            ))}
          </ol>
        ) : (
          <p className="muted">{t("lineage.none")}</p>
        )}
        <h3>{t("lineage.descendants")}</h3>
        {lineage && lineage.descendants.length > 0 ? (
          <DescendantTree
            nodes={lineage.descendants}
            render={(node) => (
              <div className="lineage-node">
                <Link to={`/objects/${node.object_id}`}>{node.title || t("common.unknown")}</Link>
                <span className="mono">v{node.version}</span>
                <span>{t("lineage.sourceVersion", { version: node.source_version })}</span>
              </div>
            )}
          />
        ) : (
          <p className="muted">{t("lineage.noVisibleBranches")}</p>
        )}
      </section>

      <section className="card">
        <h2>{t("workspace.allEvents")}</h2>
        <ol className="timeline">
          {events.map((event) => (
            <li key={event.id}>
              <div className="timeline-top">
                <span className="event-type">{t(eventKey(event.event_type))}</span>
                <span className="mono">v{event.object_version}</span>
              </div>
              <p className="event-meta">
                {actorLabel(event, currentUserId, t)} · {formatter.format(new Date(event.created_at))}
              </p>
              {eventAction(event) ? (
                <p className="event-action">{t(actionKey(eventAction(event)))}</p>
              ) : null}
              {event.event_type === "object.derived" && typeof event.payload.publication_id === "string" ? (
                <p className="event-action">
                  <Link className="text-link" to={`/explore/${event.payload.publication_id}`}>
                    {t("workspace.publicSource", {
                      title:
                        typeof event.payload.publication_title === "string"
                          ? event.payload.publication_title
                          : t("common.unknown")
                    })}
                  </Link>
                </p>
              ) : null}
              <button
                type="button"
                className="text-button"
                onClick={() => setShowPayload((previous) => ({ ...previous, [event.id]: !previous[event.id] }))}
              >
                {t("workspace.eventPayload")}
              </button>
              {showPayload[event.id] ? (
                <pre className="code-block">{JSON.stringify(event.payload, null, 2)}</pre>
              ) : null}
            </li>
          ))}
          {events.length === 0 ? <li className="muted">{t("workspace.noEvents")}</li> : null}
        </ol>
        {loadMoreError ? <ErrorNotice error={loadMoreError} compact /> : null}
        {eventCursor ? (
          <button
            className="button button-ghost button-small"
            type="button"
            disabled={loadingMore}
            onClick={() => void onLoadMore()}
          >
            {loadingMore ? t("common.loading") : t("workspace.loadMoreEvents")}
          </button>
        ) : null}
      </section>
    </div>
  );
}
