// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState, type ReactNode } from "react";
import { Link, useParams } from "react-router";
import { getLineage, getObject, type Lineage, type LineageDescendant, type ObjectView } from "../api";
import { ErrorNotice } from "../errors";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

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

export default function LineagePage() {
  const { id } = useParams();
  const { t } = useI18n();
  const { status } = useAuth();
  const [lineage, setLineage] = useState<Lineage | null>(null);
  const [object, setObject] = useState<ObjectView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    if (!id || status !== "authenticated") {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    Promise.all([getLineage(id, controller.signal), getObject(id, controller.signal).catch(() => null)])
      .then(([loadedLineage, loadedObject]) => {
        if (!controller.signal.aborted) {
          setLineage(loadedLineage);
          setObject(loadedObject);
        }
      })
      .catch((failure) => {
        if (!controller.signal.aborted) {
          setError(failure);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false);
        }
      });
    return () => controller.abort();
  }, [id, status]);

  if (status === "loading" || loading) {
    return <p className="page">{t("common.loading")}</p>;
  }
  if (status === "anonymous") {
    return (
      <div className="page">
        <p className="notice">{t("auth.required")}</p>
        <Link className="button" to={`/login?next=${encodeURIComponent(`/objects/${id ?? ""}/lineage`)}`}>
          {t("nav.login")}
        </Link>
      </div>
    );
  }
  if (!lineage) {
    return (
      <div className="page">
        <h1>{t("lineage.title")}</h1>
        <p className="notice">{t("lineage.error")}</p>
        {error ? <ErrorNotice error={error} /> : null}
        <Link className="button" to={`/objects/${id ?? ""}`}>
          {t("lineage.back")}
        </Link>
      </div>
    );
  }

  const hasRedacted = lineage.ancestors.some((ancestor) => ancestor.redacted);
  const title = object ? String(object.state.title ?? "") : lineage.object.object_id;

  return (
    <div className="page">
      <p className="eyebrow">{t("lineage.map")}</p>
      <h1>{t("lineage.title")}</h1>
      <p className="lead">{t("lineage.lead")}</p>

      {lineage.truncated ? <p className="notice notice-compact">{t("lineage.truncated")}</p> : null}
      {hasRedacted ? <p className="notice notice-compact">{t("lineage.redactedHint")}</p> : null}

      {lineage.source && lineage.source.publication_id ? (
        <section className="card">
          <h2>{t("lineage.source")}</h2>
          <p>
            <Link to={`/explore/${lineage.source.publication_id}`}>
              {lineage.source.publication_title || t("common.unknown")}
            </Link>
            <span className="mono"> · v{lineage.source.parent.version}</span>
          </p>
          <p className="muted">{t("lineage.publicSourceNote")}</p>
        </section>
      ) : null}

      <section className="card">
        <h2>{t("lineage.ancestors")}</h2>
        {lineage.ancestors.length === 0 ? (
          <p className="muted">{t("lineage.none")}</p>
        ) : (
          <ol className="lineage-chain">
            {lineage.ancestors.map((ancestor, index) => (
              <li key={`${ancestor.object_id ?? "redacted"}-${index}`}>
                {ancestor.redacted ? (
                  <span className="branch-node branch-locked">
                    <span className="map-node-label">{t("lineage.redacted")}</span>
                    <span>{t("lineage.redactedHint")}</span>
                  </span>
                ) : (
                  <Link className="branch-node" to={`/objects/${ancestor.object_id}`}>
                    <span className="map-node-label">
                      {t("lineage.depth", { depth: ancestor.depth })}
                    </span>
                    <strong>{ancestor.title || t("common.unknown")}</strong>
                    {ancestor.version ? (
                      <span className="mono">
                        {t("lineage.ancestorVersion", { version: ancestor.version })}
                      </span>
                    ) : null}
                  </Link>
                )}
                <span className="branch-arrow" aria-hidden="true">
                  →
                </span>
              </li>
            ))}
            <li>
              <span className="branch-node branch-current">
                <span className="map-node-label">{t("lineage.current")}</span>
                <strong>{title}</strong>
                <span className="mono">v{lineage.object.version}</span>
              </span>
            </li>
          </ol>
        )}
        {lineage.ancestors.length === 0 ? (
          <div className="branch-node branch-current">
            <span className="map-node-label">{t("lineage.current")}</span>
            <strong>{title}</strong>
            <span className="mono">v{lineage.object.version}</span>
          </div>
        ) : null}
      </section>

      <section className="card">
        <h2>{t("lineage.descendants")}</h2>
        {lineage.descendants.length === 0 ? (
          <p className="muted">{t("lineage.noVisibleBranches")}</p>
        ) : (
          <DescendantTree
            nodes={lineage.descendants}
            render={(node) => (
              <div className="lineage-node">
                <Link to={`/objects/${node.object_id}`}>{node.title || t("common.unknown")}</Link>
                <span className="mono">v{node.version}</span>
                <span>{t("lineage.sourceVersion", { version: node.source_version })}</span>
                <span>{t("lineage.depth", { depth: node.depth })}</span>
              </div>
            )}
          />
        )}
      </section>

      <p className="form-hint">{t("lineage.privateNote")}</p>
      <div className="form-actions">
        {object?.permissions.includes("derive") ? (
          <Link className="button" to={`/objects/${lineage.object.object_id}/derive`}>
            {t("lineage.deriveFromHere")}
          </Link>
        ) : null}
        <Link className="button button-ghost" to={`/objects/${lineage.object.object_id}`}>
          {t("lineage.back")}
        </Link>
      </div>
    </div>
  );
}
