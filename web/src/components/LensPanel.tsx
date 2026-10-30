// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router";
import {
  getLens,
  getObject,
  getObjectVersion,
  type DeriveLens,
  type ObjectView,
  type ObjectVersion
} from "../api";
import { ErrorNotice } from "../errors";
import { useI18n } from "../i18n";

function changedFields(current: Record<string, unknown>, historical: Record<string, unknown>): string[] {
  const keys = new Set([...Object.keys(current), ...Object.keys(historical)]);
  const changed: string[] = [];
  for (const key of keys) {
    if (JSON.stringify(current[key]) !== JSON.stringify(historical[key])) {
      changed.push(key);
    }
  }
  return changed.sort();
}

function statePreview(version: ObjectVersion | null): string {
  if (!version) {
    return "";
  }
  const state = version.state;
  for (const key of ["premise", "summary", "prompt", "body", "title"]) {
    const value = state[key];
    if (typeof value === "string" && value.trim()) {
      return value;
    }
  }
  if (Array.isArray(state.elements)) {
    return `${state.elements.length}`;
  }
  return "—";
}

function statePreviewFromObject(object: ObjectView): string {
  const state = object.state;
  for (const key of ["premise", "summary", "prompt", "body", "title"]) {
    const value = state[key];
    if (typeof value === "string" && value.trim()) {
      return value;
    }
  }
  if (Array.isArray(state.elements)) {
    return `${state.elements.length}`;
  }
  return "—";
}

export default function LensPanel({ objectID }: { objectID: string }) {
  const { t, lang } = useI18n();
  const [lens, setLens] = useState<DeriveLens | null>(null);
  const [historical, setHistorical] = useState<ObjectVersion | null>(null);
  const [loadingVersion, setLoadingVersion] = useState(0);
  const [branchPreview, setBranchPreview] = useState<{ id: string; object: ObjectView } | null>(null);
  const [previewingBranch, setPreviewingBranch] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);

  const timeFormatter = new Intl.DateTimeFormat(lang === "zh" ? "zh-CN" : "en", {
    dateStyle: "medium",
    timeStyle: "short"
  });

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError(null);
      try {
        const data = await getLens(objectID, signal);
        if (!signal?.aborted) {
          setLens(data);
        }
      } catch (failure) {
        if (!signal?.aborted) {
          setError(failure);
        }
      } finally {
        if (!signal?.aborted) {
          setLoading(false);
        }
      }
    },
    [objectID]
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  async function showVersion(version: number) {
    setLoadingVersion(version);
    setError(null);
    try {
      setHistorical(await getObjectVersion(objectID, version));
    } catch (failure) {
      setError(failure);
    } finally {
      setLoadingVersion(0);
    }
  }

  async function previewBranch(childID: string) {
    setPreviewingBranch(childID);
    setError(null);
    try {
      setBranchPreview({ id: childID, object: await getObject(childID) });
    } catch (failure) {
      setError(failure);
    } finally {
      setPreviewingBranch("");
    }
  }

  if (loading && !lens) {
    return <p className="muted">{t("common.loading")}</p>;
  }
  if (error && !lens) {
    return <ErrorNotice error={error} onRetry={() => void load()} />;
  }
  if (!lens) {
    return <p className="muted">{t("lens.empty")}</p>;
  }

  return (
    <section className="card lens-panel" data-testid="lens-panel">
      <div className="section-heading">
        <h2>{t("lens.title")}</h2>
        <button className="button button-ghost button-small" type="button" onClick={() => void load()}>
          {t("common.refresh")}
        </button>
      </div>
      {error ? <ErrorNotice error={error} compact /> : null}

      <div className="lens-source">
        <h3>{t("lens.source")}</h3>
        {lens.lineage ? (
          <p>
            {lens.lineage.public && lens.lineage.publication_id ? (
              <Link className="text-link" to={`/explore/${lens.lineage.publication_id}`}>
                {t("lens.publicSource", {
                  title: lens.lineage.publication_title || t("common.unknown")
                })}
              </Link>
            ) : (
              <Link className="text-link" to={`/objects/${lens.lineage.parent.object_id}`}>
                {t("lens.privateSource", {
                  title: t("lens.openSource"),
                  version: lens.lineage.parent.version
                })}
              </Link>
            )}
            <span className="muted"> · v{lens.lineage.parent.version}</span>
          </p>
        ) : (
          <p className="muted">{t("lens.noSource")}</p>
        )}
        <p className="muted">
          {t("lens.inherit")}:{" "}
          {lens.inheritance_fields.length > 0
            ? lens.inheritance_fields
                .map((field) => t(`branch.field.${field}` as never))
                .join(", ")
            : t("lens.none")}
        </p>
      </div>

      <div className="lens-versions">
        <h3>{t("lens.versions")}</h3>
        <ol className="lens-version-list">
          {lens.versions
            .slice()
            .reverse()
            .map((version) => (
              <li key={version.version}>
                <button
                  type="button"
                  className={historical?.version === version.version ? "lens-version lens-version-active" : "lens-version"}
                  disabled={loadingVersion === version.version}
                  onClick={() => void showVersion(version.version)}
                >
                  <span className="mono">v{version.version}</span>
                  <span>{version.action || version.event_type}</span>
                  <small>{timeFormatter.format(new Date(version.created_at))}</small>
                </button>
              </li>
            ))}
        </ol>
        {historical ? (
          <div className="lens-historical">
            <p className="muted">
              {t("lens.previewVersion", {
                version: historical.version,
                current: historical.current_version
              })}
            </p>
            <p className="lens-preview-text">{statePreview(historical)}</p>
            {historical.version !== historical.current_version ? (
              <p className="muted">
                {t("lens.changedFields", {
                  fields:
                    changedFields(lens.object.state, historical.state)
                      .map((field) => t(`branch.field.${field}` as never) || field)
                      .join(", ") || t("lens.none")
                })}
              </p>
            ) : null}
          </div>
        ) : null}
      </div>

      <div className="lens-branches">
        <h3>{t("lens.branches")}</h3>
        {lens.children.length === 0 ? (
          <p className="muted">{t("lens.noBranches")}</p>
        ) : (
          <ul className="lens-branch-list">
            {lens.children.map((child) => (
              <li key={child.object_id}>
                <Link to={`/objects/${child.object_id}`}>{child.title || t("common.untitled")}</Link>
                <small>
                  v{child.version} · {t("lens.fromVersion", { version: child.source_version })}
                </small>
                <button
                  className="text-link"
                  type="button"
                  disabled={previewingBranch === child.object_id}
                  onClick={() => void previewBranch(child.object_id)}
                >
                  {previewingBranch === child.object_id ? t("common.loading") : t("lens.previewBranch")}
                </button>
                {branchPreview?.id === child.object_id ? (
                  <p className="lens-preview-text">{statePreviewFromObject(branchPreview.object)}</p>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </div>

      {lens.proposals.length > 0 ? (
        <div className="lens-proposals">
          <h3>{t("lens.proposals")}</h3>
          <p className="muted">{t("lens.proposalsCount", { count: lens.proposals.length })}</p>
        </div>
      ) : null}
    </section>
  );
}
