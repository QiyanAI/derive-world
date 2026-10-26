// SPDX-License-Identifier: Apache-2.0
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router";
import {
  getWorldStage,
  listConfirmations,
  type Confirmation,
  type StageObject,
  type WorldStage
} from "../api";
import PulsePanel from "../components/PulsePanel";
import { ErrorNotice } from "../errors";
import { actionKey, eventKey } from "../humanize";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

export default function WorldStagePage() {
  const { t } = useI18n();
  const { user } = useAuth();
  const [stage, setStage] = useState<WorldStage | null>(null);
  const [confirmations, setConfirmations] = useState<Confirmation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);

  const load = useCallback(async (signal?: AbortSignal) => {
    setError(null);
    try {
      const [data, pending] = await Promise.all([
        getWorldStage(signal),
        listConfirmations("pending", signal).catch(() => ({ items: [] }))
      ]);
      if (!signal?.aborted) {
        setStage(data);
        setConfirmations(pending.items);
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
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  useEffect(() => {
    let source: EventSource | null = null;
    let fallback: number | null = null;
    const startPolling = () => {
      if (fallback !== null) {
        return;
      }
      fallback = window.setInterval(() => {
        if (!document.hidden) {
          void load();
        }
      }, 15000);
    };
    try {
      source = new EventSource("/api/v1/stream");
      source.addEventListener("refresh", () => void load());
      source.addEventListener("reconnect", () => {
        source?.close();
        startPolling();
      });
      source.onerror = () => {
        source?.close();
        source = null;
        startPolling();
      };
    } catch {
      startPolling();
    }
    const onVisible = () => {
      if (!document.hidden) {
        void load();
      }
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      source?.close();
      if (fallback !== null) {
        window.clearInterval(fallback);
      }
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [load]);

  const { roots, childrenOf } = useMemo(() => {
    const objects = stage?.objects ?? [];
    const edges = stage?.edges ?? [];
    const byId = new Map(objects.map((object) => [object.id, object]));
    const childIDs = new Set(edges.map((edge) => edge.child_id));
    const childMap = new Map<string, StageObject[]>();
    for (const edge of edges) {
      const child = byId.get(edge.child_id);
      if (!child) {
        continue;
      }
      const list = childMap.get(edge.parent_id) ?? [];
      list.push(child);
      childMap.set(edge.parent_id, list);
    }
    for (const list of childMap.values()) {
      list.sort((left, right) => right.updated_at.localeCompare(left.updated_at));
    }
    return {
      roots: objects
        .filter((object) => !childIDs.has(object.id))
        .sort((left, right) => right.updated_at.localeCompare(left.updated_at)),
      childrenOf: childMap
    };
  }, [stage]);

  const branchCount = stage?.edges.length ?? 0;
  const displayName = user?.display_name ?? "";

  function typeLabel(type: StageObject["object_type"]): string {
    if (type === "living_scene") {
      return t("object.create.scene");
    }
    if (type === "interactive") {
      return t("object.create.interactive");
    }
    return t("object.create.collaborative");
  }

  function renderNode(object: StageObject, depth: number, visited: Set<string>) {
    if (visited.has(object.id) || depth > 5) {
      return null;
    }
    const nextVisited = new Set(visited);
    nextVisited.add(object.id);
    const children = childrenOf.get(object.id) ?? [];
    return (
      <li key={object.id} className="stage-node-wrap">
        <article className={`stage-node stage-node-${object.object_type}`}>
          <div className="stage-node-top">
            <span className={`template-badge template-${object.object_type}`}>
              {typeLabel(object.object_type)}
            </span>
            <span className="version-badge">v{object.version}</span>
          </div>
          <h3>
            <Link to={`/objects/${object.id}`}>{object.title || t("common.untitled")}</Link>
          </h3>
          <p className="stage-node-meta">
            {object.member_count > 1
              ? t("stage.members", { count: object.member_count })
              : t("stage.solo")}
            {object.branch_count > 0 ? ` · ${t("stage.branches", { count: object.branch_count })}` : ""}
          </p>
          {object.latest_event ? (
            <p className="stage-node-event">
              <span className="stage-event-dot" aria-hidden="true" />
              {object.latest_event.action
                ? t(actionKey(object.latest_event.action))
                : t(eventKey(object.latest_event.event_type))}
              {object.latest_event.actor_name ? ` · ${object.latest_event.actor_name}` : ""}
            </p>
          ) : null}
          <div className="stage-node-actions">
            <Link className="button button-small" to={`/objects/${object.id}`}>
              {t("stage.open")}
            </Link>
            {object.permissions.includes("derive") ? (
              <Link className="button button-ghost button-small" to={`/objects/${object.id}/derive`}>
                {t("stage.derive")}
              </Link>
            ) : null}
            <Link className="button button-ghost button-small" to={`/objects/${object.id}?tab=ai`}>
              {t("stage.ai")}
            </Link>
          </div>
        </article>
        {children.length > 0 ? (
          <ul className="stage-children">
            {children.map((child) => renderNode(child, depth + 1, nextVisited))}
          </ul>
        ) : null}
      </li>
    );
  }

  return (
    <div className="page page-wide stage-page" data-testid="world-stage">
      <div className="page-heading with-actions stage-heading">
        <div>
          <p className="eyebrow">{t("stage.eyebrow")}</p>
          <h1>{t("stage.title", { name: displayName })}</h1>
          <p className="lead">{t("stage.lead")}</p>
        </div>
        <div className="stage-create">
          <Link className="button" to="/objects/new?template=living_scene&purpose=project">
            {t("stage.createProject")}
          </Link>
          <Link className="button button-ghost" to="/objects/new?template=living_scene&purpose=story">
            {t("stage.createStory")}
          </Link>
          <Link className="button button-ghost" to="/explore">
            {t("stage.explore")}
          </Link>
        </div>
      </div>

      {error ? <ErrorNotice error={error} onRetry={() => void load()} /> : null}

      <div className="stage-stats">
        <span>
          <strong>{stage?.objects.length ?? 0}</strong> {t("stage.statObjects")}
        </span>
        <span>
          <strong>{branchCount}</strong> {t("stage.statBranches")}
        </span>
        <span>
          <strong>{roots.length}</strong> {t("stage.statRoots")}
        </span>
        <button className="button button-ghost button-small" type="button" onClick={() => void load()}>
          {t("common.refresh")}
        </button>
      </div>

      <div className="stage-layout">
        <section className="stage-board" aria-label={t("stage.boardLabel")}>
          {loading && !stage ? <p className="muted">{t("common.loading")}</p> : null}
          {!loading && (stage?.objects.length ?? 0) === 0 ? (
            <div className="stage-empty">
              <h2>{t("stage.emptyTitle")}</h2>
              <p>{t("stage.emptyBody")}</p>
              <div className="stage-empty-actions">
                <Link className="card stage-entry" to="/objects/new?template=living_scene&purpose=project">
                  <strong>{t("stage.entryProject")}</strong>
                  <small>{t("stage.entryProjectHint")}</small>
                </Link>
                <Link className="card stage-entry" to="/objects/new?template=living_scene&purpose=story">
                  <strong>{t("stage.entryStory")}</strong>
                  <small>{t("stage.entryStoryHint")}</small>
                </Link>
                <Link className="card stage-entry" to="/explore">
                  <strong>{t("stage.entryExplore")}</strong>
                  <small>{t("stage.entryExploreHint")}</small>
                </Link>
              </div>
            </div>
          ) : (
            <ul className="stage-roots">{roots.map((object) => renderNode(object, 0, new Set()))}</ul>
          )}
        </section>
        <aside className="stage-side">
          <PulsePanel compact />
          <section className="card">
            <h2>{t("stage.approvals")}</h2>
            {confirmations.length === 0 ? (
              <p className="muted">{t("stage.noApprovals")}</p>
            ) : (
              <ul className="pulse-list">
                {confirmations.slice(0, 5).map((confirmation) => (
                  <li key={confirmation.id}>
                    <div>
                      <strong>{confirmation.target_object_title || confirmation.action}</strong>
                      <small>{confirmation.permission_summary}</small>
                    </div>
                    <Link className="text-link" to="/self">
                      {t("stage.review")}
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </section>
          <section className="card stage-quick">
            <h2>{t("stage.quick")}</h2>
            <Link to="/objects">{t("nav.objects")}</Link>
            <Link to="/friends">{t("nav.friends")}</Link>
            <Link to="/ai">{t("nav.ai")}</Link>
            <Link to="/settings">{t("nav.settings")}</Link>
          </section>
        </aside>
      </div>
    </div>
  );
}
